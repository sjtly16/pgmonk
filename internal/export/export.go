package export

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/sjtly16/pgmonk/internal/model"
)

// WriteJSON writes the snapshot as pretty-printed JSON.
func WriteJSON(w io.Writer, snap *model.Snapshot) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(snap)
}

// WriteCSVOverview writes the overview metrics as CSV.
func WriteCSVOverview(w io.Writer, snap *model.Snapshot) error {
	cw := csv.NewWriter(w)
	defer cw.Flush()

	// Header.
	if err := cw.Write([]string{
		"collected_at", "database", "active_connections", "total_connections", "max_connections",
		"cache_hit_ratio", "tx_commit_rate", "tx_rollback_rate", "deadlocks",
		"database_size_bytes", "temp_files", "temp_bytes",
		"rollback_ratio", "xid_age",
		"checkpoints_timed", "checkpoints_req",
	}); err != nil {
		return err
	}

	o := snap.Overview
	return cw.Write([]string{
		snap.CollectedAt.Format(time.RFC3339),
		o.DatabaseName,
		fmt.Sprintf("%d", o.ActiveConnections),
		fmt.Sprintf("%d", o.TotalConnections),
		fmt.Sprintf("%d", o.MaxConnections),
		fmt.Sprintf("%.4f", o.CacheHitRatio),
		fmt.Sprintf("%.2f", o.TxCommitRate),
		fmt.Sprintf("%.2f", o.TxRollbackRate),
		fmt.Sprintf("%d", o.Deadlocks),
		fmt.Sprintf("%d", o.DatabaseSize),
		fmt.Sprintf("%d", o.TempFiles),
		fmt.Sprintf("%d", o.TempBytes),
		fmt.Sprintf("%.2f", o.RollbackRatio),
		fmt.Sprintf("%d", o.DataFrozenXIDAge),
		fmt.Sprintf("%d", o.CheckpointsTimed),
		fmt.Sprintf("%d", o.CheckpointsReq),
	})
}

// WriteCSVQueries writes the top queries as CSV.
func WriteCSVQueries(w io.Writer, snap *model.Snapshot) error {
	cw := csv.NewWriter(w)
	defer cw.Flush()

	if err := cw.Write([]string{
		"query_id", "calls", "total_exec_time_ms", "mean_exec_time_ms",
		"rows", "cache_hit_ratio", "query",
	}); err != nil {
		return err
	}

	for _, q := range snap.Queries {
		if err := cw.Write([]string{
			fmt.Sprintf("%d", q.QueryID),
			fmt.Sprintf("%d", q.Calls),
			fmt.Sprintf("%.2f", q.TotalExecTime),
			fmt.Sprintf("%.2f", q.MeanExecTime),
			fmt.Sprintf("%d", q.Rows),
			fmt.Sprintf("%.2f", q.CacheHitRatio),
			q.Query,
		}); err != nil {
			return err
		}
	}
	return nil
}
