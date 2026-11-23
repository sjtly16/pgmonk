package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/sjtly16/pgmonk/internal/model"
)

// dashSectionTitle renders a cyan uppercase section label with left padding.
func dashSectionTitle(title string) string {
	return lipgloss.NewStyle().
		Foreground(colorSecondary).
		Bold(true).
		PaddingLeft(1).
		Render(title)
}

// dashBox returns a box style for the given content width.
// lipgloss Width includes padding, so we set Width = innerW + 4 (pad-left 2 + pad-right 2).
// Total rendered width = Width + border(2) + margin-left(1) = innerW + 7.
func dashBox(innerW int) lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#4B5563")).
		Padding(0, 2).
		MarginLeft(1).
		Width(innerW + 4)
}

// kvRow renders a label (left) + value (right-aligned) row using explicit lipgloss
// Width columns. This avoids any ANSI-aware word-wrap miscounting inside box.Render().
func kvRow(label, value string, innerW int) string {
	const valW = 18
	labelW := innerW - valW
	if labelW < 1 {
		labelW = 1
	}
	l := lipgloss.NewStyle().Foreground(colorMuted).Width(labelW).Render(label)
	v := lipgloss.NewStyle().Foreground(colorText).Width(valW).Align(lipgloss.Right).Render(value)
	return l + v
}

// kvDivider renders a subtle horizontal divider line for use inside boxes.
func kvDivider(innerW int) string {
	return lipgloss.NewStyle().Foreground(lipgloss.Color("#2D3748")).Render(strings.Repeat("─", innerW))
}

// joinWithDividers joins rows with subtle divider lines between them.
func joinWithDividers(rows []string, innerW int) string {
	div := kvDivider(innerW)
	var result []string
	for i, row := range rows {
		result = append(result, row)
		if i < len(rows)-1 {
			result = append(result, div)
		}
	}
	return strings.Join(result, "\n")
}

// progressBar renders a thin fixed-width progress bar using block characters.
func progressBar(current, max, width int, filledColor lipgloss.Color) string {
	if width <= 0 {
		return ""
	}
	if max <= 0 {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#2D3748")).Render(strings.Repeat("▁", width))
	}
	ratio := float64(current) / float64(max)
	if ratio > 1 {
		ratio = 1
	}
	if ratio < 0 {
		ratio = 0
	}
	filledN := int(ratio * float64(width))
	if filledN > width {
		filledN = width
	}
	filled := lipgloss.NewStyle().Foreground(filledColor).Render(strings.Repeat("▁", filledN))
	empty := lipgloss.NewStyle().Foreground(lipgloss.Color("#2D3748")).Render(strings.Repeat("▁", width-filledN))
	return filled + empty
}

// renderDashboard renders the main overview dashboard.
func (m Model) renderDashboard() string {
	if m.snapshot == nil {
		return "  No data yet..."
	}

	o := m.snapshot.Overview
	w := m.contentWidth()

	var sections []string

	sections = append(sections, m.renderConnectionsSection(o, w))
	sections = append(sections, m.renderHealthSection(o, w))
	sections = append(sections, m.renderThroughputWALRow(o, w))
	sections = append(sections, m.renderActiveQueriesSection(w))

	return strings.Join(sections, "\n")
}

func (m Model) renderConnectionsSection(o model.Overview, w int) string {
	innerW := w - 7 // border(2) + padding-left(2) + padding-right(2) + margin-left(1)
	if innerW < 20 {
		innerW = 20
	}

	connPct := 0.0
	if o.MaxConnections > 0 {
		connPct = float64(o.TotalConnections) / float64(o.MaxConnections) * 100
	}

	barColor := lipgloss.Color("#6366F1") // indigo
	pctColor := colorSecondary
	if connPct > 85 {
		barColor = colorDanger
		pctColor = colorDanger
	} else if connPct > 60 {
		barColor = colorWarning
		pctColor = colorWarning
	}

	// Use explicit-width columns: label left, pct right — no gap/ANSI-width issues.
	const pctW = 6
	labelStr := fmt.Sprintf("%d / %d connections used", o.TotalConnections, o.MaxConnections)
	pctStr := fmt.Sprintf("%.0f%%", connPct)
	line1 := lipgloss.NewStyle().Foreground(colorText).Bold(true).Width(innerW-pctW).Render(labelStr) +
		lipgloss.NewStyle().Foreground(pctColor).Bold(true).Width(pctW).Align(lipgloss.Right).Render(pctStr)
	bar := progressBar(o.TotalConnections, o.MaxConnections, innerW, barColor)
	dots := fmt.Sprintf("%s active: %d   %s idle: %d   %s idle-in-tx: %d",
		styleActive.Render("●"), o.ActiveConnections,
		styleIdle.Render("●"), o.IdleConnections,
		styleIdleInTx.Render("●"), o.IdleInTxConnections,
	)

	content := line1 + "\n" + bar + "\n" + kvDivider(innerW) + "\n" + dots
	box := dashBox(innerW).Render(content)
	return dashSectionTitle("CONNECTIONS") + "\n" + box
}

func (m Model) renderHealthSection(o model.Overview, w int) string {
	// Each card total width = cardInner + 6 (border 2 + padding 4).
	// 4 cards + 3 single-space gaps + 1 margin-left = 4*(cardInner+6) + 3 + 1 = w
	// → cardInner = (w-28)/4.
	cardInner := (w - 28) / 4
	if cardInner < 12 {
		cardInner = 12
	}

	muted := lipgloss.NewStyle().Foreground(colorMuted)
	blank := strings.Repeat(" ", cardInner) // placeholder line to equalise card heights

	// Card 1: Cache Hit Ratio (title / value / bar / label)
	cacheRatio := o.CacheHitRatio
	cacheColor := colorSuccess
	cacheLabel := "excellent"
	if cacheRatio < 95 {
		cacheColor = colorDanger
		cacheLabel = "poor"
	} else if cacheRatio < 99 {
		cacheColor = colorWarning
		cacheLabel = "fair"
	}
	card1 := lipgloss.JoinVertical(lipgloss.Left,
		muted.Render("Cache Hit Ratio"),
		lipgloss.NewStyle().Foreground(cacheColor).Bold(true).Render(fmt.Sprintf("%.2f%%", cacheRatio)),
		progressBar(int(cacheRatio*10), 1000, cardInner, cacheColor),
		muted.Render(cacheLabel),
	)

	// Card 2: Database Size (title / value / blank / label) — blank keeps height equal
	card2 := lipgloss.JoinVertical(lipgloss.Left,
		muted.Render("Database Size"),
		lipgloss.NewStyle().Foreground(colorText).Bold(true).Render(formatBytes(o.DatabaseSize)),
		blank,
		muted.Render("on disk"),
	)

	// Card 3: Deadlocks (title / value / blank / label)
	dlColor := colorSuccess
	if o.Deadlocks > 0 {
		dlColor = colorDanger
	}
	card3 := lipgloss.JoinVertical(lipgloss.Left,
		muted.Render("Deadlocks"),
		lipgloss.NewStyle().Foreground(dlColor).Bold(true).Render(fmt.Sprintf("%d", o.Deadlocks)),
		blank,
		muted.Render("since reset"),
	)

	// Card 4: XID Age (title / value / bar / label)
	const xidMax int64 = 2_100_000_000
	xidAge := o.DataFrozenXIDAge
	xidColor := colorSuccess
	if xidAge > 1_500_000_000 {
		xidColor = colorDanger
	} else if xidAge > 500_000_000 {
		xidColor = colorWarning
	}
	card4 := lipgloss.JoinVertical(lipgloss.Left,
		muted.Render("XID Age"),
		lipgloss.NewStyle().Foreground(xidColor).Bold(true).Render(formatNumber(xidAge)),
		progressBar(int(xidAge/1_000_000), int(xidMax/1_000_000), cardInner, xidColor),
		muted.Render("of 2.1B limit"),
	)

	cs := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#4B5563")).
		Padding(0, 2).
		Width(cardInner + 4)
	cards := lipgloss.JoinHorizontal(lipgloss.Top,
		cs.Render(card1), " ",
		cs.Render(card2), " ",
		cs.Render(card3), " ",
		cs.Render(card4),
	)
	// Wrap with left margin matching other sections
	wrappedCards := lipgloss.NewStyle().MarginLeft(1).Render(cards)
	return dashSectionTitle("HEALTH") + "\n" + wrappedCards
}

func (m Model) renderThroughputWALRow(o model.Overview, w int) string {
	// Left column: margin-left(1) + border(2) + padding(4) + colInner = leftTotal
	// Right column: border(2) + padding(4) + colInner = rightTotal
	// leftTotal + 1 (gap) + rightTotal = w
	// (colInner+7) + 1 + (colInner+6) = w  →  colInner = (w-14)/2
	colInner := (w - 14) / 2
	if colInner < 16 {
		colInner = 16
	}
	leftInner := colInner
	rightInner := colInner

	// Box style without left margin (for right column).
	rightBox := func(iw int) lipgloss.Style {
		return lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#4B5563")).
			Padding(0, 2).
			Width(iw + 4)
	}

	// trendVal appends " ↑" to non-zero rates as a plain string (included in value column).
	trendVal := func(rate float64) string {
		if rate > 0 {
			return fmt.Sprintf("%.0f ↑", rate)
		}
		return fmt.Sprintf("%.0f", rate)
	}

	tpLines := joinWithDividers([]string{
		kvRow("Commits", trendVal(o.TxCommitRate), leftInner),
		kvRow("Rollbacks", trendVal(o.TxRollbackRate), leftInner),
		kvRow("Tuples fetched", fmt.Sprintf("%.0f", o.TupFetchRate), leftInner),
		kvRow("Inserts", fmt.Sprintf("%.0f", o.TupInsertRate), leftInner),
		kvRow("Updates", fmt.Sprintf("%.0f", o.TupUpdateRate), leftInner),
		kvRow("Deletes", fmt.Sprintf("%.0f", o.TupDeleteRate), leftInner),
	}, leftInner)

	tpBox := dashBox(leftInner).Render(tpLines)
	leftCol := dashSectionTitle("THROUGHPUT / SEC") + "\n" + tpBox

	// Right column: WAL + CHECKPOINTS stacked (no extra left margin).
	var rightParts []string

	if walStats := m.snapshot.WALStats; walStats != nil {
		walLines := joinWithDividers([]string{
			kvRow("Records", formatNumber(walStats.WALRecords), rightInner),
			kvRow("FPI", formatNumber(walStats.WALFPI), rightInner),
			kvRow("Bytes written", formatBytes(walStats.WALBytes), rightInner),
		}, rightInner)
		walBox := rightBox(rightInner).Render(walLines)
		rightParts = append(rightParts, dashSectionTitle("WAL")+"\n"+walBox)
	}

	totalCP := o.CheckpointsReq + o.CheckpointsTimed
	cpRows := []string{
		kvRow("Total", fmt.Sprintf("%d", totalCP), rightInner),
		kvRow("Timed / Requested", fmt.Sprintf("%d / %d", o.CheckpointsTimed, o.CheckpointsReq), rightInner),
	}
	if o.CheckpointWriteTime > 0 || o.CheckpointSyncTime > 0 {
		cpRows = append(cpRows, kvRow("Write / Sync time",
			fmt.Sprintf("%.0fs / %.0fs", o.CheckpointWriteTime/1000, o.CheckpointSyncTime/1000), rightInner))
	}
	cpBox := rightBox(rightInner).Render(joinWithDividers(cpRows, rightInner))
	rightParts = append(rightParts, dashSectionTitle("CHECKPOINTS")+"\n"+cpBox)

	rightCol := strings.Join(rightParts, "\n")
	return lipgloss.JoinHorizontal(lipgloss.Top, leftCol, " ", rightCol)
}

func (m Model) renderActiveQueriesSection(w int) string {
	innerW := w - 7 // border(2) + padding-left(2) + padding-right(2) + margin-left(1)
	if innerW < 20 {
		innerW = 20
	}

	var lines []string
	if m.snapshot != nil {
		for _, a := range m.snapshot.Activities {
			if a.State != "active" || a.Query == "" {
				continue
			}
			dur := formatQueryDuration(a.Duration)
			q := truncate(strings.ReplaceAll(a.Query, "\n", " "), innerW-32)
			line := fmt.Sprintf("%s %s %s %s",
				durStyle(a.Duration).Render(dur),
				lipgloss.NewStyle().Foreground(colorMuted).Render(fmt.Sprintf("[%s]", a.UserName)),
				lipgloss.NewStyle().Foreground(colorText).Render(q),
				lipgloss.NewStyle().Foreground(colorMuted).Render(fmt.Sprintf("(pid:%d)", a.PID)),
			)
			lines = append(lines, line)
			if len(lines) >= 5 {
				break
			}
		}
	}

	var content string
	if len(lines) == 0 {
		content = lipgloss.NewStyle().
			Width(innerW).
			Align(lipgloss.Center).
			Foreground(colorMuted).
			Render("No active queries")
	} else {
		content = strings.Join(lines, "\n")
	}

	box := dashBox(innerW).Render(content)
	return dashSectionTitle("ACTIVE QUERIES") + "\n" + box
}

func renderEmptyState(title, subtitle string) string {
	content := lipgloss.NewStyle().Foreground(colorText).Bold(true).Render(title) + "\n" +
		lipgloss.NewStyle().Foreground(colorMuted).Render(subtitle)
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#4B5563")).
		Padding(1, 3).
		MarginLeft(1).
		Width(59).
		Render(content)
	return "\n" + box + "\n"
}

func formatNumber(n int64) string {
	if n >= 1_000_000_000 {
		return fmt.Sprintf("%.2fB", float64(n)/1_000_000_000)
	}
	if n >= 1_000_000 {
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	}
	if n >= 1_000 {
		return fmt.Sprintf("%.0fK", float64(n)/1_000)
	}
	return fmt.Sprintf("%d", n)
}

func formatQueryDuration(d time.Duration) string {
	switch {
	case d == 0:
		return fmt.Sprintf("%5s", "-")
	case d >= time.Minute:
		return fmt.Sprintf("%5.1fm", d.Minutes())
	case d >= time.Second:
		return fmt.Sprintf("%5.1fs", d.Seconds())
	case d >= time.Millisecond:
		return fmt.Sprintf("%5.0fms", float64(d.Milliseconds()))
	default:
		return fmt.Sprintf("%5s", "<1ms")
	}
}

func durStyle(d time.Duration) lipgloss.Style {
	switch {
	case d >= 10*time.Second:
		return styleBad.Width(8)
	case d >= time.Second:
		return styleWarn.Width(8)
	default:
		return styleGood.Width(8)
	}
}
