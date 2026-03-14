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

	// Sort indicator.
	sortLabel := fmt.Sprintf("Sorted by: %s (press 's' to cycle)", m.querySortBy)
	b.WriteString("  " + lipgloss.NewStyle().Foreground(colorAccent).Render(sortLabel) + "\n\n")

	// Sort the queries.
	queries := make([]model.QueryStat, len(m.snapshot.Queries))
	copy(queries, m.snapshot.Queries)
	sortQueries(queries, m.querySortBy)

	// Dynamic column widths based on terminal width.
	w := m.contentWidth()
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
	header := hdr
	b.WriteString(styleHeader.Render(header))
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
	var b strings.Builder

	panelWidth := m.contentWidth()
	if panelWidth > 100 {
		panelWidth = 100
	}

	b.WriteString("\n")
	b.WriteString(styleSubtitle.Render("  Query Detail") + "\n")
	b.WriteString(m.renderDivider() + "\n")

	lbl := lipgloss.NewStyle().Foreground(colorMuted)

	b.WriteString(fmt.Sprintf("  %s %s    %s %s    %s %d\n",
		lbl.Render("Database:"), styleValue.Render(q.DatabaseName),
		lbl.Render("User:"), styleValue.Render(q.UserName),
		lbl.Render("Query ID:"), q.QueryID,
	))

	// Timing.
	b.WriteString(fmt.Sprintf("  %s total=%s  mean=%s  min=%s  max=%s  stddev=%s\n",
		lbl.Render("Exec Time:"),
		styleValue.Render(formatDuration(q.TotalExecTime)),
		styleValue.Render(formatDuration(q.MeanExecTime)),
		styleValue.Render(formatDuration(q.MinExecTime)),
		styleValue.Render(formatDuration(q.MaxExecTime)),
		styleValue.Render(formatDuration(q.StddevExecTime)),
	))
	b.WriteString(fmt.Sprintf("  %s total=%s  mean=%s\n",
		lbl.Render("Plan Time:"),
		styleValue.Render(formatDuration(q.TotalPlanTime)),
		styleValue.Render(formatDuration(q.MeanPlanTime)),
	))

	// Blocks.
	totalBlks := q.SharedBlksHit + q.SharedBlksRead
	b.WriteString(fmt.Sprintf("  %s %s    %s hit=%s read=%s (total=%s)\n",
		lbl.Render("Calls:"), styleValue.Render(formatCallCount(q.Calls)),
		lbl.Render("Blocks:"),
		styleValue.Render(formatCallCount(q.SharedBlksHit)),
		styleValue.Render(formatCallCount(q.SharedBlksRead)),
		styleValue.Render(formatCallCount(totalBlks)),
	))

	// Temp and WAL.
	if q.TempBlksWritten > 0 || q.TempBlksRead > 0 {
		b.WriteString(fmt.Sprintf("  %s read=%s written=%s\n",
			lbl.Render("Temp Blocks:"),
			styleBad.Render(formatCallCount(q.TempBlksRead)),
			styleBad.Render(formatCallCount(q.TempBlksWritten)),
		))
	}
	if q.WALRecords > 0 {
		b.WriteString(fmt.Sprintf("  %s records=%s  bytes=%s  fpi=%s\n",
			lbl.Render("WAL:"),
			styleValue.Render(formatCallCount(q.WALRecords)),
			styleValue.Render(formatBytes(q.WALBytes)),
			styleValue.Render(formatCallCount(q.WALFPI)),
		))
	}
	if q.JITFunctions > 0 {
		b.WriteString(fmt.Sprintf("  %s functions=%d  time=%s\n",
			lbl.Render("JIT:"),
			q.JITFunctions,
			styleValue.Render(formatDuration(q.JITTime)),
		))
	}

	// Full query text.
	b.WriteString(fmt.Sprintf("  %s\n", lbl.Render("Query:")))
	queryText := strings.ReplaceAll(q.Query, "\n", " ")
	queryText = strings.Join(strings.Fields(queryText), " ")
	maxLineLen := panelWidth - 4
	for len(queryText) > 0 {
		end := maxLineLen
		if end > len(queryText) {
			end = len(queryText)
		}
		b.WriteString("    " + lipgloss.NewStyle().Foreground(colorText).Render(queryText[:end]) + "\n")
		queryText = queryText[end:]
	}

	return b.String()
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
