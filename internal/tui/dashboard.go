package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/shashank/pgmonk/internal/analyzer"
)

// sectionHeader renders a styled section header with a subtle separator.
func sectionHeader(title string) string {
	return styleSectionTitle.Render("── " + title + " ──")
}

// renderDashboard renders the main overview dashboard.
func (m Model) renderDashboard() string {
	if m.snapshot == nil {
		return "  No data yet..."
	}

	o := m.snapshot.Overview
	var sections []string

	// --- Connection Gauge ---
	connPct := 0.0
	if o.MaxConnections > 0 {
		connPct = float64(o.TotalConnections) / float64(o.MaxConnections) * 100
	}
	connTrend := ""
	if m.prevSnapshot != nil {
		connTrend = " " + trendIndicator(float64(o.TotalConnections), float64(m.prevSnapshot.Overview.TotalConnections), false)
	}
	connSection := sectionHeader("Connections") + "\n" +
		fmt.Sprintf("  %s %s%s\n",
			connectionBar(o.TotalConnections, o.MaxConnections, 30),
			styleValue.Render(fmt.Sprintf("%d / %d (%.0f%%)", o.TotalConnections, o.MaxConnections, connPct)),
			connTrend,
		) +
		fmt.Sprintf("  %s %s  %s %s  %s %s\n",
			styleActive.Render("●"), fmt.Sprintf("active: %d", o.ActiveConnections),
			styleIdle.Render("●"), fmt.Sprintf("idle: %d", o.IdleConnections),
			styleIdleInTx.Render("●"), fmt.Sprintf("idle-in-tx: %d", o.IdleInTxConnections),
		)
	sections = append(sections, connSection)

	// --- Throughput ---
	commitTrend, rollbackTrend := "", ""
	if m.prevSnapshot != nil {
		commitTrend = " " + trendIndicator(o.TxCommitRate, m.prevSnapshot.Overview.TxCommitRate, true)
		rollbackTrend = " " + trendIndicator(o.TxRollbackRate, m.prevSnapshot.Overview.TxRollbackRate, false)
	}
	tpsSection := sectionHeader("Throughput (per second)") + "\n" +
		fmt.Sprintf("  %s %s%s  %s%s\n", styleLabel.Render("Transactions:"),
			styleValue.Render(fmt.Sprintf("%.0f commit", o.TxCommitRate)), commitTrend,
			styleValue.Render(fmt.Sprintf("%.0f rollback", o.TxRollbackRate)), rollbackTrend) +
		fmt.Sprintf("  %s %s\n", styleLabel.Render("Tuples:"),
			styleValue.Render(fmt.Sprintf("%.0f fetch  %.0f ins  %.0f upd  %.0f del",
				o.TupFetchRate, o.TupInsertRate, o.TupUpdateRate, o.TupDeleteRate)))
	if o.RollbackRatio > 1 {
		tpsSection += fmt.Sprintf("  %s %s\n", styleLabel.Render("Rollback Ratio:"),
			rollbackStyle(o.RollbackRatio).Render(fmt.Sprintf("%.1f%%", o.RollbackRatio)))
	}
	sections = append(sections, tpsSection)

	// --- Cache & Health ---
	cacheTrend := ""
	if m.prevSnapshot != nil {
		cacheTrend = " " + trendIndicator(o.CacheHitRatio, m.prevSnapshot.Overview.CacheHitRatio, true)
	}
	cacheStyled := ratioStyle(o.CacheHitRatio).Render(fmt.Sprintf("%.2f%%", o.CacheHitRatio))
	healthSection := sectionHeader("Health") + "\n" +
		fmt.Sprintf("  %s %s%s\n", styleLabel.Render("Cache Hit Ratio:"), cacheStyled, cacheTrend) +
		fmt.Sprintf("  %s %s\n", styleLabel.Render("Database Size:"), styleValue.Render(formatBytes(o.DatabaseSize))) +
		fmt.Sprintf("  %s %s\n", styleLabel.Render("Deadlocks:"),
			deadlockStyle(o.Deadlocks).Render(fmt.Sprintf("%d", o.Deadlocks))) +
		fmt.Sprintf("  %s %s\n", styleLabel.Render("Temp Files:"),
			styleValue.Render(fmt.Sprintf("%d (%s)", o.TempFiles, formatBytes(o.TempBytes))))
	// XID wraparound age.
	if o.DataFrozenXIDAge > 0 {
		xidSt := styleGood
		if o.DataFrozenXIDAge > 500_000_000 {
			xidSt = styleWarn
		}
		if o.DataFrozenXIDAge > 1_500_000_000 {
			xidSt = styleBad
		}
		healthSection += fmt.Sprintf("  %s %s\n", styleLabel.Render("XID Age:"),
			xidSt.Render(formatNumber(o.DataFrozenXIDAge)))
	}
	sections = append(sections, healthSection)

	// --- Checkpoints ---
	totalCP := o.CheckpointsReq + o.CheckpointsTimed
	cpSection := sectionHeader("Checkpoints") + "\n" +
		fmt.Sprintf("  %s %s\n", styleLabel.Render("Total:"), styleValue.Render(fmt.Sprintf("%d", totalCP))) +
		fmt.Sprintf("  %s %s\n", styleLabel.Render("Timed / Requested:"),
			styleValue.Render(fmt.Sprintf("%d / %d", o.CheckpointsTimed, o.CheckpointsReq)))
	if o.CheckpointWriteTime > 0 {
		cpSection += fmt.Sprintf("  %s %s\n", styleLabel.Render("Write / Sync Time:"),
			styleValue.Render(fmt.Sprintf("%.1fs / %.1fs", o.CheckpointWriteTime/1000, o.CheckpointSyncTime/1000)))
	}
	sections = append(sections, cpSection)

	// --- WAL Stats ---
	if w := m.snapshot.WALStats; w != nil {
		walSection := sectionHeader("WAL") + "\n" +
			fmt.Sprintf("  %s %s\n", styleLabel.Render("Records / FPI:"),
				styleValue.Render(fmt.Sprintf("%s / %s", formatNumber(w.WALRecords), formatNumber(w.WALFPI)))) +
			fmt.Sprintf("  %s %s\n", styleLabel.Render("Bytes Written:"),
				styleValue.Render(formatBytes(w.WALBytes)))
		if w.WALBuffersFull > 0 {
			walSection += fmt.Sprintf("  %s %s\n", styleLabel.Render("Buffers Full:"),
				styleWarn.Render(fmt.Sprintf("%d", w.WALBuffersFull)))
		}
		sections = append(sections, walSection)
	}

	// --- Archiver Stats ---
	if a := m.snapshot.ArchiverStats; a != nil && (a.ArchivedCount > 0 || a.FailedCount > 0) {
		archSection := sectionHeader("WAL Archiver") + "\n" +
			fmt.Sprintf("  %s %s\n", styleLabel.Render("Archived:"),
				styleGood.Render(fmt.Sprintf("%d", a.ArchivedCount)))
		if a.FailedCount > 0 {
			archSection += fmt.Sprintf("  %s %s\n", styleLabel.Render("Failed:"),
				styleBad.Render(fmt.Sprintf("%d (last: %s)", a.FailedCount, a.LastFailedWAL)))
		}
		sections = append(sections, archSection)
	}

	// --- Replication ---
	if len(m.snapshot.Replication) > 0 {
		replSection := sectionHeader("Replication") + "\n"
		for _, r := range m.snapshot.Replication {
			lagStr := "n/a"
			lagSt := lipgloss.NewStyle().Foreground(colorMuted)
			if r.ReplayLag != nil {
				lagStr = r.ReplayLag.Round(time.Millisecond).String()
				if *r.ReplayLag > 5*time.Minute {
					lagSt = styleBad
				} else if *r.ReplayLag > 30*time.Second {
					lagSt = styleWarn
				} else {
					lagSt = styleGood
				}
			}
			replSection += fmt.Sprintf("  %-20s %s  lag: %s  state: %s\n",
				truncate(r.ApplicationName, 20),
				lipgloss.NewStyle().Foreground(colorMuted).Render(r.ClientAddr),
				lagSt.Render(lagStr),
				stateColor(r.State).Render(r.State),
			)
		}
		sections = append(sections, replSection)
	}

	// --- BGWriter ---
	if bg := m.snapshot.BGWriterStats; bg != nil && bg.MaxWrittenClean > 0 {
		bgSection := sectionHeader("Background Writer") + "\n" +
			fmt.Sprintf("  %s %s\n", styleLabel.Render("Buffers Cleaned:"),
				styleValue.Render(fmt.Sprintf("%d", bg.BuffersClean))) +
			fmt.Sprintf("  %s %s\n", styleLabel.Render("Max Written Stops:"),
				styleWarn.Render(fmt.Sprintf("%d", bg.MaxWrittenClean)))
		sections = append(sections, bgSection)
	}

	// --- Top Active Queries ---
	if len(m.snapshot.Activities) > 0 {
		querySection := sectionHeader("Active Queries") + "\n"
		count := 0
		for _, a := range m.snapshot.Activities {
			if a.State != "active" || a.Query == "" {
				continue
			}
			dur := formatQueryDuration(a.Duration)
			q := truncate(strings.ReplaceAll(a.Query, "\n", " "), 60)
			querySection += fmt.Sprintf("  %s %s %s %s\n",
				durStyle(a.Duration).Render(dur),
				lipgloss.NewStyle().Foreground(colorMuted).Render(fmt.Sprintf("[%s]", a.UserName)),
				lipgloss.NewStyle().Foreground(colorText).Render(q),
				lipgloss.NewStyle().Foreground(colorMuted).Render(fmt.Sprintf("(pid:%d)", a.PID)),
			)
			count++
			if count >= 5 {
				break
			}
		}
		if count == 0 {
			querySection += lipgloss.NewStyle().Foreground(colorMuted).Render("  No active queries") + "\n"
		}
		sections = append(sections, querySection)
	}

	// --- Quick Insights Summary ---
	insights := analyzer.Analyze(m.snapshot)
	if len(insights) > 0 {
		critCount := 0
		warnCount := 0
		for _, ins := range insights {
			switch ins.Severity {
			case analyzer.Critical:
				critCount++
			case analyzer.Warning:
				warnCount++
			}
		}
		insightLine := sectionHeader("Insights") + "  "
		if critCount > 0 {
			insightLine += styleBad.Render(fmt.Sprintf("%d critical", critCount)) + "  "
		}
		if warnCount > 0 {
			insightLine += styleWarn.Render(fmt.Sprintf("%d warnings", warnCount)) + "  "
		}
		insightLine += lipgloss.NewStyle().Foreground(colorMuted).Render(
			fmt.Sprintf("(%d total — press 7 for details)", len(insights)))
		sections = append(sections, insightLine)
	}

	return strings.Join(sections, "\n\n")
}

// trendIndicator returns a colored arrow showing the direction of change.
// If higherIsBetter is true, up arrow is green; if false, up arrow is amber/red.
func trendIndicator(current, previous float64, higherIsBetter bool) string {
	diff := current - previous
	if diff > 0 {
		if higherIsBetter {
			return styleGood.Render("↑")
		}
		return styleWarn.Render("↑")
	}
	if diff < 0 {
		if higherIsBetter {
			return styleWarn.Render("↓")
		}
		return styleGood.Render("↓")
	}
	return lipgloss.NewStyle().Foreground(colorMuted).Render("→")
}

// renderEmptyState renders a styled empty state box.
func renderEmptyState(title, subtitle string) string {
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#374151")).
		Padding(1, 3).
		Width(55).
		Render(
			lipgloss.NewStyle().Foreground(colorText).Bold(true).Render(title) + "\n" +
				lipgloss.NewStyle().Foreground(colorMuted).Render(subtitle),
		)
	return "\n  " + box + "\n"
}

func deadlockStyle(count int64) lipgloss.Style {
	if count > 0 {
		return styleBad
	}
	return styleGood
}

func rollbackStyle(ratio float64) lipgloss.Style {
	if ratio > 5 {
		return styleBad
	}
	if ratio > 1 {
		return styleWarn
	}
	return styleGood
}

func stateColor(state string) lipgloss.Style {
	switch state {
	case "streaming":
		return styleGood
	case "startup", "catchup":
		return styleWarn
	case "backup", "stopping":
		return lipgloss.NewStyle().Foreground(colorSecondary)
	default:
		return lipgloss.NewStyle().Foreground(colorMuted)
	}
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
