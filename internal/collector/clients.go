package collector

import (
	"context"
	"sort"
	"time"

	"github.com/sjtly16/pgmonk/internal/model"
)

// collectClients gathers per-client-address aggregated metrics and per-backend thread details.
func (c *Collector) collectClients(ctx context.Context) ([]model.ClientStat, error) {
	rows, err := c.query(ctx, `
		SELECT
			pid,
			COALESCE(client_addr::text, 'local'),
			COALESCE(datname, ''),
			COALESCE(usename, ''),
			COALESCE(application_name, ''),
			COALESCE(backend_type, 'client backend'),
			COALESCE(state, 'unknown'),
			COALESCE(wait_event_type, ''),
			COALESCE(wait_event, ''),
			COALESCE(query, ''),
			query_start,
			xact_start,
			COALESCE(backend_start, now()),
			COALESCE(state_change, now()),
			`+c.leaderPIDColumn()+`
		FROM pg_stat_activity
		WHERE pid != pg_backend_pid()
			-- Only real client connections; background processes
			-- (checkpointer, walwriter, autovacuum, parallel workers, ...)
			-- have no client and would otherwise be lumped under "local".
			AND COALESCE(backend_type, 'client backend') = 'client backend'
		ORDER BY client_addr, backend_start
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	now := time.Now()

	// Collect all backends first, then aggregate by client.
	type backendRow struct {
		backend model.BackendThread
	}
	var backends []backendRow

	for rows.Next() {
		var b model.BackendThread
		var queryStart, xactStart *time.Time

		err := rows.Scan(
			&b.PID,
			&b.ClientAddr,
			&b.DatabaseName,
			&b.UserName,
			&b.ApplicationName,
			&b.BackendType,
			&b.State,
			&b.WaitEventType,
			&b.WaitEvent,
			&b.Query,
			&queryStart,
			&xactStart,
			&b.BackendStart,
			&b.StateChange,
			&b.LeaderPID,
		)
		if err != nil {
			continue
		}

		// Compute durations.
		if queryStart != nil {
			b.QueryStart = *queryStart
			if b.State == "active" {
				b.QueryDuration = now.Sub(*queryStart)
			}
		}
		if xactStart != nil {
			b.XactStart = *xactStart
			b.TxDuration = now.Sub(*xactStart)
		}
		b.Uptime = now.Sub(b.BackendStart)

		backends = append(backends, backendRow{backend: b})
	}

	// Aggregate by client address.
	clientMap := make(map[string]*model.ClientStat)
	for _, br := range backends {
		b := br.backend
		cs, exists := clientMap[b.ClientAddr]
		if !exists {
			cs = &model.ClientStat{
				ClientAddr: b.ClientAddr,
			}
			clientMap[b.ClientAddr] = cs
		}

		cs.TotalConnections++
		cs.Backends = append(cs.Backends, b)

		switch b.State {
		case "active":
			cs.ActiveConnections++
		case "idle":
			cs.IdleConnections++
		case "idle in transaction", "idle in transaction (aborted)":
			cs.IdleInTxConnections++
		}
		if b.WaitEventType != "" && b.State == "active" {
			cs.WaitingConnections++
		}

		if b.QueryDuration > cs.LongestQueryDur {
			cs.LongestQueryDur = b.QueryDuration
		}
		if b.TxDuration > cs.LongestTxDur {
			cs.LongestTxDur = b.TxDuration
		}

		// Track distinct databases, users, applications.
		cs.Databases = addUnique(cs.Databases, b.DatabaseName)
		cs.Users = addUnique(cs.Users, b.UserName)
		if b.ApplicationName != "" {
			cs.Applications = addUnique(cs.Applications, b.ApplicationName)
		}
	}

	// Convert map to sorted slice.
	var clients []model.ClientStat
	for _, cs := range clientMap {
		// Sort backends within each client: active first, then by duration.
		sort.Slice(cs.Backends, func(i, j int) bool {
			si := stateOrder(cs.Backends[i].State)
			sj := stateOrder(cs.Backends[j].State)
			if si != sj {
				return si < sj
			}
			return cs.Backends[i].QueryDuration > cs.Backends[j].QueryDuration
		})
		clients = append(clients, *cs)
	}

	// Sort clients: most connections first.
	sort.Slice(clients, func(i, j int) bool {
		if clients[i].TotalConnections != clients[j].TotalConnections {
			return clients[i].TotalConnections > clients[j].TotalConnections
		}
		return clients[i].ActiveConnections > clients[j].ActiveConnections
	})

	return clients, nil
}

func addUnique(slice []string, val string) []string {
	if val == "" {
		return slice
	}
	for _, s := range slice {
		if s == val {
			return slice
		}
	}
	return append(slice, val)
}

func stateOrder(state string) int {
	switch state {
	case "active":
		return 1
	case "idle in transaction", "idle in transaction (aborted)":
		return 2
	case "idle":
		return 3
	default:
		return 4
	}
}
