package ai

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/sjtly16/pgmonk/internal/analyzer"
	"github.com/sjtly16/pgmonk/internal/model"
)

// Limits keep the digest around 1.5k tokens so pack + digest + answer fit
// comfortably in an 8k context.
const (
	maxSessions = 8
	maxQueries  = 8
	maxTables   = 8
	maxIndexes  = 5
	maxQueryLen = 160
)

// Digest renders the parts of a snapshot that matter for diagnosis as compact
// text, followed by the rule-based findings. The model's job is to explain and
// connect these, not to rediscover them.
func Digest(snap *model.Snapshot, findings []analyzer.Insight) string {
	var b strings.Builder
	o := snap.Overview
	si := snap.ServerInfo

	b.WriteString("## Snapshot\n")
	fmt.Fprintf(&b, "Database %q, server %s", o.DatabaseName, shortVersion(si.Version))
	if !si.StartTime.IsZero() {
		fmt.Fprintf(&b, ", up %s", dur(snap.CollectedAt.Sub(si.StartTime)))
	}
	fmt.Fprintf(&b, ". Standby: %s. track_io_timing: %s. pg_stat_statements: %s.\n",
		yesNo(si.IsStandby), onOff(si.TrackIOTiming), yesNo(si.HasStatStatements))
	fmt.Fprintf(&b, "Connections: %d/%d (active %d, idle %d, idle in transaction %d).\n",
		o.TotalConnections, o.MaxConnections, o.ActiveConnections, o.IdleConnections, o.IdleInTxConnections)
	fmt.Fprintf(&b, "Throughput: %.0f commits/s, %.1f rollbacks/s (rollback ratio %.1f%%). Cache hit ratio %.2f%%. Deadlocks %d. Temp files %d (%s).\n",
		o.TxCommitRate, o.TxRollbackRate, o.RollbackRatio, o.CacheHitRatio, o.Deadlocks, o.TempFiles, fmtBytes(o.TempBytes))
	fmt.Fprintf(&b, "Database size %s. Oldest frozen XID age %s (wraparound at ~2.1B).\n", fmtBytes(o.DatabaseSize), num(o.DataFrozenXIDAge))

	// Live problems first: small models weigh early context more.
	writeVacuumHorizon(&b, snap)
	writeSessions(&b, snap)
	writeLockChains(&b, snap)

	b.WriteString("\n## Configuration and background activity\n")
	writeSettings(&b, snap.Settings)
	writeBackground(&b, snap)
	writeQueries(&b, snap)
	writeTables(&b, snap)
	writeIndexes(&b, snap)
	writeReplication(&b, snap)

	b.WriteString("\n## Rule-based findings (pg-monk's built-in checks)\n")
	if len(findings) == 0 {
		b.WriteString("None — all checks passed.\n")
	}
	for _, f := range findings {
		fmt.Fprintf(&b, "- [%s] %s: %s\n", severityName(f.Severity), f.Title, f.Description)
	}
	return b.String()
}

// writeVacuumHorizon names the client session whose open transaction is
// oldest among those holding an xid or snapshot: while it is open, VACUUM
// cannot remove rows that became dead after it started, so dead tuples pile
// up even on freshly vacuumed tables. Stated explicitly because models rarely
// infer it from raw session data.
func writeVacuumHorizon(b *strings.Builder, snap *model.Snapshot) {
	var oldest *model.Activity
	for i := range snap.Activities {
		a := &snap.Activities[i]
		if a.ApplicationName == "pg-monk" || a.XactStart.IsZero() || (a.BackendXID == "" && a.BackendXmin == "") {
			continue
		}
		if a.BackendType != "" && a.BackendType != "client backend" {
			continue
		}
		if oldest == nil || a.XactStart.Before(oldest.XactStart) {
			oldest = a
		}
	}
	if oldest == nil {
		return
	}
	age := snap.CollectedAt.Sub(oldest.XactStart)
	if age < 30*time.Second {
		return
	}
	fmt.Fprintf(b, "\n## Vacuum horizon\nHeld back by pid %d (app=%s, state=%q), whose transaction has been open for %s. Until it ends, VACUUM and autovacuum cannot remove any row version that became dead after it started — dead tuples will keep growing on every frequently updated table even right after autovacuum runs.\n",
		oldest.PID, orDash(oldest.ApplicationName), oldest.State, dur(age))
}

func writeSettings(b *strings.Builder, settings []model.Setting) {
	if len(settings) == 0 {
		return
	}
	parts := make([]string, 0, len(settings))
	for _, s := range settings {
		v := s.Value
		if s.Unit != "" {
			v += s.Unit
		}
		if s.Context == "postmaster" {
			v += "*"
		}
		parts = append(parts, s.Name+"="+v)
	}
	b.WriteString("Settings (* = only changes after a server restart, not with pg_reload_conf): " + strings.Join(parts, ", ") + ".\n")
}

func writeBackground(b *strings.Builder, snap *model.Snapshot) {
	o := snap.Overview
	if c := snap.CheckpointerStats; c != nil {
		done := ""
		if snap.ServerInfo.Catalog.Has("pg_stat_checkpointer", "num_done") {
			done = fmt.Sprintf(", done %d", c.NumDone)
		}
		fmt.Fprintf(b, "Checkpoints (pg_stat_checkpointer): timed %d, requested %d%s, write %.0fms, sync %.0fms, buffers written %d.\n",
			c.NumTimed, c.NumRequested, done, c.WriteTime, c.SyncTime, c.BuffersWritten)
	} else if o.CheckpointsTimed+o.CheckpointsReq > 0 {
		fmt.Fprintf(b, "Checkpoints (pg_stat_bgwriter): timed %d, requested %d.\n", o.CheckpointsTimed, o.CheckpointsReq)
	}
	if w := snap.WALStats; w != nil {
		fmt.Fprintf(b, "WAL: %s records, %s full-page images, %s generated, wal_buffers_full %d.\n",
			num(w.WALRecords), num(w.WALFPI), fmtBytes(w.WALBytes), w.WALBuffersFull)
	}
	if a := snap.ArchiverStats; a != nil && (a.ArchivedCount > 0 || a.FailedCount > 0) {
		fmt.Fprintf(b, "Archiver: archived %d, failed %d.\n", a.ArchivedCount, a.FailedCount)
	}
}

func writeSessions(b *strings.Builder, snap *model.Snapshot) {
	type row struct {
		a   model.Activity
		age time.Duration
	}
	var rows []row
	for _, a := range snap.Activities {
		if a.BackendType != "" && a.BackendType != "client backend" {
			continue
		}
		if a.ApplicationName == "pg-monk" {
			continue
		}
		age := time.Duration(0)
		switch {
		case strings.HasPrefix(a.State, "idle in transaction") && !a.XactStart.IsZero():
			age = snap.CollectedAt.Sub(a.XactStart)
		case a.State == "active":
			age = a.Duration
		default:
			continue
		}
		if age < time.Second && a.WaitEventType != "Lock" {
			continue
		}
		rows = append(rows, row{a, age})
	}
	if len(rows) == 0 {
		return
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].age > rows[j].age })
	b.WriteString("\n## Sessions of interest right now (longest first)\n")
	for i, r := range rows {
		if i == maxSessions {
			fmt.Fprintf(b, "- … %d more\n", len(rows)-maxSessions)
			break
		}
		a := r.a
		wait := ""
		if a.WaitEventType != "" {
			wait = fmt.Sprintf(", waiting on %s:%s", a.WaitEventType, a.WaitEvent)
		}
		fmt.Fprintf(b, "- pid %d app=%s state=%q for %s%s: %s\n",
			a.PID, orDash(a.ApplicationName), a.State, dur(r.age), wait, clip(a.Query))
	}
}

func writeLockChains(b *strings.Builder, snap *model.Snapshot) {
	waiting := map[int]model.LockInfo{}
	for _, l := range snap.Locks {
		if !l.Granted && len(l.BlockedByPIDs) > 0 {
			if _, ok := waiting[l.PID]; !ok {
				waiting[l.PID] = l
			}
		}
	}
	if len(waiting) == 0 {
		return
	}
	apps := map[int]model.Activity{}
	for _, a := range snap.Activities {
		apps[a.PID] = a
	}
	pids := make([]int, 0, len(waiting))
	for p := range waiting {
		pids = append(pids, p)
	}
	sort.Ints(pids)
	b.WriteString("\n## Lock waits right now\n")
	for _, p := range pids {
		l := waiting[p]
		target := l.RelationName
		if target == "" {
			target = l.LockType
		}
		blockers := make([]string, 0, len(l.BlockedByPIDs))
		for _, bp := range l.BlockedByPIDs {
			ba := apps[bp]
			blockers = append(blockers, fmt.Sprintf("pid %d (%s, %s)", bp, orDash(ba.ApplicationName), ba.State))
		}
		fmt.Fprintf(b, "- pid %d (%s) waiting %s for %s on %s, blocked by %s\n",
			p, orDash(apps[p].ApplicationName), dur(l.Duration), l.Mode, target, strings.Join(blockers, ", "))
	}
}

func writeQueries(b *strings.Builder, snap *model.Snapshot) {
	if len(snap.Queries) == 0 {
		return
	}
	qs := append([]model.QueryStat(nil), snap.Queries...)
	sort.SliceStable(qs, func(i, j int) bool { return qs[i].TotalExecTime > qs[j].TotalExecTime })
	b.WriteString("\n## Top queries by total execution time (pg_stat_statements)\n")
	for i, q := range qs {
		if i == maxQueries {
			break
		}
		fmt.Fprintf(b, "%d. total %s, %s calls, mean %s, rows %s, cache hit %.0f%%, temp blks written %d: %s\n",
			i+1, ms(q.TotalExecTime), num(q.Calls), ms(q.MeanExecTime), num(q.Rows), q.CacheHitRatio, q.TempBlksWritten, clip(q.Query))
	}
}

func writeTables(b *strings.Builder, snap *model.Snapshot) {
	if len(snap.Tables) == 0 {
		return
	}
	ts := append([]model.TableStat(nil), snap.Tables...)
	// Most interesting first: dead tuples, then sequential scan volume.
	sort.SliceStable(ts, func(i, j int) bool {
		if ts[i].DeadTuples != ts[j].DeadTuples {
			return ts[i].DeadTuples > ts[j].DeadTuples
		}
		return ts[i].SeqTupRead > ts[j].SeqTupRead
	})
	b.WriteString("\n## Tables (most dead tuples first)\n")
	for i, t := range ts {
		if i == maxTables {
			break
		}
		fmt.Fprintf(b, "- %s.%s: %s live, %s dead, seq_scan %s (%s rows read), idx_scan %s, size %s, mods since analyze %s, last vacuum %s, last autovacuum %s\n",
			t.SchemaName, t.TableName, num(t.LiveTuples), num(t.DeadTuples), num(t.SeqScan), num(t.SeqTupRead), num(t.IdxScan),
			fmtBytes(t.TotalSize), num(t.ModsSinceAnalyze), ago(snap.CollectedAt, t.LastVacuum), ago(snap.CollectedAt, t.LastAutoVacuum))
	}
}

func writeIndexes(b *strings.Builder, snap *model.Snapshot) {
	var unused []model.IndexStat
	for _, ix := range snap.Indexes {
		if ix.IndexScan == 0 && !ix.IsUnique {
			unused = append(unused, ix)
		}
	}
	if len(unused) == 0 {
		return
	}
	sort.SliceStable(unused, func(i, j int) bool { return unused[i].IndexSize > unused[j].IndexSize })
	b.WriteString("\n## Never-scanned non-unique indexes\n")
	for i, ix := range unused {
		if i == maxIndexes {
			break
		}
		fmt.Fprintf(b, "- %s on %s.%s (%s)\n", ix.IndexName, ix.SchemaName, ix.TableName, fmtBytes(ix.IndexSize))
	}
}

func writeReplication(b *strings.Builder, snap *model.Snapshot) {
	if len(snap.Replication) == 0 {
		return
	}
	b.WriteString("\n## Replication\n")
	for _, r := range snap.Replication {
		lag := "-"
		if r.ReplayLag != nil {
			lag = dur(*r.ReplayLag)
		}
		fmt.Fprintf(b, "- %s (%s) state=%s sync=%s replay lag %s\n", orDash(r.ApplicationName), r.ClientAddr, r.State, r.SyncState, lag)
	}
}

// ---- formatting helpers ----

func severityName(s analyzer.Severity) string {
	switch s {
	case analyzer.Critical:
		return "critical"
	case analyzer.Warning:
		return "warning"
	default:
		return "info"
	}
}

func clip(q string) string {
	q = strings.Join(strings.Fields(q), " ")
	if r := []rune(q); len(r) > maxQueryLen {
		return string(r[:maxQueryLen-1]) + "…"
	}
	return q
}

func shortVersion(v string) string {
	// "PostgreSQL 17.9 (Debian 17.9-1.pgdg13+1) on aarch64-..." -> "PostgreSQL 17.9"
	f := strings.Fields(v)
	if len(f) >= 2 {
		return f[0] + " " + f[1]
	}
	return v
}

func dur(d time.Duration) string {
	switch {
	case d <= 0:
		return "0s"
	case d < time.Second:
		return fmt.Sprintf("%dms", d.Milliseconds())
	case d < time.Minute:
		return fmt.Sprintf("%.0fs", d.Seconds())
	case d < time.Hour:
		return fmt.Sprintf("%.1fm", d.Minutes())
	case d < 48*time.Hour:
		return fmt.Sprintf("%.1fh", d.Hours())
	default:
		return fmt.Sprintf("%.1fd", d.Hours()/24)
	}
}

func ms(v float64) string { return dur(time.Duration(v * float64(time.Millisecond))) }

func ago(now time.Time, t *time.Time) string {
	if t == nil || t.IsZero() {
		return "never"
	}
	return dur(now.Sub(*t)) + " ago"
}

func num(n int64) string {
	switch {
	case n >= 1_000_000_000:
		return fmt.Sprintf("%.1fB", float64(n)/1e9)
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 10_000:
		return fmt.Sprintf("%.1fK", float64(n)/1e3)
	default:
		return fmt.Sprintf("%d", n)
	}
}

func fmtBytes(n int64) string {
	const k = 1024
	switch {
	case n >= k*k*k:
		return fmt.Sprintf("%.1f GB", float64(n)/(k*k*k))
	case n >= k*k:
		return fmt.Sprintf("%.1f MB", float64(n)/(k*k))
	case n >= k:
		return fmt.Sprintf("%.1f KB", float64(n)/k)
	default:
		return fmt.Sprintf("%d B", n)
	}
}

func yesNo(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

func onOff(v bool) string {
	if v {
		return "on"
	}
	return "off"
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
