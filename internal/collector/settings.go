package collector

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sjtly16/pgmonk/internal/model"
)

// diagnosticSettings are the configuration parameters most often involved in
// performance problems. Names that don't exist on a version are just absent.
var diagnosticSettings = []string{
	"max_connections", "shared_buffers", "effective_cache_size", "work_mem",
	"maintenance_work_mem", "autovacuum_work_mem", "random_page_cost",
	"effective_io_concurrency", "max_parallel_workers_per_gather",
	"wal_buffers", "max_wal_size", "min_wal_size", "checkpoint_timeout",
	"checkpoint_completion_target", "synchronous_commit", "wal_compression",
	"autovacuum", "autovacuum_max_workers", "autovacuum_naptime",
	"autovacuum_vacuum_scale_factor", "autovacuum_vacuum_threshold",
	"autovacuum_vacuum_insert_scale_factor", "autovacuum_analyze_scale_factor",
	"autovacuum_vacuum_cost_limit", "autovacuum_vacuum_cost_delay",
	"autovacuum_freeze_max_age", "vacuum_cost_limit",
	"idle_in_transaction_session_timeout", "statement_timeout", "lock_timeout",
	"idle_session_timeout", "deadlock_timeout", "log_lock_waits",
	"track_io_timing", "track_wal_io_timing", "compute_query_id",
	"pg_stat_statements.track", "pg_stat_statements.track_planning",
	"default_statistics_target", "jit", "huge_pages", "temp_file_limit",
}

// collectSettings reads the diagnostic subset of pg_settings.
func (c *Collector) collectSettings(ctx context.Context) ([]model.Setting, error) {
	rows, err := c.query(ctx, `
		SELECT name, setting, COALESCE(unit, ''), category, short_desc, source, context
		FROM pg_settings
		WHERE name = ANY($1)
		ORDER BY name
	`, diagnosticSettings)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.Setting
	for rows.Next() {
		var s model.Setting
		if err := rows.Scan(&s.Name, &s.Value, &s.Unit, &s.Category, &s.Description, &s.Source, &s.Context); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ErrNotReadOnly is returned by ExplainReadOnly for statements it refuses to plan.
var ErrNotReadOnly = errors.New("only single SELECT/WITH statements can be verified")

// ExplainReadOnly verifies that a suggested query is valid on this server by
// planning it (EXPLAIN without ANALYZE, so nothing executes) inside a
// READ ONLY transaction with a short timeout. It returns the server's error
// for unknown views/columns, syntax errors, and so on.
func (c *Collector) ExplainReadOnly(ctx context.Context, sql string) error {
	q := strings.TrimSpace(sql)
	q = strings.TrimSuffix(q, ";")
	lower := strings.ToLower(q)
	isQuery := strings.HasPrefix(lower, "select") || strings.HasPrefix(lower, "with")
	if strings.Contains(q, ";") || !isQuery {
		return ErrNotReadOnly
	}

	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	tx, err := c.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err := tx.Exec(ctx, "SET LOCAL statement_timeout = '2s'"); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, selfTag+"EXPLAIN "+q)
	if err != nil {
		return err
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("%w", err)
	}
	return nil
}
