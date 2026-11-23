package knowledge

import (
	"fmt"
	"strings"

	"github.com/sjtly16/pgmonk/internal/model"
)

// packViews are the views whose columns go into the pack, in output order.
// Kept to what pg-monk collects plus views a DBA would query next, so the
// pack fits small local models' context windows.
var packViews = []string{
	"pg_stat_activity",
	"pg_stat_database",
	"pg_stat_statements",
	"pg_stat_user_tables",
	"pg_statio_user_tables",
	"pg_stat_user_indexes",
	"pg_stat_progress_vacuum",
	"pg_stat_bgwriter",
	"pg_stat_checkpointer",
	"pg_stat_wal",
	"pg_stat_io",
	"pg_stat_archiver",
	"pg_stat_replication",
	"pg_stat_database_conflicts",
	"pg_stat_lock",
	"pg_stat_autovacuum_scores",
}

// skipColumns are low-value-for-diagnosis columns dropped to save tokens.
var skipColumns = map[string]bool{
	"datid": true, "relid": true, "indexrelid": true, "usesysid": true, "userid": true, "dbid": true,
	"client_hostname": true, "client_port": true, "backend_xid": true,
	"toplevel": true, "stats_since": true, "minmax_stats_since": true,
}

// describe lists the columns whose documentation is included verbatim
// (shortened). Every other column is listed by name only: models generally
// understand names like n_dead_tup, and what they get wrong is which columns
// exist, which the name list settles. Keeps the pack around 2k tokens.
var describe = map[string]bool{
	"pg_stat_activity.backend_xmin":             true,
	"pg_stat_activity.wait_event_type":          true,
	"pg_stat_database.blks_read":                true,
	"pg_stat_database.temp_bytes":               true,
	"pg_stat_database.conflicts":                true,
	"pg_stat_database.idle_in_transaction_time": true,
	"pg_stat_statements.shared_blks_dirtied":    true,
	"pg_stat_statements.temp_blks_written":      true,
	"pg_stat_statements.wal_fpi":                true,
	"pg_stat_all_tables.n_mod_since_analyze":    true,
	"pg_stat_all_tables.n_ins_since_vacuum":     true,
	"pg_stat_all_tables.n_tup_hot_upd":          true,
	"pg_stat_all_tables.n_tup_newpage_upd":      true,
	"pg_stat_checkpointer.num_done":             true,
	"pg_stat_checkpointer.num_requested":        true,
	"pg_stat_bgwriter.maxwritten_clean":         true,
	"pg_stat_bgwriter.buffers_backend_fsync":    true,
	"pg_stat_wal.wal_buffers_full":              true,
	"pg_stat_wal.wal_fpi":                       true,
	"pg_stat_io.context":                        true,
	"pg_stat_io.evictions":                      true,
	"pg_stat_io.reuses":                         true,
	"pg_stat_replication.replay_lag":            true,
	"pg_stat_archiver.failed_count":             true,
	"pg_stat_progress_vacuum.phase":             true,
}

// maxDescLen caps each included description.
const maxDescLen = 90

// Pack is the rendered, version-specific knowledge for one server.
type Pack struct {
	Text      string // stable for a given server — safe to use as a cached prompt prefix
	Major     int    // server major version
	RefMajor  int    // reference version used
	Exact     bool   // RefMajor == Major
	Reference *Reference
	Catalog   model.Catalog
}

// BuildPack renders the knowledge pack for a server. Output is deterministic
// for the same server (no timestamps, fixed ordering) so local LLM servers can
// reuse their KV cache for it across requests.
func BuildPack(info model.ServerInfo) (*Pack, error) {
	major := info.VersionNum / 10000
	ref, exact, err := ForServer(major)
	if err != nil {
		return nil, err
	}
	notes, err := Notes()
	if err != nil {
		return nil, err
	}

	var b strings.Builder
	fmt.Fprintf(&b, "## PostgreSQL %d knowledge\n", major)
	if !exact {
		fmt.Fprintf(&b, "No reference ships for PostgreSQL %d; using PostgreSQL %d docs. Column lists below come from the live server.\n", major, ref.Major)
	}

	b.WriteString("\n### Version notes (authoritative for this server)\n")
	for _, n := range notes {
		if n.Applies(major) {
			b.WriteString("- ")
			b.WriteString(n.render())
			b.WriteString("\n")
		}
	}

	b.WriteString("\n### Statistics columns that exist on this server\n")
	b.WriteString("Only these columns exist; do not reference any other columns of these views. ")
	b.WriteString("Counters are cumulative since stats_reset; *_time columns are milliseconds; *_blks_* are 8kB blocks.\n")
	for _, view := range packViews {
		cols := packColumns(view, ref, info.Catalog)
		if len(cols) == 0 {
			continue
		}
		b.WriteString("\n")
		b.WriteString(view)
		b.WriteString(": ")
		b.WriteString(strings.Join(cols, ", "))
		b.WriteString("\n")
	}

	return &Pack{
		Text:      b.String(),
		Major:     major,
		RefMajor:  ref.Major,
		Exact:     exact,
		Reference: ref,
		Catalog:   info.Catalog,
	}, nil
}

// packColumns lists a view's columns as "name (description)". With a live
// catalog, only columns present on the server are listed, in documentation
// order, followed by any undocumented ones (newer minor or fork additions).
func packColumns(view string, ref *Reference, cat model.Catalog) []string {
	if cat != nil && !cat.HasView(view) {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	for _, c := range ref.Views[docView(view)] {
		seen[c.Name] = true
		if skipColumns[c.Name] || (cat != nil && !cat.Has(view, c.Name)) {
			continue
		}
		if describe[docView(view)+"."+c.Name] && c.Desc != "" {
			out = append(out, fmt.Sprintf("%s (%s)", c.Name, shorten(c.Desc)))
		} else {
			out = append(out, c.Name)
		}
	}
	for _, col := range cat.Columns(view) {
		if !seen[col] && !skipColumns[col] {
			out = append(out, col)
		}
	}
	return out
}

func shorten(s string) string {
	s = strings.TrimSuffix(strings.TrimSpace(s), ".")
	if r := []rune(s); len(r) > maxDescLen {
		cut := string(r[:maxDescLen])
		if i := strings.LastIndex(cut, " "); i > maxDescLen/2 {
			cut = cut[:i]
		}
		s = cut + "…"
	}
	return s
}
