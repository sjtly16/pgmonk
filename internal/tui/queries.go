package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/shashank/pgmonk/internal/model"
)

// renderQueries renders the top queries view.
func (m Model) renderQueries() string {
	if m.snapshot == nil || len(m.snapshot.Queries) == 0 {
		if !m.hasStatStmts {
			return renderEmptyState("pg_stat_statements Not Installed",
				"Enable it with: CREATE EXTENSION pg_stat_statements;")
		}
		return renderEmptyState("No Query Statistics", "Run some queries and check back")
	}

	var b strings.Builder
	w := m.contentWidth()
	innerW := w - 7

	// Sort the queries.
	queries := make([]model.QueryStat, len(m.snapshot.Queries))
	copy(queries, m.snapshot.Queries)
	sortQueries(queries, m.querySortBy)

	// Summary box.
	sortLine := lipgloss.NewStyle().Foreground(colorAccent).Render(
		fmt.Sprintf("Sorted by: %s (press 's' to cycle)", m.querySortBy))
	totalQueries := fmt.Sprintf("Showing %s queries",
		styleValue.Render(fmt.Sprintf("%d", len(queries))))
	summaryContent := totalQueries + "\n" + kvDivider(innerW) + "\n" + sortLine
	b.WriteString(dashBox(innerW).Render(summaryContent))
	b.WriteString("\n")

	// Dynamic column widths based on terminal width.
	showPlan := w > 100
	showTemp := w > 110

	fixedCols := 10 + 10 + 10 + 10 + 8 + 6 // Total+Mean+Calls+Rows+Cache + margins
	if showPlan {
		fixedCols += 8
	}
	if showTemp {
		fixedCols += 8
	}
	queryWidth := w - fixedCols - 2
	if queryWidth < 10 {
		queryWidth = 10
	}

	// Table header.
	hdr := fmt.Sprintf("  %-10s %-10s %-10s %-10s %-8s", "Total", "Mean", "Calls", "Rows", "Cache%")
	if showPlan {
		hdr += fmt.Sprintf(" %-8s", "Plan%")
	}
	if showTemp {
		hdr += fmt.Sprintf(" %-8s", "Temp")
	}
	hdr += " Query"
	b.WriteString(styleHeader.Render(hdr))
	b.WriteString("\n")
	b.WriteString(m.renderDivider())
	b.WriteString("\n")

	// Rows with scrolling.
	max := m.maxRows()
	end := m.scrollOffset + max
	if end > len(queries) {
		end = len(queries)
	}

	for i := m.scrollOffset; i < end; i++ {
		q := queries[i]
		isSelected := i == m.selectedRow

		cacheStr := fmt.Sprintf("%.0f%%", q.CacheHitRatio)
		cacheSt := ratioStyle(q.CacheHitRatio)

		// Plan time percentage.
		planPct := 0.0
		totalTime := q.TotalPlanTime + q.TotalExecTime
		if totalTime > 0 {
			planPct = q.TotalPlanTime / totalTime * 100
		}
		planStr := fmt.Sprintf("%.0f%%", planPct)
		planSt := lipgloss.NewStyle().Foreground(colorMuted)
		if planPct > 80 {
			planSt = styleBad
		} else if planPct > 50 {
			planSt = styleWarn
		}

		tempStr := ""
		if q.TempBlksWritten > 0 {
			tempStr = styleBad.Render(fmt.Sprintf("%d", q.TempBlksWritten))
		} else {
			tempStr = lipgloss.NewStyle().Foreground(colorMuted).Render("0")
		}

		queryText := truncate(strings.ReplaceAll(q.Query, "\n", " "), queryWidth)

		line := fmt.Sprintf("  %-10s %-10s %-10s %-10s %s",
			formatDuration(q.TotalExecTime),
			formatDuration(q.MeanExecTime),
			formatCallCount(q.Calls),
			formatCallCount(q.Rows),
			cacheSt.Width(8).Render(cacheStr),
		)
		if showPlan {
			line += " " + planSt.Width(8).Render(planStr)
		}
		if showTemp {
			line += " " + fmt.Sprintf("%-8s", tempStr)
		}
		line += " " + lipgloss.NewStyle().Foreground(colorText).Render(queryText)

		if isSelected {
			line = styleSelectedRow.Render("▸" + line[1:])
		}

		b.WriteString(line + "\n")
	}

	// Scroll indicator.
	b.WriteString(m.renderScrollInfo(len(queries)))
	b.WriteString("\n")

	// Detail panel for selected query.
	if m.showDetail && m.selectedRow < len(queries) {
		b.WriteString(m.renderQueryDetail(queries[m.selectedRow]))
	}

	return b.String()
}

// renderQueryDetail renders the detail panel for a selected query.
func (m Model) renderQueryDetail(q model.QueryStat) string {
	w := m.contentWidth()
	innerW := w - 7
	if innerW > 100 {
		innerW = 100
	}

	lbl := lipgloss.NewStyle().Foreground(colorMuted)

	var rows []string
	rows = append(rows, fmt.Sprintf("%s %s    %s %s    %s %d",
		lbl.Render("Database:"), styleValue.Render(q.DatabaseName),
		lbl.Render("User:"), styleValue.Render(q.UserName),
		lbl.Render("Query ID:"), q.QueryID,
	))

	// Timing.
	rows = append(rows, fmt.Sprintf("%s total=%s  mean=%s  min=%s  max=%s  stddev=%s",
		lbl.Render("Exec Time:"),
		styleValue.Render(formatDuration(q.TotalExecTime)),
		styleValue.Render(formatDuration(q.MeanExecTime)),
		styleValue.Render(formatDuration(q.MinExecTime)),
		styleValue.Render(formatDuration(q.MaxExecTime)),
		styleValue.Render(formatDuration(q.StddevExecTime)),
	))
	rows = append(rows, fmt.Sprintf("%s total=%s  mean=%s",
		lbl.Render("Plan Time:"),
		styleValue.Render(formatDuration(q.TotalPlanTime)),
		styleValue.Render(formatDuration(q.MeanPlanTime)),
	))

	// Blocks.
	totalBlks := q.SharedBlksHit + q.SharedBlksRead
	rows = append(rows, fmt.Sprintf("%s %s    %s hit=%s read=%s (total=%s)",
		lbl.Render("Calls:"), styleValue.Render(formatCallCount(q.Calls)),
		lbl.Render("Blocks:"),
		styleValue.Render(formatCallCount(q.SharedBlksHit)),
		styleValue.Render(formatCallCount(q.SharedBlksRead)),
		styleValue.Render(formatCallCount(totalBlks)),
	))

	// Temp and WAL.
	if q.TempBlksWritten > 0 || q.TempBlksRead > 0 {
		rows = append(rows, fmt.Sprintf("%s read=%s written=%s",
			lbl.Render("Temp Blocks:"),
			styleBad.Render(formatCallCount(q.TempBlksRead)),
			styleBad.Render(formatCallCount(q.TempBlksWritten)),
		))
	}
	if q.WALRecords > 0 {
		rows = append(rows, fmt.Sprintf("%s records=%s  bytes=%s  fpi=%s",
			lbl.Render("WAL:"),
			styleValue.Render(formatCallCount(q.WALRecords)),
			styleValue.Render(formatBytes(q.WALBytes)),
			styleValue.Render(formatCallCount(q.WALFPI)),
		))
	}
	if q.JITFunctions > 0 {
		rows = append(rows, fmt.Sprintf("%s functions=%d  time=%s",
			lbl.Render("JIT:"),
			q.JITFunctions,
			styleValue.Render(formatDuration(q.JITTime)),
		))
	}

	// Full query text.
	queryText := strings.ReplaceAll(q.Query, "\n", " ")
	queryText = strings.Join(strings.Fields(queryText), " ")
	maxLineLen := innerW - 2
	queryEntry := lbl.Render("Query:")
	for len(queryText) > 0 {
		end := maxLineLen
		if end > len(queryText) {
			end = len(queryText)
		}
		queryEntry += "\n  " + lipgloss.NewStyle().Foreground(colorText).Render(queryText[:end])
		queryText = queryText[end:]
	}
	rows = append(rows, queryEntry)

	content := joinWithDividers(rows, innerW)
	box := dashBox(innerW).Render(content)
	return "\n" + dashSectionTitle("QUERY DETAIL") + "\n" + box + "\n"
}

func sortQueries(queries []model.QueryStat, by string) {
	sort.Slice(queries, func(i, j int) bool {
		switch by {
		case "mean_time":
			return queries[i].MeanExecTime > queries[j].MeanExecTime
		case "calls":
			return queries[i].Calls > queries[j].Calls
		case "rows":
			return queries[i].Rows > queries[j].Rows
		default: // total_time
			return queries[i].TotalExecTime > queries[j].TotalExecTime
		}
	})
}

func formatCallCount(n int64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fK", float64(n)/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}
