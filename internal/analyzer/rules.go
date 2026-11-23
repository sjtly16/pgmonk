package analyzer

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/sjtly16/pgmonk/internal/model"
)

type Severity int

const (
	Info Severity = iota
	Warning
	Critical
)

func (s Severity) String() string {
	switch s {
	case Info:
		return "INFO"
	case Warning:
		return "WARN"
	case Critical:
		return "CRIT"
	default:
		return "UNKNOWN"
	}
}

type Insight struct {
	Severity    Severity
	Category    string
	Title       string
	Description string
	Suggestion  string
}

// Thresholds holds configurable thresholds for analyzer rules.
// The package-level ActiveThresholds is used by all rules.
type Thresholds struct {
	CacheHitWarn           float64
	CacheHitCritical       float64
	ConnectionWarn         float64
	ConnectionCritical     float64
	DeadTuplesWarn         int64
	DeadTuplesCritical     int64
	IdleInTxWarn           time.Duration
	IdleInTxCritical       time.Duration
	XIDWarn                int64
	XIDCritical            int64
	ReplicationLagWarn     time.Duration
	ReplicationLagCritical time.Duration
	RollbackRatioWarn      float64
	TempBytesWarn          int64
	LongTxWarn             time.Duration
	LongTxCritical         time.Duration
}

// DefaultThresholds returns the default thresholds.
func DefaultThresholds() Thresholds {
	return Thresholds{
		CacheHitWarn:           99.0,
		CacheHitCritical:       95.0,
		ConnectionWarn:         70.0,
		ConnectionCritical:     85.0,
		DeadTuplesWarn:         10_000,
		DeadTuplesCritical:     100_000,
		IdleInTxWarn:           30 * time.Second,
		IdleInTxCritical:       5 * time.Minute,
		XIDWarn:                500_000_000,
		XIDCritical:            1_500_000_000,
		ReplicationLagWarn:     30 * time.Second,
		ReplicationLagCritical: 5 * time.Minute,
		RollbackRatioWarn:      5.0,
		TempBytesWarn:          1 << 30, // 1 GB
		LongTxWarn:             1 * time.Hour,
		LongTxCritical:         6 * time.Hour,
	}
}

// ActiveThresholds is the package-level thresholds used by all rules.
// Set via SetThresholds() before calling Analyze().
var ActiveThresholds = DefaultThresholds()

// SetThresholds updates the active thresholds used by the analyzer.
func SetThresholds(t Thresholds) {
	ActiveThresholds = t
}

func Analyze(snap *model.Snapshot) []Insight {
	if snap == nil {
		return nil
	}
	var insights []Insight
	insights = append(insights, checkCacheHitRatio(snap)...)
	insights = append(insights, checkConnections(snap)...)
	insights = append(insights, checkDeadTuples(snap)...)
	insights = append(insights, checkUnusedIndexes(snap)...)
	insights = append(insights, checkMissingIndexes(snap)...)
	insights = append(insights, checkIdleInTransaction(snap)...)
	insights = append(insights, checkSlowQueries(snap)...)
	insights = append(insights, checkClientConcentration(snap)...)
	insights = append(insights, checkXIDWraparound(snap)...)
	insights = append(insights, checkCheckpointFrequency(snap)...)
	insights = append(insights, checkArchiverFailures(snap)...)
	insights = append(insights, checkWALGeneration(snap)...)
	insights = append(insights, checkBGWriterPressure(snap)...)
	insights = append(insights, checkClientBackendIO(snap)...)
	insights = append(insights, checkReplicationLag(snap)...)
	insights = append(insights, checkRollbackRatio(snap)...)
	insights = append(insights, checkTempFileUsage(snap)...)
	insights = append(insights, checkStaleStatistics(snap)...)
	insights = append(insights, checkLongRunningTransactions(snap)...)
	insights = append(insights, checkTableBloat(snap)...)
	insights = append(insights, checkSequentialScanDominance(snap)...)
	insights = append(insights, checkTableIOCacheRatio(snap)...)
	insights = append(insights, checkVacuumProgress(snap)...)
	insights = append(insights, checkStandbyConflicts(snap)...)
	insights = append(insights, checkWALBuffersFull(snap)...)
	insights = append(insights, checkHighTempBlkQueries(snap)...)
	insights = append(insights, checkWaitEvents(snap)...)

	// Most severe first; rule order is preserved within a severity.
	sort.SliceStable(insights, func(i, j int) bool {
		return insights[i].Severity > insights[j].Severity
	})
	return insights
}

// ---- Core rules ----

func checkCacheHitRatio(snap *model.Snapshot) []Insight {
	ratio := snap.Overview.CacheHitRatio
	if ratio == 0 {
		return nil
	}
	t := ActiveThresholds
	if ratio < t.CacheHitCritical {
		return []Insight{{Critical, "cache", "Low cache hit ratio",
			fmt.Sprintf("Cache hit ratio is %.2f%%, well below the recommended %.0f%%.", ratio, t.CacheHitWarn),
			"Consider increasing shared_buffers or reducing the working set size."}}
	}
	if ratio < t.CacheHitWarn {
		return []Insight{{Warning, "cache", "Cache hit ratio below optimal",
			fmt.Sprintf("Cache hit ratio is %.2f%%, below the recommended %.0f%%.", ratio, t.CacheHitWarn),
			"Monitor if this degrades further. Consider increasing shared_buffers."}}
	}
	return nil
}

func checkConnections(snap *model.Snapshot) []Insight {
	o := snap.Overview
	if o.MaxConnections == 0 {
		return nil
	}
	t := ActiveThresholds
	pct := float64(o.TotalConnections) / float64(o.MaxConnections) * 100
	if pct > t.ConnectionCritical {
		return []Insight{{Critical, "connections", "Connection pool near capacity",
			fmt.Sprintf("Using %d of %d connections (%.0f%%).", o.TotalConnections, o.MaxConnections, pct),
			"Consider using a connection pooler like PgBouncer, or increase max_connections."}}
	}
	if pct > t.ConnectionWarn {
		return []Insight{{Warning, "connections", "Moderate connection usage",
			fmt.Sprintf("Using %d of %d connections (%.0f%%).", o.TotalConnections, o.MaxConnections, pct),
			"Monitor trend. Consider PgBouncer if connections keep growing."}}
	}
	return nil
}

func checkDeadTuples(snap *model.Snapshot) []Insight {
	var insights []Insight
	for _, t := range snap.Tables {
		if t.DeadTuples > 10000 && t.LiveTuples > 0 {
			deadRatio := float64(t.DeadTuples) / float64(t.LiveTuples) * 100
			if deadRatio > 20 {
				insights = append(insights, Insight{Critical, "vacuum",
					fmt.Sprintf("High dead tuple ratio on %s.%s", t.SchemaName, t.TableName),
					fmt.Sprintf("%d dead tuples (%.0f%% of live). Last vacuum: %v", t.DeadTuples, deadRatio, formatTimePtr(t.LastAutoVacuum)),
					"Run VACUUM ANALYZE on this table, or tune autovacuum parameters."})
			}
		}
	}
	return insights
}

func checkUnusedIndexes(snap *model.Snapshot) []Insight {
	var insights []Insight
	for _, idx := range snap.Indexes {
		if idx.IndexScan == 0 && idx.IndexSize > 1024*1024 && !idx.IsUnique {
			insights = append(insights, Insight{Info, "indexes",
				fmt.Sprintf("Unused index: %s", idx.IndexName),
				fmt.Sprintf("Index on %s.%s has 0 scans and uses %s.", idx.SchemaName, idx.TableName, formatBytes(idx.IndexSize)),
				"Consider dropping this index to save space and improve write performance."})
		}
	}
	return insights
}

func checkMissingIndexes(snap *model.Snapshot) []Insight {
	var insights []Insight
	for _, t := range snap.Tables {
		if t.SeqScan > 1000 && t.IdxScan == 0 && t.LiveTuples > 10000 {
			insights = append(insights, Insight{Warning, "indexes",
				fmt.Sprintf("Potential missing index on %s.%s", t.SchemaName, t.TableName),
				fmt.Sprintf("Table has %d sequential scans, 0 index scans, and %d live rows.", t.SeqScan, t.LiveTuples),
				"Analyze common query patterns on this table and add appropriate indexes."})
		}
	}
	return insights
}

func checkIdleInTransaction(snap *model.Snapshot) []Insight {
	count := 0
	for _, a := range snap.Activities {
		if a.State == "idle in transaction" || a.State == "idle in transaction (aborted)" {
			count++
		}
	}
	if count > 3 {
		return []Insight{{Warning, "connections", "Multiple idle-in-transaction sessions",
			fmt.Sprintf("%d sessions are idle in transaction, holding locks and preventing vacuum.", count),
			"Check application code for uncommitted transactions. Consider setting idle_in_transaction_session_timeout."}}
	}
	return nil
}

func checkSlowQueries(snap *model.Snapshot) []Insight {
	var insights []Insight
	for _, q := range snap.Queries {
		if q.MeanExecTime > 5000 && q.Calls > 10 {
			insights = append(insights, Insight{Warning, "queries", "Slow frequent query detected",
				fmt.Sprintf("%d calls averaging %.0fms: %s", q.Calls, q.MeanExecTime, shortQuery(q.Query, 80)),
				"Run EXPLAIN ANALYZE on this query and consider adding indexes or rewriting it."})
			if len(insights) >= 3 {
				break
			}
		}
	}
	return insights
}

// shortQuery collapses whitespace and truncates query text for messages.
func shortQuery(q string, max int) string {
	r := []rune(strings.Join(strings.Fields(q), " "))
	if len(r) <= max {
		return string(r)
	}
	return string(r[:max-3]) + "..."
}

func checkClientConcentration(snap *model.Snapshot) []Insight {
	var insights []Insight
	if len(snap.Clients) == 0 || snap.Overview.MaxConnections == 0 {
		return nil
	}
	for _, c := range snap.Clients {
		pct := float64(c.TotalConnections) / float64(snap.Overview.MaxConnections) * 100
		if pct > 30 {
			insights = append(insights, Insight{Warning, "clients",
				fmt.Sprintf("Client %s using %.0f%% of connections", c.ClientAddr, pct),
				fmt.Sprintf("Client %s holds %d of %d max connections (%.0f%%).", c.ClientAddr, c.TotalConnections, snap.Overview.MaxConnections, pct),
				"Check if this client has a connection pool leak. Consider using PgBouncer."})
		}
		if c.IdleInTxConnections > 3 {
			insights = append(insights, Insight{Warning, "clients",
				fmt.Sprintf("Client %s has %d idle-in-transaction threads", c.ClientAddr, c.IdleInTxConnections),
				fmt.Sprintf("Client %s has %d backends stuck in idle-in-transaction state.", c.ClientAddr, c.IdleInTxConnections),
				"Review the application code for uncommitted transactions. Set idle_in_transaction_session_timeout."})
		}
		if c.LongestQueryDur.Minutes() > 5 {
			insights = append(insights, Insight{Warning, "clients",
				fmt.Sprintf("Client %s has a query running for %.0f minutes", c.ClientAddr, c.LongestQueryDur.Minutes()),
				fmt.Sprintf("The longest active query from %s has been running for %s.", c.ClientAddr, c.LongestQueryDur.Round(time.Second)),
				"Investigate if this is a long-running report or a stuck query. Consider statement_timeout."})
		}
		if len(insights) >= 5 {
			break
		}
	}
	return insights
}

// ---- New rules from PostgreSQL monitoring documentation ----

// checkXIDWraparound detects proximity to transaction ID wraparound.
// Per PostgreSQL docs §24.1.5: when datfrozenxid age exceeds ~2 billion, system refuses writes.
// autovacuum_freeze_max_age defaults to 200 million.
func checkXIDWraparound(snap *model.Snapshot) []Insight {
	age := snap.Overview.DataFrozenXIDAge
	if age == 0 {
		return nil
	}
	t := ActiveThresholds
	if age > t.XIDCritical {
		return []Insight{{Critical, "xid_wraparound",
			"Transaction ID wraparound danger",
			fmt.Sprintf("Database %s has datfrozenxid age of %s. System will stop accepting writes at 2 billion.", snap.Overview.OldestXactDB, formatNumber(age)),
			"Run aggressive VACUUM FREEZE on the affected database immediately. Check for long-running transactions blocking vacuum."}}
	}
	if age > t.XIDWarn {
		return []Insight{{Warning, "xid_wraparound",
			"Transaction ID wraparound risk growing",
			fmt.Sprintf("Database %s has datfrozenxid age of %s. Anti-wraparound vacuum may be overdue.", snap.Overview.OldestXactDB, formatNumber(age)),
			"Check that autovacuum is running and not blocked. Consider running manual VACUUM FREEZE."}}
	}
	return nil
}

// checkCheckpointFrequency analyzes if checkpoints are happening too often.
// Per PG docs: requested checkpoints indicate WAL filling up faster than configured.
func checkCheckpointFrequency(snap *model.Snapshot) []Insight {
	o := snap.Overview
	total := o.CheckpointsReq + o.CheckpointsTimed
	if total == 0 {
		return nil
	}
	reqPct := float64(o.CheckpointsReq) / float64(total) * 100
	if reqPct > 50 {
		return []Insight{{Warning, "checkpoints",
			"High ratio of requested checkpoints",
			fmt.Sprintf("%.0f%% of checkpoints are requested (not timed): %d requested vs %d timed.", reqPct, o.CheckpointsReq, o.CheckpointsTimed),
			"Checkpoints are triggered too frequently by WAL volume. Increase max_wal_size to allow more WAL between timed checkpoints."}}
	}
	return nil
}

// checkArchiverFailures detects WAL archiver issues from pg_stat_archiver.
func checkArchiverFailures(snap *model.Snapshot) []Insight {
	a := snap.ArchiverStats
	if a == nil {
		return nil
	}
	if a.FailedCount > 0 && a.LastFailedTime != nil {
		timeSinceFail := time.Since(*a.LastFailedTime)
		if timeSinceFail < 30*time.Minute {
			return []Insight{{Critical, "archiver",
				"WAL archiver failures detected",
				fmt.Sprintf("Archiver has %d total failures. Last failure: %s ago on file %s.", a.FailedCount, timeSinceFail.Round(time.Second), a.LastFailedWAL),
				"Check archive_command configuration and disk space on the archive destination. Unarchived WAL files consume pg_wal space."}}
		}
		if a.FailedCount > 10 {
			return []Insight{{Warning, "archiver",
				"WAL archiver has accumulated failures",
				fmt.Sprintf("Archiver has %d total failures. Last failure on file %s.", a.FailedCount, a.LastFailedWAL),
				"Review archive_command logs for recurring issues."}}
		}
	}
	return nil
}

// checkWALGeneration analyzes WAL generation volume from pg_stat_wal.
func checkWALGeneration(snap *model.Snapshot) []Insight {
	w := snap.WALStats
	if w == nil || w.StatsReset == nil {
		return nil
	}
	elapsed := time.Since(*w.StatsReset).Hours()
	if elapsed < 1 {
		return nil
	}
	walPerHourGB := float64(w.WALBytes) / (1024 * 1024 * 1024) / elapsed
	if walPerHourGB > 10 {
		return []Insight{{Warning, "wal",
			"High WAL generation rate",
			fmt.Sprintf("Generating %.1f GB/hour of WAL data (%d full-page images).", walPerHourGB, w.WALFPI),
			"High WAL volume increases disk I/O and replication lag. Consider reducing full_page_writes impact with larger checkpoint intervals, or review write-heavy workloads."}}
	}
	return nil
}

// checkBGWriterPressure detects if the background writer is falling behind.
// Per PG docs: maxwritten_clean > 0 means bgwriter stopped a scan due to too many buffers written.
func checkBGWriterPressure(snap *model.Snapshot) []Insight {
	bg := snap.BGWriterStats
	if bg == nil {
		return nil
	}
	if bg.MaxWrittenClean > 100 {
		return []Insight{{Warning, "bgwriter",
			"Background writer is hitting write limit",
			fmt.Sprintf("Background writer stopped cleaning %d times because it wrote too many buffers.", bg.MaxWrittenClean),
			"Increase bgwriter_lru_maxpages to allow the background writer to clean more buffers per round."}}
	}
	return nil
}

// checkClientBackendIO detects when client backends are performing excessive direct I/O.
// Per PG docs §27.2.13: large numbers of writes/fsyncs by client backends indicate misconfiguration.
func checkClientBackendIO(snap *model.Snapshot) []Insight {
	if len(snap.IOStats) == 0 {
		return nil
	}
	var clientWrites, clientFsyncs, checkpointerWrites int64
	for _, io := range snap.IOStats {
		if io.Object == "relation" && io.Context == "normal" {
			if io.BackendType == "client backend" {
				clientWrites += io.Writes
				clientFsyncs += io.Fsyncs
			}
			if io.BackendType == "checkpointer" {
				checkpointerWrites += io.Writes
			}
		}
	}
	var insights []Insight
	if clientFsyncs > 100 {
		insights = append(insights, Insight{Warning, "io",
			"Client backends performing direct fsyncs",
			fmt.Sprintf("Client backends have performed %d fsyncs directly, bypassing the checkpointer.", clientFsyncs),
			"This indicates the checkpointer cannot keep up. Increase shared_buffers or tune checkpoint_completion_target."})
	}
	if checkpointerWrites > 0 && clientWrites > checkpointerWrites {
		insights = append(insights, Insight{Warning, "io",
			"Client backends writing more than checkpointer",
			fmt.Sprintf("Client backends have written %d blocks vs checkpointer's %d.", clientWrites, checkpointerWrites),
			"Backends are forced to write dirty buffers themselves. Increase shared_buffers or reduce checkpoint interval."})
	}
	return insights
}

// checkReplicationLag detects lagging replicas from pg_stat_replication.
func checkReplicationLag(snap *model.Snapshot) []Insight {
	var insights []Insight
	t := ActiveThresholds
	for _, r := range snap.Replication {
		if r.ReplayLag != nil && *r.ReplayLag > t.ReplicationLagWarn {
			sev := Warning
			if *r.ReplayLag > t.ReplicationLagCritical {
				sev = Critical
			}
			insights = append(insights, Insight{sev, "replication",
				fmt.Sprintf("Replication lag on %s", r.ApplicationName),
				fmt.Sprintf("Replica %s (%s) has replay lag of %s.", r.ApplicationName, r.ClientAddr, r.ReplayLag.Round(time.Second)),
				"Check replica I/O capacity, network bandwidth, and recovery_min_apply_delay. High WAL generation on primary can also cause lag."})
		}
	}
	return insights
}

// checkRollbackRatio detects high transaction rollback ratio.
func checkRollbackRatio(snap *model.Snapshot) []Insight {
	ratio := snap.Overview.RollbackRatio
	if ratio > ActiveThresholds.RollbackRatioWarn {
		return []Insight{{Warning, "transactions",
			"High transaction rollback ratio",
			fmt.Sprintf("%.1f%% of transactions are being rolled back.", ratio),
			"Investigate application errors causing rollbacks. Check for serialization failures, constraint violations, or deadlocks."}}
	}
	return nil
}

// checkTempFileUsage detects excessive temp file usage from pg_stat_database.
func checkTempFileUsage(snap *model.Snapshot) []Insight {
	o := snap.Overview
	if o.TempBytes > ActiveThresholds.TempBytesWarn {
		return []Insight{{Warning, "temp_files",
			"High temporary file usage",
			fmt.Sprintf("Database has used %s in temp files (%d temp files created).", formatBytes(o.TempBytes), o.TempFiles),
			"Queries are spilling to disk. Consider increasing work_mem to reduce temp file usage for sorts and hash joins."}}
	}
	return nil
}

// checkStaleStatistics detects tables with outdated planner statistics.
// Per PG docs §14.2: stale statistics lead to poor query plans.
func checkStaleStatistics(snap *model.Snapshot) []Insight {
	var insights []Insight
	for _, t := range snap.Tables {
		if t.LiveTuples > 10000 && t.ModsSinceAnalyze > 0 {
			modRatio := float64(t.ModsSinceAnalyze) / float64(t.LiveTuples) * 100
			if modRatio > 30 {
				insights = append(insights, Insight{Warning, "statistics",
					fmt.Sprintf("Stale planner statistics on %s.%s", t.SchemaName, t.TableName),
					fmt.Sprintf("%d rows modified since last analyze (%.0f%% of %d live rows).", t.ModsSinceAnalyze, modRatio, t.LiveTuples),
					"Run ANALYZE on this table to update planner statistics. Check autovacuum_analyze_scale_factor."})
				if len(insights) >= 3 {
					break
				}
			}
		}
	}
	return insights
}

// checkLongRunningTransactions detects transactions holding old XIDs.
// Per PG docs §24.1.5: long-running transactions prevent vacuum from cleaning up.
func checkLongRunningTransactions(snap *model.Snapshot) []Insight {
	var insights []Insight
	t := ActiveThresholds
	for _, a := range snap.Activities {
		if (a.State == "idle in transaction" || a.State == "active") && !a.XactStart.IsZero() {
			txDuration := time.Since(a.XactStart)
			if txDuration > t.LongTxWarn {
				sev := Warning
				if txDuration > t.LongTxCritical {
					sev = Critical
				}
				insights = append(insights, Insight{sev, "transactions",
					fmt.Sprintf("Long-running transaction (PID %d)", a.PID),
					fmt.Sprintf("Transaction from %s@%s has been open for %s (state: %s). This blocks autovacuum and holds XIDs.",
						a.UserName, a.ClientAddr, txDuration.Round(time.Second), a.State),
					"Commit or rollback this transaction. Consider idle_in_transaction_session_timeout or statement_timeout."})
				if len(insights) >= 3 {
					break
				}
			}
		}
	}
	return insights
}

// checkTableBloat estimates table bloat from dead tuple ratios and vacuum lag.
func checkTableBloat(snap *model.Snapshot) []Insight {
	var insights []Insight
	now := time.Now()
	for _, t := range snap.Tables {
		if t.LiveTuples < 10000 || t.TableSize < 10*1024*1024 {
			continue
		}
		// Tables with high dead tuples and no recent vacuum.
		vacuumOverdue := true
		if t.LastAutoVacuum != nil && now.Sub(*t.LastAutoVacuum) < 24*time.Hour {
			vacuumOverdue = false
		}
		if t.LastVacuum != nil && now.Sub(*t.LastVacuum) < 24*time.Hour {
			vacuumOverdue = false
		}
		if vacuumOverdue && t.DeadTupleRatio > 10 && t.InsSinceVacuum > 50000 {
			insights = append(insights, Insight{Warning, "bloat",
				fmt.Sprintf("Possible table bloat on %s.%s", t.SchemaName, t.TableName),
				fmt.Sprintf("Table has %.0f%% dead tuples, %d inserts since last vacuum, and size of %s. No vacuum in 24+ hours.",
					t.DeadTupleRatio, t.InsSinceVacuum, formatBytes(t.TableSize)),
				"Run VACUUM on this table. If bloat is severe, consider VACUUM FULL (requires exclusive lock) or pg_repack."})
			if len(insights) >= 3 {
				break
			}
		}
	}
	return insights
}

// checkSequentialScanDominance detects tables heavily relying on seq scans.
func checkSequentialScanDominance(snap *model.Snapshot) []Insight {
	var insights []Insight
	for _, t := range snap.Tables {
		if t.LiveTuples > 50000 && t.SeqScanRatio > 90 && t.SeqScan > 500 {
			insights = append(insights, Insight{Info, "indexes",
				fmt.Sprintf("Sequential scan dominance on %s.%s", t.SchemaName, t.TableName),
				fmt.Sprintf("%.0f%% of scans are sequential (%d seq vs %d idx) on a table with %d rows.", t.SeqScanRatio, t.SeqScan, t.IdxScan, t.LiveTuples),
				"Review query patterns and consider adding indexes to reduce sequential scans on large tables."})
			if len(insights) >= 3 {
				break
			}
		}
	}
	return insights
}

// checkTableIOCacheRatio detects tables with poor buffer cache hit rates from pg_statio_user_tables.
func checkTableIOCacheRatio(snap *model.Snapshot) []Insight {
	var insights []Insight
	for _, t := range snap.TableIOStats {
		totalOps := t.HeapBlksHit + t.HeapBlksRead + t.IdxBlksHit + t.IdxBlksRead
		if totalOps < 10000 {
			continue
		}
		if t.CacheHitRatio < 90 {
			insights = append(insights, Insight{Warning, "cache",
				fmt.Sprintf("Low cache hit ratio on table %s.%s", t.SchemaName, t.TableName),
				fmt.Sprintf("Table has a cache hit ratio of %.1f%% (%d hits vs %d reads). Heavy disk I/O.",
					t.CacheHitRatio, t.HeapBlksHit+t.IdxBlksHit, t.HeapBlksRead+t.IdxBlksRead),
				"This table's working set may exceed shared_buffers. Consider increasing shared_buffers or optimizing queries to access less data."})
			if len(insights) >= 3 {
				break
			}
		}
	}
	return insights
}

// checkVacuumProgress reports on currently running vacuums.
func checkVacuumProgress(snap *model.Snapshot) []Insight {
	var insights []Insight
	for _, v := range snap.VacuumProgress {
		if v.HeapBlksTotal > 0 {
			pct := float64(v.HeapBlksScanned) / float64(v.HeapBlksTotal) * 100
			if pct < 50 && v.NumDeadTuples > 100000 {
				insights = append(insights, Insight{Info, "vacuum",
					fmt.Sprintf("Vacuum in progress on %s.%s", v.TableSchema, v.TableName),
					fmt.Sprintf("Phase: %s, %.0f%% scanned, %d dead tuples found so far.", v.Phase, pct, v.NumDeadTuples),
					"Vacuum is actively cleaning this table. Monitor progress."})
			}
		}
	}
	return insights
}

// checkStandbyConflicts analyzes query cancellations on standby servers.
// Per PG docs §27.2.18: pg_stat_database_conflicts tracks recovery vs query conflicts.
func checkStandbyConflicts(snap *model.Snapshot) []Insight {
	cs := snap.ConflictStats
	if cs == nil {
		return nil
	}
	total := cs.ConflTablespace + cs.ConflLock + cs.ConflSnapshot + cs.ConflBufferpin + cs.ConflDeadlock + cs.ConflLogicalSlot
	if total > 100 {
		details := fmt.Sprintf("snapshot=%d, lock=%d, bufferpin=%d, deadlock=%d, tablespace=%d",
			cs.ConflSnapshot, cs.ConflLock, cs.ConflBufferpin, cs.ConflDeadlock, cs.ConflTablespace)
		return []Insight{{Warning, "replication",
			"High standby query cancellations",
			fmt.Sprintf("%d total queries cancelled due to recovery conflicts (%s).", total, details),
			"Consider increasing max_standby_streaming_delay or hot_standby_feedback on the standby to reduce cancellations."}}
	}
	return nil
}

// checkWALBuffersFull detects WAL buffer pressure from pg_stat_wal.
func checkWALBuffersFull(snap *model.Snapshot) []Insight {
	w := snap.WALStats
	if w == nil {
		return nil
	}
	if w.WALBuffersFull > 1000 {
		return []Insight{{Warning, "wal",
			"WAL buffers filling up frequently",
			fmt.Sprintf("WAL data was written to disk %d times because WAL buffers were full.", w.WALBuffersFull),
			"Increase wal_buffers to reduce forced WAL writes. Default is typically 1/32 of shared_buffers."}}
	}
	return nil
}

// checkHighTempBlkQueries detects queries with excessive temp block usage from pg_stat_statements.
func checkHighTempBlkQueries(snap *model.Snapshot) []Insight {
	var insights []Insight
	for _, q := range snap.Queries {
		totalTemp := q.TempBlksRead + q.TempBlksWritten
		if totalTemp > 100000 && q.Calls > 5 {
			insights = append(insights, Insight{Warning, "queries",
				"Query with excessive temp block usage",
				fmt.Sprintf("Query with %d calls uses %d temp blocks per execution (reads: %d, writes: %d).",
					q.Calls, totalTemp/q.Calls, q.TempBlksRead/q.Calls, q.TempBlksWritten/q.Calls),
				"This query spills data to disk for sorts or hash joins. Increase work_mem or optimize the query to reduce intermediate result sets."})
			if len(insights) >= 3 {
				break
			}
		}
	}
	return insights
}

// checkWaitEvents analyzes common wait event patterns from pg_stat_activity.
// Per PG docs §27.2.3: wait_event_type and wait_event reveal bottlenecks.
func checkWaitEvents(snap *model.Snapshot) []Insight {
	waitCounts := make(map[string]int)
	for _, a := range snap.Activities {
		if a.State == "active" && a.WaitEventType != "" {
			key := a.WaitEventType + ":" + a.WaitEvent
			waitCounts[key]++
		}
	}

	var insights []Insight
	for key, count := range waitCounts {
		if count < 3 {
			continue
		}
		switch key {
		case "Lock:relation":
			insights = append(insights, Insight{Warning, "locks",
				fmt.Sprintf("%d backends waiting on relation locks", count),
				fmt.Sprintf("%d active backends are waiting to acquire relation-level locks.", count),
				"Identify blocking queries with pg_blocking_pids(). Consider reducing transaction scope or using advisory locks."})
		case "Lock:transactionid":
			insights = append(insights, Insight{Warning, "locks",
				fmt.Sprintf("%d backends waiting on transaction locks", count),
				fmt.Sprintf("%d active backends are waiting for other transactions to complete.", count),
				"Reduce transaction duration and lock contention. Check for long-running write transactions."})
		case "IO:DataFileRead":
			insights = append(insights, Insight{Info, "io",
				fmt.Sprintf("%d backends waiting on data file reads", count),
				fmt.Sprintf("%d active backends are waiting for data to be read from disk.", count),
				"Check disk I/O performance and consider increasing shared_buffers to reduce disk reads."})
		case "LWLock:WALInsert":
			insights = append(insights, Insight{Warning, "wal",
				fmt.Sprintf("%d backends waiting on WAL insert locks", count),
				fmt.Sprintf("%d active backends are contending for WAL insertion.", count),
				"Heavy WAL contention. Consider wal_buffers increase or reducing WAL-heavy operations."})
		case "LWLock:BufferMapping":
			insights = append(insights, Insight{Warning, "cache",
				fmt.Sprintf("%d backends waiting on buffer mapping", count),
				fmt.Sprintf("%d active backends are contending for buffer pool access.", count),
				"Buffer pool contention is high. Consider increasing shared_buffers."})
		}
	}
	return insights
}

// ---- Helpers ----

func formatBytes(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)
	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(GB))
	case bytes >= MB:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(MB))
	case bytes >= KB:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(KB))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

func formatNumber(n int64) string {
	if n >= 1_000_000_000 {
		return fmt.Sprintf("%.2fB", float64(n)/1_000_000_000)
	}
	if n >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	}
	if n >= 1_000 {
		return fmt.Sprintf("%.0fK", float64(n)/1_000)
	}
	return fmt.Sprintf("%d", n)
}

func formatTimePtr(t *time.Time) string {
	if t == nil {
		return "never"
	}
	elapsed := time.Since(*t)
	if elapsed < time.Hour {
		return fmt.Sprintf("%s ago", elapsed.Round(time.Second))
	}
	if elapsed < 24*time.Hour {
		return fmt.Sprintf("%.1f hours ago", elapsed.Hours())
	}
	return fmt.Sprintf("%.0f days ago", elapsed.Hours()/24)
}
