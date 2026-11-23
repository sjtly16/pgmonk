package collector

import (
	"context"
	"time"

	"github.com/sjtly16/pgmonk/internal/model"
)

// collectActivities gathers per-session data from pg_stat_activity.
func (c *Collector) collectActivities(ctx context.Context) ([]model.Activity, error) {
	rows, err := c.query(ctx, `
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
			query_start,
			backend_start,
			xact_start,
			state_change,
			`+c.leaderPIDColumn()+`,
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
		// Timestamps are NULL for many background processes; leave those as
		// the zero time.Time so callers can rely on IsZero().
		var queryStart, backendStart, xactStart, stateChange *time.Time
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
			&queryStart,
			&backendStart,
			&xactStart,
			&stateChange,
			&a.LeaderPID,
			&a.BackendType,
			&a.BackendXID,
			&a.BackendXmin,
		)
		if err != nil {
			continue
		}
		a.QueryStart = derefTime(queryStart)
		a.BackendStart = derefTime(backendStart)
		a.XactStart = derefTime(xactStart)
		a.StateChange = derefTime(stateChange)
		if !a.QueryStart.IsZero() {
			a.Duration = now.Sub(a.QueryStart)
		}
		activities = append(activities, a)
	}
	return activities, rows.Err()
}

func derefTime(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}
