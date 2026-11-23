package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/sjtly16/pgmonk/internal/model"
)

// renderClients renders the per-client / per-thread metrics view.
func (m Model) renderClients() string {
	if m.snapshot == nil || len(m.snapshot.Clients) == 0 {
		return renderEmptyState("No Client Connections", "No active client connections detected")
	}

	if m.expandedClient != "" {
		return m.renderClientThreads()
	}

	return m.renderClientSummary()
}

// renderClientSummary renders the aggregated per-client view.
func (m Model) renderClientSummary() string {
	var b strings.Builder
	w := m.contentWidth()
	innerW := w - 7

	clients := m.sortedClients()

	// Summary bar.
	totalClients := len(clients)
	totalConns := 0
	totalActive := 0
	for _, c := range clients {
		totalConns += c.TotalConnections
		totalActive += c.ActiveConnections
	}

	summaryLine := fmt.Sprintf("Clients: %s  │  Total connections: %s  │  Active: %s",
		styleValue.Render(fmt.Sprintf("%d", totalClients)),
		styleValue.Render(fmt.Sprintf("%d", totalConns)),
		styleActive.Render(fmt.Sprintf("%d", totalActive)),
	)
	sortLine := lipgloss.NewStyle().Foreground(colorAccent).Render(
		fmt.Sprintf("Sorted by: %s (press 's' to cycle)", m.clientSortBy))
	summaryContent := summaryLine + "\n" + kvDivider(innerW) + "\n" + sortLine
	b.WriteString(dashBox(innerW).Render(summaryContent))
	b.WriteString("\n")

	// Table header.
	header := fmt.Sprintf("  %-18s %-6s %-6s %-6s %-6s %-6s %-12s %-12s %-18s %s",
		"Client", "Total", "Act", "Idle", "IdTx", "Wait", "Max Query", "Max Tx", "Users", "Databases",
	)
	b.WriteString(styleHeader.Render(header))
	b.WriteString("\n")
	b.WriteString(m.renderDivider())
	b.WriteString("\n")

	maxRows := 20
	if m.height > 0 {
		maxRows = m.height - 12
		if maxRows < 5 {
			maxRows = 5
		}
	}

	for i, c := range clients {
		if i >= maxRows {
			remaining := len(clients) - maxRows
			b.WriteString(lipgloss.NewStyle().Foreground(colorMuted).PaddingLeft(2).Render(
				fmt.Sprintf("... and %d more clients", remaining)))
			b.WriteString("\n")
			break
		}

		// Connection count styling.
		totalSt := styleValue
		if c.TotalConnections > 20 {
			totalSt = styleBad
		} else if c.TotalConnections > 10 {
			totalSt = styleWarn
		}

		activeSt := lipgloss.NewStyle().Foreground(colorText)
		if c.ActiveConnections > 10 {
			activeSt = styleWarn
		}

		idleTxSt := lipgloss.NewStyle().Foreground(colorText)
		if c.IdleInTxConnections > 0 {
			idleTxSt = styleWarn
		}
		if c.IdleInTxConnections > 3 {
			idleTxSt = styleBad
		}

		waitSt := lipgloss.NewStyle().Foreground(colorText)
		if c.WaitingConnections > 0 {
			waitSt = styleBad
		}

		maxQueryStr := formatShortDuration(c.LongestQueryDur)
		maxTxStr := formatShortDuration(c.LongestTxDur)

		users := truncate(strings.Join(c.Users, ","), 18)
		dbs := truncate(strings.Join(c.Databases, ","), 15)

		// Indicator for the connection bar.
		connBar := miniBar(c.TotalConnections, m.snapshot.Overview.MaxConnections, 5)

		fmt.Fprintf(&b, "  %-18s %s %s %s %s %s %s %s %s %s %s\n",
			truncate(c.ClientAddr, 18),
			totalSt.Width(6).Render(fmt.Sprintf("%d", c.TotalConnections)),
			activeSt.Width(6).Render(fmt.Sprintf("%d", c.ActiveConnections)),
			lipgloss.NewStyle().Foreground(colorMuted).Width(6).Render(fmt.Sprintf("%d", c.IdleConnections)),
			idleTxSt.Width(6).Render(fmt.Sprintf("%d", c.IdleInTxConnections)),
			waitSt.Width(6).Render(fmt.Sprintf("%d", c.WaitingConnections)),
			durStyle(c.LongestQueryDur).Width(12).Render(maxQueryStr),
			durStyle(c.LongestTxDur).Width(12).Render(maxTxStr),
			lipgloss.NewStyle().Foreground(colorMuted).Width(18).Render(users),
			lipgloss.NewStyle().Foreground(colorMuted).Width(16).Render(dbs),
			connBar,
		)
	}

	b.WriteString("\n")
	b.WriteString(lipgloss.NewStyle().Foreground(colorMuted).PaddingLeft(2).Render(
		"Press Enter on a client to see per-thread details"))
	b.WriteString("\n")

	return b.String()
}

// renderClientThreads renders the per-thread (backend) details for the expanded client.
func (m Model) renderClientThreads() string {
	var b strings.Builder
	w := m.contentWidth()
	innerW := w - 7

	// Find the selected client.
	var client *model.ClientStat
	for i := range m.snapshot.Clients {
		if m.snapshot.Clients[i].ClientAddr == m.expandedClient {
			client = &m.snapshot.Clients[i]
			break
		}
	}
	if client == nil {
		return "  Client not found. Press Esc to go back."
	}

	// Client summary in a box.
	lbl := lipgloss.NewStyle().Foreground(colorMuted)
	var summaryRows []string
	summaryRows = append(summaryRows,
		fmt.Sprintf("Connections: %s  │  Active: %s  │  Idle: %s  │  Idle-in-Tx: %s  │  Waiting: %s",
			styleValue.Render(fmt.Sprintf("%d", client.TotalConnections)),
			styleActive.Render(fmt.Sprintf("%d", client.ActiveConnections)),
			styleIdle.Render(fmt.Sprintf("%d", client.IdleConnections)),
			styleIdleInTx.Render(fmt.Sprintf("%d", client.IdleInTxConnections)),
			styleWaiting.Render(fmt.Sprintf("%d", client.WaitingConnections)),
		))
	summaryRows = append(summaryRows,
		fmt.Sprintf("Users: %s  │  Databases: %s  │  Apps: %s",
			lipgloss.NewStyle().Foreground(colorAccent).Render(strings.Join(client.Users, ", ")),
			lipgloss.NewStyle().Foreground(colorAccent).Render(strings.Join(client.Databases, ", ")),
			lipgloss.NewStyle().Foreground(colorAccent).Render(strings.Join(client.Applications, ", ")),
		))
	summaryRows = append(summaryRows,
		fmt.Sprintf("Longest query: %s  │  Longest transaction: %s",
			durStyle(client.LongestQueryDur).Render(formatShortDuration(client.LongestQueryDur)),
			durStyle(client.LongestTxDur).Render(formatShortDuration(client.LongestTxDur)),
		))

	_ = lbl // used in chart below
	clientTitle := fmt.Sprintf("CLIENT: %s", client.ClientAddr)
	summaryContent := joinWithDividers(summaryRows, innerW)
	b.WriteString(dashSectionTitle(clientTitle) + "\n")
	b.WriteString(dashBox(innerW).Render(summaryContent))
	b.WriteString("\n")

	// Thread table.
	b.WriteString(dashSectionTitle("BACKENDS") + "\n")
	header := fmt.Sprintf("  %-7s %-8s %-12s %-15s %-10s %-10s %-18s %-10s %s",
		"PID", "State", "User", "Database", "Query Dur", "Tx Dur", "Wait Event", "Uptime", "Query",
	)
	b.WriteString(styleHeader.Render(header))
	b.WriteString("\n")
	b.WriteString(m.renderDivider())
	b.WriteString("\n")

	maxRows := 20
	if m.height > 0 {
		maxRows = m.height - 14
		if maxRows < 5 {
			maxRows = 5
		}
	}

	for i, t := range client.Backends {
		if i >= maxRows {
			remaining := len(client.Backends) - maxRows
			b.WriteString(lipgloss.NewStyle().Foreground(colorMuted).PaddingLeft(2).Render(
				fmt.Sprintf("... and %d more threads", remaining)))
			b.WriteString("\n")
			break
		}

		stateLabel := t.State
		switch t.State {
		case "idle in transaction":
			stateLabel = "idle-tx"
		case "idle in transaction (aborted)":
			stateLabel = "idle-tx!"
		}
		state := stateStyle(t.State).Width(8).Render(truncate(stateLabel, 8))

		queryDur := formatShortDuration(t.QueryDuration)
		txDur := formatShortDuration(t.TxDuration)

		wait := ""
		if t.WaitEventType != "" {
			wait = fmt.Sprintf("%s:%s", t.WaitEventType, t.WaitEvent)
		}

		uptime := formatUptime(t.Uptime)
		query := truncate(strings.ReplaceAll(t.Query, "\n", " "), 35)

		fmt.Fprintf(&b, "  %-7d %s %-12s %-15s %s %s %-18s %s %s\n",
			t.PID,
			state,
			truncate(t.UserName, 12),
			truncate(t.DatabaseName, 15),
			durStyle(t.QueryDuration).Width(10).Render(queryDur),
			durStyle(t.TxDuration).Width(10).Render(txDur),
			truncate(wait, 18),
			lipgloss.NewStyle().Foreground(colorMuted).Width(10).Render(uptime),
			lipgloss.NewStyle().Foreground(colorText).Render(query),
		)
	}

	// Per-state breakdown mini chart in a box.
	b.WriteString("\n")
	b.WriteString(m.renderThreadStateChart(client))

	return b.String()
}

// renderThreadStateChart renders a small horizontal bar chart of thread states.
func (m Model) renderThreadStateChart(client *model.ClientStat) string {
	if client.TotalConnections == 0 {
		return ""
	}

	w := m.contentWidth()
	innerW := w - 7

	var b strings.Builder
	total := float64(client.TotalConnections)
	barWidth := 40

	type stateCount struct {
		name  string
		count int
		style lipgloss.Style
	}
	states := []stateCount{
		{"active", client.ActiveConnections, styleActive},
		{"idle", client.IdleConnections, styleIdle},
		{"idle-in-tx", client.IdleInTxConnections, styleIdleInTx},
		{"waiting", client.WaitingConnections, styleWaiting},
	}

	var chartRows []string
	for _, sc := range states {
		if sc.count == 0 {
			continue
		}
		pct := float64(sc.count) / total * 100
		filled := int(float64(sc.count) / total * float64(barWidth))
		if filled < 1 {
			filled = 1
		}

		bar := sc.style.Render(strings.Repeat("█", filled))
		label := fmt.Sprintf("%-10s %3d (%4.1f%%)", sc.name, sc.count, pct)
		chartRows = append(chartRows, lipgloss.NewStyle().Width(25).Render(label)+" "+bar)
	}

	// Per-database breakdown.
	if len(client.Databases) > 1 {
		chartRows = append(chartRows, "")
		chartRows = append(chartRows, lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render("By Database"))
		dbCounts := map[string]int{}
		for _, t := range client.Backends {
			dbCounts[t.DatabaseName]++
		}
		for db, count := range dbCounts {
			pct := float64(count) / total * 100
			filled := int(float64(count) / total * float64(barWidth))
			if filled < 1 {
				filled = 1
			}
			bar := lipgloss.NewStyle().Foreground(colorSecondary).Render(strings.Repeat("█", filled))
			label := fmt.Sprintf("%-15s %3d (%4.1f%%)", truncate(db, 15), count, pct)
			chartRows = append(chartRows, lipgloss.NewStyle().Width(25).Render(label)+" "+bar)
		}
	}

	// Per-user breakdown.
	if len(client.Users) > 1 {
		chartRows = append(chartRows, "")
		chartRows = append(chartRows, lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render("By User"))
		userCounts := map[string]int{}
		for _, t := range client.Backends {
			userCounts[t.UserName]++
		}
		for user, count := range userCounts {
			pct := float64(count) / total * 100
			filled := int(float64(count) / total * float64(barWidth))
			if filled < 1 {
				filled = 1
			}
			bar := lipgloss.NewStyle().Foreground(colorAccent).Render(strings.Repeat("█", filled))
			label := fmt.Sprintf("%-15s %3d (%4.1f%%)", truncate(user, 15), count, pct)
			chartRows = append(chartRows, lipgloss.NewStyle().Width(25).Render(label)+" "+bar)
		}
	}

	chartContent := strings.Join(chartRows, "\n")
	b.WriteString(dashSectionTitle("THREAD STATE DISTRIBUTION") + "\n")
	b.WriteString(dashBox(innerW).Render(chartContent))
	b.WriteString("\n")

	return b.String()
}

// --- Sort helpers ---

func (m Model) sortedClients() []model.ClientStat {
	if m.snapshot == nil {
		return nil
	}
	clients := make([]model.ClientStat, len(m.snapshot.Clients))
	copy(clients, m.snapshot.Clients)

	sort.Slice(clients, func(i, j int) bool {
		switch m.clientSortBy {
		case "active":
			if clients[i].ActiveConnections != clients[j].ActiveConnections {
				return clients[i].ActiveConnections > clients[j].ActiveConnections
			}
			return clients[i].TotalConnections > clients[j].TotalConnections
		case "duration":
			if clients[i].LongestQueryDur != clients[j].LongestQueryDur {
				return clients[i].LongestQueryDur > clients[j].LongestQueryDur
			}
			return clients[i].TotalConnections > clients[j].TotalConnections
		default: // "connections"
			return clients[i].TotalConnections > clients[j].TotalConnections
		}
	})

	return clients
}

// --- Format helpers ---

func formatShortDuration(d time.Duration) string {
	if d == 0 {
		return "-"
	}
	switch {
	case d >= time.Hour:
		return fmt.Sprintf("%.1fh", d.Hours())
	case d >= time.Minute:
		return fmt.Sprintf("%.1fm", d.Minutes())
	case d >= time.Second:
		return fmt.Sprintf("%.1fs", d.Seconds())
	case d >= time.Millisecond:
		return fmt.Sprintf("%dms", d.Milliseconds())
	default:
		return "<1ms"
	}
}

func formatUptime(d time.Duration) string {
	if d == 0 {
		return "-"
	}
	switch {
	case d >= 24*time.Hour:
		return fmt.Sprintf("%.0fd", d.Hours()/24)
	case d >= time.Hour:
		return fmt.Sprintf("%.0fh", d.Hours())
	case d >= time.Minute:
		return fmt.Sprintf("%.0fm", d.Minutes())
	default:
		return fmt.Sprintf("%.0fs", d.Seconds())
	}
}

func miniBar(current, max, width int) string {
	if max == 0 || current == 0 {
		return ""
	}
	ratio := float64(current) / float64(max)
	filled := int(ratio * float64(width))
	if filled < 1 && current > 0 {
		filled = 1
	}
	if filled > width {
		filled = width
	}

	var barColor lipgloss.Color
	switch {
	case ratio > 0.10:
		barColor = lipgloss.Color(colorWarning)
	default:
		barColor = lipgloss.Color(colorMuted)
	}

	return lipgloss.NewStyle().Foreground(barColor).Render(strings.Repeat("▪", filled))
}
