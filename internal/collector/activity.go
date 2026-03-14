package collector

import (
	"context"
	"time"

	"github.com/shashank/pgmonk/internal/model"
)

// collectActivities gathers per-session data from pg_stat_activity.
func (c *Collector) collectActivities(ctx context.Context) ([]model.Activity, error) {
	rows, err := c.pool.Query(ctx, `
		SELECT
			pid,
			COALESCE(datname, ''),
			COALESCE(usename, ''),
			COALESCE(application_name, ''),
			COALESCE(client_addr::text, 'local'),
			COALESCE(state, ''),
			COALESCE(wait_event_type, ''),
			COALESCE(wait_event, ''),
			COALESCE(query, ''),
			COALESCE(query_start, '1970-01-01'::timestamptz),
			COALESCE(backend_start, '1970-01-01'::timestamptz),
			COALESCE(xact_start, '1970-01-01'::timestamptz),
			COALESCE(state_change, '1970-01-01'::timestamptz),
			COALESCE(leader_pid, 0),
			COALESCE(backend_type, ''),
			COALESCE(backend_xid::text, ''),
			COALESCE(backend_xmin::text, '')
		FROM pg_stat_activity
		WHERE pid != pg_backend_pid()
		ORDER BY
			CASE state
				WHEN 'active' THEN 1
				WHEN 'idle in transaction' THEN 2
				WHEN 'idle in transaction (aborted)' THEN 3
				WHEN 'idle' THEN 4
				ELSE 5
			END,
			query_start ASC NULLS LAST
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	now := time.Now()
	var activities []model.Activity
	for rows.Next() {
		var a model.Activity
		err := rows.Scan(
			&a.PID,
			&a.DatabaseName,
			&a.UserName,
			&a.ApplicationName,
			&a.ClientAddr,
			&a.State,
			&a.WaitEventType,
			&a.WaitEvent,
			&a.Query,
			&a.QueryStart,
			&a.BackendStart,
			&a.XactStart,
			&a.StateChange,
			&a.LeaderPID,
			&a.BackendType,
			&a.BackendXID,
			&a.BackendXmin,
		)
		if err != nil {
			continue
		}
		if !a.QueryStart.IsZero() {
			a.Duration = now.Sub(a.QueryStart)
		}
		activities = append(activities, a)
	}
	return activities, nil
}
