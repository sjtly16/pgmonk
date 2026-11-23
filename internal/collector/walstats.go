package collector

import (
	"context"

	"github.com/sjtly16/pgmonk/internal/model"
)

// collectWALStats gathers WAL generation metrics from pg_stat_wal (PG 14+).
//
// PG 18 removed wal_write, wal_sync, wal_write_time and wal_sync_time from
// pg_stat_wal; the same numbers now live in pg_stat_io rows with object = 'wal'.
func (c *Collector) collectWALStats(ctx context.Context) (*model.WALStats, error) {
	ioCols := `
			COALESCE(wal_write, 0),
			COALESCE(wal_sync, 0),
			COALESCE(wal_write_time, 0),
			COALESCE(wal_sync_time, 0),`
	if !c.has("pg_stat_wal", "wal_write", 140000) {
		ioCols = `
			COALESCE((SELECT sum(writes) FROM pg_stat_io WHERE object = 'wal'), 0)::bigint,
			COALESCE((SELECT sum(fsyncs) FROM pg_stat_io WHERE object = 'wal'), 0)::bigint,
			COALESCE((SELECT sum(write_time) FROM pg_stat_io WHERE object = 'wal'), 0)::float8,
			COALESCE((SELECT sum(fsync_time) FROM pg_stat_io WHERE object = 'wal'), 0)::float8,`
	}

	var w model.WALStats
	err := c.queryRow(ctx, `
		SELECT
			COALESCE(wal_records, 0),
			COALESCE(wal_fpi, 0),
			COALESCE(wal_bytes, 0),
			COALESCE(wal_buffers_full, 0),`+ioCols+`
			stats_reset
		FROM pg_stat_wal
	`).Scan(
		&w.WALRecords, &w.WALFPI, &w.WALBytes, &w.WALBuffersFull,
		&w.WALWrite, &w.WALSync, &w.WALWriteTime, &w.WALSyncTime,
		&w.StatsReset,
	)
	if err != nil {
		return nil, err
	}
	return &w, nil
}

// collectCheckpointerStats gathers checkpoint process stats from pg_stat_checkpointer (PG 17+).
func (c *Collector) collectCheckpointerStats(ctx context.Context) (*model.CheckpointerStats, error) {
	// num_done was added in PG 18.
	numDone := c.colOr("pg_stat_checkpointer", "num_done", 180000, "COALESCE(num_done, 0)", "0::bigint")
	var s model.CheckpointerStats
	err := c.queryRow(ctx, `
		SELECT
			COALESCE(num_timed, 0),
			COALESCE(num_requested, 0),
			`+numDone+`,
			COALESCE(restartpoints_timed, 0),
			COALESCE(restartpoints_req, 0),
			COALESCE(restartpoints_done, 0),
			COALESCE(write_time, 0),
			COALESCE(sync_time, 0),
			COALESCE(buffers_written, 0),
			stats_reset
		FROM pg_stat_checkpointer
	`).Scan(
		&s.NumTimed, &s.NumRequested, &s.NumDone,
		&s.RestartpointsTimed, &s.RestartpointsReq, &s.RestartpointsDone,
		&s.WriteTime, &s.SyncTime, &s.BuffersWritten,
		&s.StatsReset,
	)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// collectBGWriterStats gathers background writer stats from pg_stat_bgwriter.
func (c *Collector) collectBGWriterStats(ctx context.Context) (*model.BGWriterStats, error) {
	var s model.BGWriterStats

	// PG 17+ separated checkpointer stats; bgwriter only has buffers_clean, maxwritten_clean, buffers_alloc.
	// Older PG has checkpoint fields too, but we pull those in collectOverview already.
	err := c.queryRow(ctx, `
		SELECT
			COALESCE(buffers_clean, 0),
			COALESCE(maxwritten_clean, 0),
			COALESCE(buffers_alloc, 0),
			stats_reset
		FROM pg_stat_bgwriter
	`).Scan(
		&s.BuffersClean, &s.MaxWrittenClean, &s.BuffersAlloc,
		&s.StatsReset,
	)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// collectArchiverStats gathers WAL archiver stats from pg_stat_archiver.
func (c *Collector) collectArchiverStats(ctx context.Context) (*model.ArchiverStats, error) {
	var a model.ArchiverStats
	err := c.queryRow(ctx, `
		SELECT
			COALESCE(archived_count, 0),
			COALESCE(last_archived_wal, ''),
			last_archived_time,
			COALESCE(failed_count, 0),
			COALESCE(last_failed_wal, ''),
			last_failed_time,
			stats_reset
		FROM pg_stat_archiver
	`).Scan(
		&a.ArchivedCount, &a.LastArchivedWAL, &a.LastArchivedTime,
		&a.FailedCount, &a.LastFailedWAL, &a.LastFailedTime,
		&a.StatsReset,
	)
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// collectIOStats gathers per-backend I/O stats from pg_stat_io (PG 16+).
func (c *Collector) collectIOStats(ctx context.Context) ([]model.IOStat, error) {
	rows, err := c.query(ctx, `
		SELECT
			backend_type,
			object,
			context,
			COALESCE(reads, 0),
			COALESCE(read_time, 0),
			COALESCE(writes, 0),
			COALESCE(write_time, 0),
			COALESCE(extends, 0),
			COALESCE(extend_time, 0),
			COALESCE(hits, 0),
			COALESCE(evictions, 0),
			COALESCE(reuses, 0),
			COALESCE(fsyncs, 0),
			COALESCE(fsync_time, 0),
			stats_reset
		FROM pg_stat_io
		WHERE reads > 0 OR writes > 0 OR hits > 0
		ORDER BY reads + writes + hits DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var stats []model.IOStat
	for rows.Next() {
		var s model.IOStat
		if err := rows.Scan(
			&s.BackendType, &s.Object, &s.Context,
			&s.Reads, &s.ReadTime, &s.Writes, &s.WriteTime,
			&s.Extends, &s.ExtendTime,
			&s.Hits, &s.Evictions, &s.Reuses,
			&s.Fsyncs, &s.FsyncTime, &s.StatsReset,
		); err != nil {
			continue
		}
		stats = append(stats, s)
	}
	return stats, nil
}

// collectConflictStats gathers query cancellation stats (relevant on standbys).
func (c *Collector) collectConflictStats(ctx context.Context) (*model.ConflictStats, error) {
	var s model.ConflictStats

	query := `
		SELECT
			COALESCE(SUM(confl_tablespace), 0),
			COALESCE(SUM(confl_lock), 0),
			COALESCE(SUM(confl_snapshot), 0),
			COALESCE(SUM(confl_bufferpin), 0),
			COALESCE(SUM(confl_deadlock), 0)
		FROM pg_stat_database_conflicts
	`
	err := c.queryRow(ctx, query).Scan(
		&s.ConflTablespace, &s.ConflLock, &s.ConflSnapshot,
		&s.ConflBufferpin, &s.ConflDeadlock,
	)
	if err != nil {
		return nil, err
	}

	// confl_active_logicalslot added in PG 15.
	if c.has("pg_stat_database_conflicts", "confl_active_logicalslot", 160000) {
		_ = c.queryRow(ctx, `
			SELECT COALESCE(SUM(confl_active_logicalslot), 0)
			FROM pg_stat_database_conflicts
		`).Scan(&s.ConflLogicalSlot)
	}

	return &s, nil
}

// collectTableIOStats gathers table-level I/O stats from pg_statio_user_tables.
func (c *Collector) collectTableIOStats(ctx context.Context) ([]model.TableIOStat, error) {
	rows, err := c.query(ctx, `
		SELECT
			schemaname,
			relname,
			COALESCE(heap_blks_read, 0),
			COALESCE(heap_blks_hit, 0),
			COALESCE(idx_blks_read, 0),
			COALESCE(idx_blks_hit, 0),
			COALESCE(toast_blks_read, 0),
			COALESCE(toast_blks_hit, 0),
			COALESCE(tidx_blks_read, 0),
			COALESCE(tidx_blks_hit, 0)
		FROM pg_statio_user_tables
		WHERE (heap_blks_read + heap_blks_hit + COALESCE(idx_blks_read, 0) + COALESCE(idx_blks_hit, 0)) > 0
		ORDER BY (heap_blks_read + COALESCE(idx_blks_read, 0)) DESC
		LIMIT 50
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var stats []model.TableIOStat
	for rows.Next() {
		var s model.TableIOStat
		if err := rows.Scan(
			&s.SchemaName, &s.TableName,
			&s.HeapBlksRead, &s.HeapBlksHit,
			&s.IdxBlksRead, &s.IdxBlksHit,
			&s.ToastBlksRead, &s.ToastBlksHit,
			&s.TidxBlksRead, &s.TidxBlksHit,
		); err != nil {
			continue
		}
		totalHit := s.HeapBlksHit + s.IdxBlksHit + s.ToastBlksHit + s.TidxBlksHit
		totalRead := s.HeapBlksRead + s.IdxBlksRead + s.ToastBlksRead + s.TidxBlksRead
		total := totalHit + totalRead
		if total > 0 {
			s.CacheHitRatio = float64(totalHit) / float64(total) * 100.0
		}
		stats = append(stats, s)
	}
	return stats, nil
}

// collectVacuumProgress gathers live vacuum progress from pg_stat_progress_vacuum.
func (c *Collector) collectVacuumProgress(ctx context.Context) ([]model.VacuumProgress, error) {
	rows, err := c.query(ctx, `
		SELECT
			p.pid,
			COALESCE(d.datname, ''),
			COALESCE(n.nspname, ''),
			COALESCE(c.relname, ''),
			CASE p.param1
				WHEN 1 THEN 'initializing'
				WHEN 2 THEN 'scanning heap'
				WHEN 3 THEN 'vacuuming indexes'
				WHEN 4 THEN 'vacuuming heap'
				WHEN 5 THEN 'cleaning up indexes'
				WHEN 6 THEN 'truncating heap'
				WHEN 7 THEN 'performing final cleanup'
				ELSE 'unknown'
			END,
			COALESCE(p.param2, 0),
			COALESCE(p.param3, 0),
			COALESCE(p.param4, 0),
			COALESCE(p.param5, 0),
			COALESCE(p.param6, 0),
			COALESCE(p.param7, 0)
		FROM pg_stat_progress_vacuum p
		LEFT JOIN pg_database d ON d.oid = p.datid
		LEFT JOIN pg_class c ON c.oid = p.relid
		LEFT JOIN pg_namespace n ON n.oid = c.relnamespace
	`)
	if err != nil {
		// pg_stat_progress_vacuum might not exist on very old versions.
		return nil, nil
	}
	defer rows.Close()

	var progress []model.VacuumProgress
	for rows.Next() {
		var v model.VacuumProgress
		if err := rows.Scan(
			&v.PID, &v.DatabaseName, &v.TableSchema, &v.TableName,
			&v.Phase,
			&v.HeapBlksTotal, &v.HeapBlksScanned, &v.HeapBlksVacuumed,
			&v.IndexVacuumCount, &v.MaxDeadTuples, &v.NumDeadTuples,
		); err != nil {
			continue
		}
		progress = append(progress, v)
	}
	return progress, nil
}
