package collector

import (
	"context"
	"fmt"

	"github.com/sjtly16/pgmonk/internal/model"
)

// collectQueries gathers the top queries from pg_stat_statements.
// Version-aware: PG 13+ has WAL stats, PG 15+ has JIT stats.
func (c *Collector) collectQueries(ctx context.Context) ([]model.QueryStat, error) {
	ver := c.serverInfo.VersionNum

	walCols := "0::bigint, 0::bigint, 0::bigint"
	jitCols := "0::bigint, 0::float8"
	localCols := "COALESCE(s.local_blks_hit, 0), COALESCE(s.local_blks_read, 0)"

	// PG 13 renamed the *_time columns to *_exec_time and added planning stats.
	execCols := "s.total_exec_time, s.mean_exec_time, s.min_exec_time, s.max_exec_time"
	planCols := "COALESCE(s.total_plan_time, 0), COALESCE(s.mean_plan_time, 0), COALESCE(s.stddev_exec_time, 0)"
	orderCol := "s.total_exec_time"
	if ver < 130000 {
		execCols = "s.total_time, s.mean_time, s.min_time, s.max_time"
		planCols = "0::float8, 0::float8, COALESCE(s.stddev_time, 0)"
		orderCol = "s.total_time"
	}

	if ver >= 130000 {
		walCols = "COALESCE(s.wal_records, 0), COALESCE(s.wal_bytes, 0), COALESCE(s.wal_fpi, 0)"
	}
	if ver >= 150000 {
		jitCols = "COALESCE(s.jit_functions, 0), COALESCE(s.jit_generation_time + s.jit_inlining_time + s.jit_optimization_time + s.jit_emission_time, 0)"
	}

	query := fmt.Sprintf(`
		SELECT
			s.queryid,
			LEFT(s.query, 500),
			COALESCE(d.datname, ''),
			COALESCE(u.rolname, ''),
			s.calls,
			%s,
			s.rows,
			s.shared_blks_hit,
			s.shared_blks_read,
			%s,
			COALESCE(s.temp_blks_read, 0),
			COALESCE(s.temp_blks_written, 0),
			%s,
			%s,
			%s
		FROM pg_stat_statements s
		LEFT JOIN pg_database d ON d.oid = s.dbid
		LEFT JOIN pg_roles u ON u.oid = s.userid
		WHERE s.calls > 0
			AND d.datname = current_database()
			AND s.query NOT LIKE '/* pg-monk */%%'
		ORDER BY %s DESC
		LIMIT 50
	`, execCols, planCols, localCols, walCols, jitCols, orderCol)

	rows, err := c.query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var queries []model.QueryStat
	for rows.Next() {
		var q model.QueryStat
		err := rows.Scan(
			&q.QueryID, &q.Query, &q.DatabaseName, &q.UserName,
			&q.Calls, &q.TotalExecTime, &q.MeanExecTime, &q.MinExecTime, &q.MaxExecTime,
			&q.Rows, &q.SharedBlksHit, &q.SharedBlksRead,
			&q.TotalPlanTime, &q.MeanPlanTime, &q.StddevExecTime,
			&q.TempBlksRead, &q.TempBlksWritten,
			&q.LocalBlksHit, &q.LocalBlksRead,
			&q.WALRecords, &q.WALBytes, &q.WALFPI,
			&q.JITFunctions, &q.JITTime,
		)
		if err != nil {
			continue
		}

		totalBlks := q.SharedBlksHit + q.SharedBlksRead
		if totalBlks > 0 {
			q.CacheHitRatio = float64(q.SharedBlksHit) / float64(totalBlks) * 100.0
		}

		queries = append(queries, q)
	}

	return queries, nil
}
