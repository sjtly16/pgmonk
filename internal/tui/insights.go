package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/sjtly16/pgmonk/internal/analyzer"
)

// renderInsights renders the rule-based insights view.
func (m Model) renderInsights() string {
	if m.snapshot == nil {
		return "  No data yet..."
	}

	w := m.contentWidth()
	innerW := w - 7

	insights := analyzer.Analyze(m.snapshot)

	var b strings.Builder
	b.WriteString(m.renderAISection(innerW))
	b.WriteString(dashSectionTitle("RULE-BASED CHECKS") + "\n")

	if len(insights) == 0 {
		content := styleGood.Render("All checks passed — database looks healthy!")
		b.WriteString(dashBox(innerW).Render(content))
		b.WriteString("\n")
		return b.String()
	}

	// Summary line.
	critCount, warnCount, infoCount := 0, 0, 0
	for _, ins := range insights {
		switch ins.Severity {
		case analyzer.Critical:
			critCount++
		case analyzer.Warning:
			warnCount++
		default:
			infoCount++
		}
	}
	summaryLine := fmt.Sprintf("%s  %s  %s  │  Total: %d",
		styleBad.Render(fmt.Sprintf("● critical: %d", critCount)),
		styleWarn.Render(fmt.Sprintf("● warning: %d", warnCount)),
		lipgloss.NewStyle().Foreground(colorSecondary).Render(fmt.Sprintf("● info: %d", infoCount)),
		len(insights),
	)
	b.WriteString(dashBox(innerW).Render(summaryLine))
	b.WriteString("\n")

	textWidth := innerW - 6
	if textWidth < 40 {
		textWidth = 40
	}

	shown := 0
	for _, sev := range []analyzer.Severity{analyzer.Critical, analyzer.Warning, analyzer.Info} {
		for _, ins := range insights {
			if ins.Severity != sev {
				continue
			}
			icon := severityIcon(ins.Severity)
			titleStyle := severityStyle(ins.Severity)

			content := fmt.Sprintf("%s %s  %s\n%s\n%s %s",
				icon,
				titleStyle.Render(ins.Title),
				lipgloss.NewStyle().Foreground(colorMuted).Render("["+ins.Category+"]"),
				lipgloss.NewStyle().Foreground(colorText).Render(truncate(ins.Description, textWidth)),
				lipgloss.NewStyle().Foreground(colorAccent).Render("->"),
				lipgloss.NewStyle().Foreground(colorMuted).Render(truncate(ins.Suggestion, textWidth-3)),
			)
			b.WriteString(dashBox(innerW).Render(content))
			b.WriteString("\n")
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
