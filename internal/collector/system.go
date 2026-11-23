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

	// Capabilities come from the live catalog when we can read it, falling
	// back to version numbers otherwise.
	if cat, err := c.collectCatalog(ctx); err == nil && len(cat) > 0 {
		info.Catalog = cat
		info.HasStatWAL = cat.HasView("pg_stat_wal")
		info.HasStatIO = cat.HasView("pg_stat_io")
		info.HasStatCheckpointer = cat.HasView("pg_stat_checkpointer")
		info.HasStatStatements = info.HasStatStatements && cat.HasView("pg_stat_statements")
	} else {
		major := info.VersionNum / 10000
		info.HasStatWAL = major >= 14
		info.HasStatIO = major >= 16
		info.HasStatCheckpointer = major >= 17
	}

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

// collectCatalog reads the columns of every statistics view in pg_catalog,
// plus pg_stat_statements wherever the extension is installed.
func (c *Collector) collectCatalog(ctx context.Context) (model.Catalog, error) {
	rows, err := c.query(ctx, `
		SELECT c.relname, a.attname
		FROM pg_catalog.pg_class c
		JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
		JOIN pg_catalog.pg_attribute a ON a.attrelid = c.oid
		WHERE a.attnum > 0 AND NOT a.attisdropped
		  AND (
		        (n.nspname = 'pg_catalog'
		         AND (c.relname LIKE 'pg\_stat\_%' OR c.relname LIKE 'pg\_statio\_%' OR c.relname = 'pg_locks'))
		     OR c.oid = to_regclass('pg_stat_statements')
		  )
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cat := model.Catalog{}
	for rows.Next() {
		var view, col string
		if err := rows.Scan(&view, &col); err != nil {
			return nil, err
		}
		cat.Add(view, col)
	}
	return cat, rows.Err()
}

// has reports whether view.column exists on the server. When the catalog
// could not be read it falls back to the minimum server version given.
func (c *Collector) has(view, column string, minVersion int) bool {
	if c.serverInfo.Catalog != nil {
		return c.serverInfo.Catalog.Has(view, column)
	}
	return c.serverInfo.VersionNum >= minVersion
}

// colOr returns expr if view.column exists, otherwise fallback.
func (c *Collector) colOr(view, column string, minVersion int, expr, fallback string) string {
	if c.has(view, column, minVersion) {
		return expr
	}
	return fallback
}
