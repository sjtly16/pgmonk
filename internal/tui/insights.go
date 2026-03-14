package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/shashank/pgmonk/internal/analyzer"
)

// renderInsights renders the real-time analyzer insights view.
func (m Model) renderInsights() string {
	if m.snapshot == nil {
		return "  No data yet..."
	}

	insights := analyzer.Analyze(m.snapshot)

	var b strings.Builder

	if len(insights) == 0 {
		b.WriteString("  " + styleGood.Render("✓ No issues detected — database looks healthy!") + "\n")
		return b.String()
	}

	// Count by severity.
	critCount, warnCount, infoCount := 0, 0, 0
	for _, ins := range insights {
		switch ins.Severity {
		case analyzer.Critical:
			critCount++
		case analyzer.Warning:
			warnCount++
		case analyzer.Info:
			infoCount++
		}
	}

	b.WriteString(fmt.Sprintf("  %s  %s  %s  Total: %d\n\n",
		styleBad.Render(fmt.Sprintf("● critical: %d", critCount)),
		styleWarn.Render(fmt.Sprintf("● warning: %d", warnCount)),
		lipgloss.NewStyle().Foreground(colorSecondary).Render(fmt.Sprintf("● info: %d", infoCount)),
		len(insights),
	))

	maxRows := 20
	if m.height > 0 {
		maxRows = (m.height - 10) / 3 // 3 lines per insight
		if maxRows < 3 {
			maxRows = 3
		}
	}

	shown := 0
	// Show critical first, then warning, then info.
	for _, sev := range []analyzer.Severity{analyzer.Critical, analyzer.Warning, analyzer.Info} {
		for _, ins := range insights {
			if ins.Severity != sev {
				continue
			}
			if shown >= maxRows {
				remaining := len(insights) - shown
				b.WriteString(lipgloss.NewStyle().Foreground(colorMuted).Render(
					fmt.Sprintf("\n  ... and %d more insights", remaining)))
				b.WriteString("\n")
				return b.String()
			}

			icon := severityIcon(ins.Severity)
			titleStyle := severityStyle(ins.Severity)

			// Responsive text width.
			textWidth := m.contentWidth() - 8
			if textWidth < 40 {
				textWidth = 40
			}

			b.WriteString(fmt.Sprintf("  %s %s  %s\n",
				icon,
				titleStyle.Render(ins.Title),
				lipgloss.NewStyle().Foreground(colorMuted).Render("["+ins.Category+"]"),
			))
			b.WriteString(fmt.Sprintf("     %s\n",
				lipgloss.NewStyle().Foreground(colorText).Render(truncate(ins.Description, textWidth)),
			))
			b.WriteString(fmt.Sprintf("     %s %s\n",
				lipgloss.NewStyle().Foreground(colorAccent).Render("→"),
				lipgloss.NewStyle().Foreground(colorMuted).Render(truncate(ins.Suggestion, textWidth-2)),
			))
			shown++
		}
	}

	return b.String()
}

func severityIcon(s analyzer.Severity) string {
	switch s {
	case analyzer.Critical:
		return styleBad.Render("●")
	case analyzer.Warning:
		return styleWarn.Render("●")
	default:
		return lipgloss.NewStyle().Foreground(colorSecondary).Render("●")
	}
}

func severityStyle(s analyzer.Severity) lipgloss.Style {
	switch s {
	case analyzer.Critical:
		return styleBad
	case analyzer.Warning:
		return styleWarn
	default:
		return lipgloss.NewStyle().Foreground(colorSecondary).Bold(true)
	}
}
