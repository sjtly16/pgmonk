package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// renderLocks renders the lock monitor view.
func (m Model) renderLocks() string {
	if m.snapshot == nil || len(m.snapshot.Locks) == 0 {
		return renderEmptyState("No Active Locks", "All clear — no locks detected")
	}

	var b strings.Builder

	// Summary: count granted vs waiting.
	granted, waiting, blocking := 0, 0, 0
	for _, l := range m.snapshot.Locks {
		if l.Granted {
			granted++
		} else {
			waiting++
		}
		if len(l.BlockedByPIDs) > 0 {
			blocking++
		}
	}

	b.WriteString(fmt.Sprintf("  Granted: %s  │  Waiting: %s  │  Blocked: %s\n\n",
		styleGood.Render(fmt.Sprintf("%d", granted)),
		waitingStyle(waiting).Render(fmt.Sprintf("%d", waiting)),
		waitingStyle(blocking).Render(fmt.Sprintf("%d", blocking)),
	))

	// Show waiting / blocked locks first (they're the interesting ones).
	if waiting > 0 || blocking > 0 {
		b.WriteString(styleSubtitle.Render("  ⚠ Blocked Sessions") + "\n")
		header := fmt.Sprintf("  %-7s %-10s %-8s %-20s %-25s %-12s %-9s %s",
			"PID", "Duration", "User", "Relation", "Mode", "Blocked By", "Status", "Query",
		)
		b.WriteString(styleHeader.Render(header))
		b.WriteString("\n")
		b.WriteString(m.renderDivider())
		b.WriteString("\n")

		for _, l := range m.snapshot.Locks {
			if l.Granted && len(l.BlockedByPIDs) == 0 {
				continue
			}

			blockers := ""
			if len(l.BlockedByPIDs) > 0 {
				pids := make([]string, len(l.BlockedByPIDs))
				for i, p := range l.BlockedByPIDs {
					pids[i] = fmt.Sprintf("%d", p)
				}
				blockers = strings.Join(pids, ",")
			}

			// Status indicator with consistent styling.
			statusStr := ""
			if !l.Granted {
				statusStr = styleBad.Render("● WAITING")
			} else {
				statusStr = styleGood.Render("● granted")
			}

			// Lock wait duration.
			durStr := "-"
			if l.Duration > 0 {
				durStr = formatShortDuration(l.Duration)
			}

			query := truncate(strings.ReplaceAll(l.Query, "\n", " "), 35)
			relation := l.RelationName
			if relation == "" {
				relation = l.LockType
			}

			b.WriteString(fmt.Sprintf("  %-7d %s %-8s %-20s %-25s %-12s %s %s\n",
				l.PID,
				durStyle(l.Duration).Width(10).Render(durStr),
				truncate(l.UserName, 8),
				truncate(relation, 20),
				l.Mode,
				blockers,
				statusStr,
				lipgloss.NewStyle().Foreground(colorMuted).Render(query),
			))
		}
		b.WriteString("\n")
	}

	// All locks summary by type.
	locksByType := map[string]int{}
	for _, l := range m.snapshot.Locks {
		locksByType[l.LockType]++
	}

	b.WriteString(styleSubtitle.Render("  Lock Summary by Type") + "\n")
	for lt, count := range locksByType {
		b.WriteString(fmt.Sprintf("  %-25s %s\n",
			lipgloss.NewStyle().Foreground(colorMuted).Render(lt),
			styleValue.Render(fmt.Sprintf("%d", count)),
		))
	}

	return b.String()
}

func waitingStyle(n int) lipgloss.Style {
	if n > 0 {
		return styleBad
	}
	return styleGood
}
