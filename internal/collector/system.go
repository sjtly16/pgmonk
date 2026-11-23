package collector

import (
	"context"
	"strings"

	"github.com/sjtly16/pgmonk/internal/model"
)

// collectServerInfo gathers one-time server metadata.
func (c *Collector) collectServerInfo(ctx context.Context) (model.ServerInfo, error) {
	var info model.ServerInfo

	// Version string and numeric version.
	err := c.queryRow(ctx, "SELECT version(), current_setting('server_version_num')::int").
		Scan(&info.Version, &info.VersionNum)
	if err != nil {
		return info, err
	}

	// Server start time and max connections.
	err = c.queryRow(ctx, `
		SELECT
			pg_postmaster_start_time(),
			current_setting('max_connections')::int
	`).Scan(&info.StartTime, &info.MaxConnections)
	if err != nil {
		return info, err
	}

	// Data directory (may fail without superuser, that's okay).
	_ = c.queryRow(ctx, "SELECT current_setting('data_directory')").
		Scan(&info.DataDirectory)

	// Detect installed extensions.
	rows, err := c.query(ctx, "SELECT extname FROM pg_extension ORDER BY extname")
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
	if err := c.queryRow(ctx, "SELECT pg_is_in_recovery()").Scan(&isInRecovery); err == nil {
		info.IsStandby = isInRecovery
	}

	// Check if track_io_timing is enabled.
	var trackIO string
	if err := c.queryRow(ctx, "SHOW track_io_timing").Scan(&trackIO); err == nil {
		info.TrackIOTiming = strings.EqualFold(trackIO, "on")
	}

	return info, nil
}
