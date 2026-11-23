package collector

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/sjtly16/pgmonk/internal/model"
)

// collectLocks gathers active locks and identifies blocking chains.
func (c *Collector) collectLocks(ctx context.Context) ([]model.LockInfo, error) {
	// pg_locks.waitstart exists from PG 14; fall back to the backend's
	// state_change (when it started waiting) on older servers.
	waitStart := "a.state_change"
	if c.serverInfo.VersionNum >= 140000 {
		waitStart = "COALESCE(l.waitstart, a.state_change)"
	}

	// pg_blocking_pids() is comparatively expensive, so only call it for
	// locks that are actually waiting.
	rows, err := c.query(ctx, `
		SELECT
			l.pid,
			l.locktype,
			COALESCE(d.datname, ''),
			COALESCE(c.relname, ''),
			l.mode,
			l.granted,
			CASE WHEN l.granted THEN NULL ELSE `+waitStart+` END,
			CASE WHEN l.granted THEN ARRAY[]::int[] ELSE pg_blocking_pids(l.pid) END,
			COALESCE(a.query, ''),
			COALESCE(a.usename, '')
		FROM pg_locks l
		LEFT JOIN pg_database d ON d.oid = l.database
		LEFT JOIN pg_class c ON c.oid = l.relation
		LEFT JOIN pg_stat_activity a ON a.pid = l.pid
		WHERE l.pid != pg_backend_pid()
		ORDER BY l.granted ASC, a.query_start ASC
	`)
	if err != nil {
		// pg_blocking_pids is unavailable before PG 9.6.
		return c.collectLocksLegacy(ctx)
	}
	defer rows.Close()

	var locks []model.LockInfo
	for rows.Next() {
		var li model.LockInfo
		err := rows.Scan(
			&li.PID,
			&li.LockType,
			&li.DatabaseName,
			&li.RelationName,
			&li.Mode,
			&li.Granted,
			&li.WaitStart,
			&li.BlockedByPIDs,
			&li.Query,
			&li.UserName,
		)
		if err != nil {
			continue
		}
		if li.WaitStart != nil && !li.Granted {
			li.Duration = time.Since(*li.WaitStart)
		}
		locks = append(locks, li)
	}

	return locks, rows.Err()
}

// collectLocksLegacy is a fallback for PostgreSQL versions without pg_blocking_pids.
func (c *Collector) collectLocksLegacy(ctx context.Context) ([]model.LockInfo, error) {
	rows, err := c.query(ctx, `
		SELECT
			l.pid,
			l.locktype,
			COALESCE(d.datname, ''),
			COALESCE(c.relname, ''),
			l.mode,
			l.granted,
			COALESCE(a.query, ''),
			COALESCE(a.usename, '')
		FROM pg_locks l
		LEFT JOIN pg_database d ON d.oid = l.database
		LEFT JOIN pg_class c ON c.oid = l.relation
		LEFT JOIN pg_stat_activity a ON a.pid = l.pid
		WHERE l.pid != pg_backend_pid()
		ORDER BY l.granted ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var locks []model.LockInfo
	for rows.Next() {
		var li model.LockInfo
		err := rows.Scan(
			&li.PID,
			&li.LockType,
			&li.DatabaseName,
			&li.RelationName,
			&li.Mode,
			&li.Granted,
			&li.Query,
			&li.UserName,
		)
		if err != nil {
			continue
		}
		locks = append(locks, li)
	}

	return locks, nil
}

// collectReplication gathers replication status from pg_stat_replication.
func (c *Collector) collectReplication(ctx context.Context) ([]model.ReplicationStat, error) {
	rows, err := c.query(ctx, `
		SELECT
			pid,
			COALESCE(usename, ''),
			COALESCE(application_name, ''),
			COALESCE(client_addr::text, 'local'),
			COALESCE(state, ''),
			COALESCE(sent_lsn::text, ''),
			COALESCE(write_lsn::text, ''),
			COALESCE(flush_lsn::text, ''),
			COALESCE(replay_lsn::text, ''),
			write_lag,
			flush_lag,
			replay_lag,
			COALESCE(sync_state, '')
		FROM pg_stat_replication
		ORDER BY application_name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var stats []model.ReplicationStat
	for rows.Next() {
		var r model.ReplicationStat
		err := rows.Scan(
			&r.PID,
			&r.UserName,
			&r.ApplicationName,
			&r.ClientAddr,
			&r.State,
			&r.SentLSN,
			&r.WriteLSN,
			&r.FlushLSN,
			&r.ReplayLSN,
			&r.WriteLag,
			&r.FlushLag,
			&r.ReplayLag,
			&r.SyncState,
		)
		if err != nil {
			continue
		}
		stats = append(stats, r)
	}

	if len(stats) == 0 {
		return nil, pgx.ErrNoRows
	}
	return stats, nil
}
