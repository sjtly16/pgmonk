package collector

import (
	"context"

	"github.com/sjtly16/pgmonk/internal/model"
)

// collectTables gathers per-table statistics from pg_stat_user_tables.
func (c *Collector) collectTables(ctx context.Context) ([]model.TableStat, error) {
	rows, err := c.query(ctx, `
		SELECT
			schemaname,
			relname,
			COALESCE(seq_scan, 0),
			COALESCE(seq_tup_read, 0),
			COALESCE(idx_scan, 0),
			COALESCE(idx_tup_fetch, 0),
			COALESCE(n_tup_ins, 0),
			COALESCE(n_tup_upd, 0),
			COALESCE(n_tup_del, 0),
			COALESCE(n_tup_hot_upd, 0),
			COALESCE(n_live_tup, 0),
			COALESCE(n_dead_tup, 0),
			COALESCE(n_mod_since_analyze, 0),
			`+c.insSinceVacuumColumn()+`,
			last_vacuum,
			last_autovacuum,
			last_analyze,
			last_autoanalyze,
			COALESCE(vacuum_count, 0),
			COALESCE(autovacuum_count, 0),
			COALESCE(analyze_count, 0),
			COALESCE(autoanalyze_count, 0),
			pg_relation_size(relid),
			pg_total_relation_size(relid)
		FROM pg_stat_user_tables
		ORDER BY n_dead_tup DESC
		LIMIT 100
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tables []model.TableStat
	for rows.Next() {
		var t model.TableStat
		err := rows.Scan(
			&t.SchemaName, &t.TableName,
			&t.SeqScan, &t.SeqTupRead,
			&t.IdxScan, &t.IdxTupFetch,
			&t.InsertCount, &t.UpdateCount, &t.DeleteCount,
			&t.HotUpdateCount,
			&t.LiveTuples, &t.DeadTuples,
			&t.ModsSinceAnalyze, &t.InsSinceVacuum,
			&t.LastVacuum, &t.LastAutoVacuum,
			&t.LastAnalyze, &t.LastAutoAnalyze,
			&t.VacuumCount, &t.AutoVacuumCount,
			&t.AnalyzeCount, &t.AutoAnalyzeCount,
			&t.TableSize, &t.TotalSize,
		)
		if err != nil {
			continue
		}

		// Compute dead tuple ratio.
		total := t.LiveTuples + t.DeadTuples
		if total > 0 {
			t.DeadTupleRatio = float64(t.DeadTuples) / float64(total) * 100.0
		}

		// Compute sequential scan ratio.
		totalScans := t.SeqScan + t.IdxScan
		if totalScans > 0 {
			t.SeqScanRatio = float64(t.SeqScan) / float64(totalScans) * 100.0
		}

		tables = append(tables, t)
	}
	return tables, nil
}

// collectIndexes gathers per-index statistics from pg_stat_user_indexes.
func (c *Collector) collectIndexes(ctx context.Context) ([]model.IndexStat, error) {
	rows, err := c.query(ctx, `
		SELECT
			s.schemaname,
			s.relname,
			s.indexrelname,
			COALESCE(s.idx_scan, 0),
			COALESCE(s.idx_tup_read, 0),
			COALESCE(s.idx_tup_fetch, 0),
			pg_relation_size(s.indexrelid),
			i.indisunique,
			i.indisvalid
		FROM pg_stat_user_indexes s
		JOIN pg_index i ON i.indexrelid = s.indexrelid
		ORDER BY s.idx_scan ASC, pg_relation_size(s.indexrelid) DESC
		LIMIT 100
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var indexes []model.IndexStat
	for rows.Next() {
		var idx model.IndexStat
		err := rows.Scan(
			&idx.SchemaName, &idx.TableName, &idx.IndexName,
			&idx.IndexScan, &idx.TupRead, &idx.TupFetch,
			&idx.IndexSize,
			&idx.IsUnique, &idx.IsValid,
		)
		if err != nil {
			continue
		}
		indexes = append(indexes, idx)
	}
	return indexes, nil
}

// insSinceVacuumColumn returns pg_stat_user_tables.n_ins_since_vacuum, which
// was added in PostgreSQL 13.
func (c *Collector) insSinceVacuumColumn() string {
	if c.serverInfo.VersionNum >= 130000 {
		return "COALESCE(n_ins_since_vacuum, 0)"
	}
	return "0::bigint"
}
