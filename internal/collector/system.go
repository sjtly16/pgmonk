package collector

import (
	"context"
	"strings"

	"github.com/shashank/pgmonk/internal/model"
)

// collectServerInfo gathers one-time server metadata.
func (c *Collector) collectServerInfo(ctx context.Context) (model.ServerInfo, error) {
	var info model.ServerInfo

	// Version string and numeric version.
	err := c.pool.QueryRow(ctx, "SELECT version(), current_setting('server_version_num')::int").
		Scan(&info.Version, &info.VersionNum)
	if err != nil {
		return info, err
	}

	// Server start time and max connections.
	err = c.pool.QueryRow(ctx, `
		SELECT
			pg_postmaster_start_time(),
			current_setting('max_connections')::int
	`).Scan(&info.StartTime, &info.MaxConnections)
	if err != nil {
		return info, err
	}

	// Data directory (may fail without superuser, that's okay).
	_ = c.pool.QueryRow(ctx, "SELECT current_setting('data_directory')").
		Scan(&info.DataDirectory)

	// Detect installed extensions.
	rows, err := c.pool.Query(ctx, "SELECT extname FROM pg_extension ORDER BY extname")
	if err != nil {
		return info, err
	}
	defer rows.Close()

	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			continue
		}
		info.Extensions = append(info.Extensions, name)
	}

	// Check for pg_stat_statements.
	for _, ext := range info.Extensions {
		if strings.EqualFold(ext, "pg_stat_statements") {
			info.HasStatStatements = true
			break
		}
	}

	// PG version-based capability detection.
	majorVersion := info.VersionNum / 10000

	// pg_stat_wal available since PG 14.
	info.HasStatWAL = majorVersion >= 14

	// pg_stat_io available since PG 16.
	info.HasStatIO = majorVersion >= 16

	// pg_stat_checkpointer separated from pg_stat_bgwriter in PG 17.
	info.HasStatCheckpointer = majorVersion >= 17

	// Detect if this is a standby/replica.
	var isInRecovery bool
	if err := c.pool.QueryRow(ctx, "SELECT pg_is_in_recovery()").Scan(&isInRecovery); err == nil {
		info.IsStandby = isInRecovery
	}

	// Check if track_io_timing is enabled.
	var trackIO string
	if err := c.pool.QueryRow(ctx, "SHOW track_io_timing").Scan(&trackIO); err == nil {
		info.TrackIOTiming = strings.EqualFold(trackIO, "on")
	}

	return info, nil
}

// collectSettings gathers performance-relevant PostgreSQL settings.
func (c *Collector) collectSettings(ctx context.Context) ([]model.Setting, error) {
	rows, err := c.pool.Query(ctx, `
		SELECT
			name,
			setting,
			COALESCE(unit, ''),
			COALESCE(category, ''),
			COALESCE(short_desc, ''),
			COALESCE(source, '')
		FROM pg_settings
		WHERE name IN (
			'shared_buffers', 'work_mem', 'maintenance_work_mem',
			'effective_cache_size', 'max_connections', 'max_worker_processes',
			'max_parallel_workers_per_gather', 'max_parallel_workers',
			'wal_buffers', 'checkpoint_completion_target', 'checkpoint_timeout',
			'max_wal_size', 'min_wal_size',
			'random_page_cost', 'seq_page_cost', 'effective_io_concurrency',
			'autovacuum', 'autovacuum_max_workers', 'autovacuum_naptime',
			'autovacuum_vacuum_threshold', 'autovacuum_vacuum_scale_factor',
			'autovacuum_analyze_threshold', 'autovacuum_analyze_scale_factor',
			'autovacuum_freeze_max_age', 'vacuum_freeze_table_age',
			'log_min_duration_statement', 'shared_preload_libraries',
			'track_activity_query_size', 'track_io_timing', 'track_wal_io_timing',
			'idle_in_transaction_session_timeout', 'statement_timeout',
			'wal_level', 'archive_mode', 'archive_command',
			'hot_standby', 'hot_standby_feedback',
			'synchronous_commit', 'synchronous_standby_names'
		)
		ORDER BY name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var settings []model.Setting
	for rows.Next() {
		var s model.Setting
		if err := rows.Scan(&s.Name, &s.Value, &s.Unit, &s.Category, &s.Description, &s.Source); err != nil {
			continue
		}
		settings = append(settings, s)
	}
	return settings, nil
}
