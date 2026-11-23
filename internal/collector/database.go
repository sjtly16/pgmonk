package collector

import (
	"context"

	"github.com/sjtly16/pgmonk/internal/model"
)

// collectOverview gathers database-level aggregate metrics.
func (c *Collector) collectOverview(ctx context.Context) (model.Overview, *dbAbsoluteStats, error) {
	var o model.Overview
	var abs dbAbsoluteStats

	// Connection counts from pg_stat_activity.
	err := c.queryRow(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE state = 'active'),
			COUNT(*) FILTER (WHERE state = 'idle'),
			COUNT(*) FILTER (WHERE state = 'idle in transaction'),
			COUNT(*),
			current_setting('max_connections')::int
		FROM pg_stat_activity
		WHERE backend_type = 'client backend'
	`).Scan(
		&o.ActiveConnections,
		&o.IdleConnections,
		&o.IdleInTxConnections,
		&o.TotalConnections,
		&o.MaxConnections,
	)
	if err != nil {
		return o, nil, err
	}

	// Per-database stats from pg_stat_database.
	// PG 14+ added session_time, active_time, idle_in_transaction_time, sessions, etc.
	if c.serverInfo.VersionNum >= 140000 {
		err = c.queryRow(ctx, `
			SELECT
				d.datname,
				COALESCE(s.xact_commit, 0),
				COALESCE(s.xact_rollback, 0),
				COALESCE(s.tup_inserted, 0),
				COALESCE(s.tup_updated, 0),
				COALESCE(s.tup_deleted, 0),
				COALESCE(s.tup_fetched, 0),
				COALESCE(s.blks_hit, 0),
				COALESCE(s.blks_read, 0),
				COALESCE(s.deadlocks, 0),
				COALESCE(s.conflicts, 0),
				COALESCE(s.temp_files, 0),
				COALESCE(s.temp_bytes, 0),
				pg_database_size(d.datname),
				COALESCE(s.session_time, 0),
				COALESCE(s.active_time, 0),
				COALESCE(s.idle_in_transaction_time, 0),
				COALESCE(s.sessions, 0),
				COALESCE(s.sessions_abandoned, 0),
				COALESCE(s.sessions_fatal, 0),
				COALESCE(s.sessions_killed, 0)
			FROM pg_database d
			LEFT JOIN pg_stat_database s ON s.datid = d.oid
			WHERE d.datname = current_database()
		`).Scan(
			&o.DatabaseName,
			&abs.XactCommit, &abs.XactRollback,
			&abs.TupInserted, &abs.TupUpdated, &abs.TupDeleted, &abs.TupFetched,
			&abs.BlksHit, &abs.BlksRead,
			&o.Deadlocks, &o.Conflicts, &o.TempFiles, &o.TempBytes,
			&o.DatabaseSize,
			&o.SessionTime, &o.ActiveTime, &o.IdleInTxTime,
			&o.Sessions, &o.SessionsAbandoned, &o.SessionsFatal, &o.SessionsKilled,
		)
	} else {
		err = c.queryRow(ctx, `
			SELECT
				d.datname,
				COALESCE(s.xact_commit, 0),
				COALESCE(s.xact_rollback, 0),
				COALESCE(s.tup_inserted, 0),
				COALESCE(s.tup_updated, 0),
				COALESCE(s.tup_deleted, 0),
				COALESCE(s.tup_fetched, 0),
				COALESCE(s.blks_hit, 0),
				COALESCE(s.blks_read, 0),
				COALESCE(s.deadlocks, 0),
				COALESCE(s.conflicts, 0),
				COALESCE(s.temp_files, 0),
				COALESCE(s.temp_bytes, 0),
				pg_database_size(d.datname)
			FROM pg_database d
			LEFT JOIN pg_stat_database s ON s.datid = d.oid
			WHERE d.datname = current_database()
		`).Scan(
			&o.DatabaseName,
			&abs.XactCommit, &abs.XactRollback,
			&abs.TupInserted, &abs.TupUpdated, &abs.TupDeleted, &abs.TupFetched,
			&abs.BlksHit, &abs.BlksRead,
			&o.Deadlocks, &o.Conflicts, &o.TempFiles, &o.TempBytes,
			&o.DatabaseSize,
		)
	}
	if err != nil {
		return o, nil, err
	}

	// Compute cache hit ratio.
	totalBlks := abs.BlksHit + abs.BlksRead
	if totalBlks > 0 {
		o.CacheHitRatio = float64(abs.BlksHit) / float64(totalBlks) * 100.0
	}

	// Compute rollback ratio.
	totalTx := abs.XactCommit + abs.XactRollback
	if totalTx > 0 {
		o.RollbackRatio = float64(abs.XactRollback) / float64(totalTx) * 100.0
	}

	// Checkpoint stats — PG 17+ uses pg_stat_checkpointer, older uses pg_stat_bgwriter.
	if c.serverInfo.HasStatCheckpointer {
		_ = c.queryRow(ctx, `
			SELECT
				COALESCE(num_timed, 0),
				COALESCE(num_requested, 0),
				COALESCE(write_time, 0),
				COALESCE(sync_time, 0)
			FROM pg_stat_checkpointer
		`).Scan(
			&o.CheckpointsTimed,
			&o.CheckpointsReq,
			&o.CheckpointWriteTime,
			&o.CheckpointSyncTime,
		)
	} else {
		_ = c.queryRow(ctx, `
			SELECT
				COALESCE(checkpoints_timed, 0),
				COALESCE(checkpoints_req, 0),
				COALESCE(checkpoint_write_time, 0),
				COALESCE(checkpoint_sync_time, 0)
			FROM pg_stat_bgwriter
		`).Scan(
			&o.CheckpointsTimed,
			&o.CheckpointsReq,
			&o.CheckpointWriteTime,
			&o.CheckpointSyncTime,
		)
	}

	// Transaction ID wraparound monitoring.
	_ = c.queryRow(ctx, `
		SELECT
			COALESCE(age(datfrozenxid), 0),
			datname
		FROM pg_database
		WHERE datallowconn
		ORDER BY age(datfrozenxid) DESC
		LIMIT 1
	`).Scan(&o.DataFrozenXIDAge, &o.OldestXactDB)

	// Oldest running transaction age.
	_ = c.queryRow(ctx, `
		SELECT COALESCE(MAX(age(backend_xid)), 0)
		FROM pg_stat_activity
		WHERE backend_xid IS NOT NULL
	`).Scan(&o.OldestXactAge)

	return o, &abs, nil
}
