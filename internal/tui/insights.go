package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/shashank/pgmonk/internal/analyzer"
)

// renderInsights renders the insights view with AI section at top and local rules below.
func (m Model) renderInsights() string {
	if m.snapshot == nil {
		return "  No data yet..."
	}

	w := m.contentWidth()
	innerW := w - 7

	var b strings.Builder

	// ── AI-Powered Insights section ──────────────────────────────────────────
	b.WriteString(m.renderAISection(innerW))

	// ── Local Rules section ──────────────────────────────────────────────────
	b.WriteString(m.renderLocalRulesSection(innerW))

	return b.String()
}

func (m Model) renderAISection(innerW int) string {
	var b strings.Builder

	aiHeader := lipgloss.NewStyle().Foreground(colorPrimary).Bold(true).Render("  🤖 AI-Powered Insights")
	b.WriteString(aiHeader)
	b.WriteString("\n")

	if m.apiKey == "" {
		msg := lipgloss.NewStyle().Foreground(colorMuted).Render(
			"  Add your API key to ~/.pgmonk.json to enable AI insights.\n" +
				"  Get a free key at pgmonk.dev")
		b.WriteString(dashBox(innerW).Render(msg))
		b.WriteString("\n")
		return b.String()
	}

	if m.aiLoading {
		loading := lipgloss.NewStyle().Foreground(colorAccent).Render("  Analyzing with Claude AI...")
		b.WriteString(dashBox(innerW).Render(loading))
		b.WriteString("\n")
		return b.String()
	}

	if m.aiError != "" {
		errMsg := styleWarn.Render("  ⚠ " + m.aiError)
		b.WriteString(dashBox(innerW).Render(errMsg))
		b.WriteString("\n")
		return b.String()
	}

	if m.aiLastUpdated.IsZero() {
		msg := lipgloss.NewStyle().Foreground(colorMuted).Render("  AI analysis pending...")
		b.WriteString(dashBox(innerW).Render(msg))
		b.WriteString("\n")
		return b.String()
	}

	// Usage counter.
	var usageStr string
	if m.aiUsage.Limit > 0 {
		usageStr = lipgloss.NewStyle().Foreground(colorMuted).Render(
			fmt.Sprintf("  %d/%d %s analyses today", m.aiUsage.AnalysesToday, m.aiUsage.Limit, m.aiUsage.Tier))
	}

	if len(m.aiInsights) == 0 {
		content := styleGood.Render("  AI analysis complete — no critical issues found")
		if usageStr != "" {
			content += "\n" + usageStr
		}
		b.WriteString(dashBox(innerW).Render(content))
		b.WriteString("\n")
		return b.String()
	}

	// Count by severity.
	critCount, warnCount, infoCount := 0, 0, 0
	for _, ins := range m.aiInsights {
		switch ins.Severity {
		case "critical":
			critCount++
		case "warning":
			warnCount++
		default:
			infoCount++
		}
	}

	summaryParts := []string{
		styleBad.Render(fmt.Sprintf("● critical: %d", critCount)),
		styleWarn.Render(fmt.Sprintf("● warning: %d", warnCount)),
		lipgloss.NewStyle().Foreground(colorSecondary).Render(fmt.Sprintf("● info: %d", infoCount)),
		lipgloss.NewStyle().Foreground(colorMuted).Render(fmt.Sprintf("Total: %d", len(m.aiInsights))),
	}
	if usageStr != "" {
		summaryParts = append(summaryParts, usageStr)
	}
	summaryLine := strings.Join(summaryParts, "  ")
	b.WriteString(dashBox(innerW).Render(summaryLine))
	b.WriteString("\n")

	maxRows := (m.height - 20) / 3
	if maxRows < 2 {
		maxRows = 2
	}
	textWidth := innerW - 6
	if textWidth < 40 {
		textWidth = 40
	}

	shown := 0
	for _, sev := range []string{"critical", "warning", "info"} {
		for _, ins := range m.aiInsights {
			if ins.Severity != sev {
				continue
			}
			if shown >= maxRows {
				remaining := len(m.aiInsights) - shown
				b.WriteString(lipgloss.NewStyle().Foreground(colorMuted).PaddingLeft(2).Render(
					fmt.Sprintf("... and %d more AI insights", remaining)))
				b.WriteString("\n")
				return b.String()
			}

			icon := aiSeverityIcon(ins.Severity)
			titleSty := aiSeverityStyle(ins.Severity)

			content := fmt.Sprintf("%s %s  %s\n%s\n%s %s",
				icon,
				titleSty.Render(ins.Title),
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

func (m Model) renderLocalRulesSection(innerW int) string {
	insights := analyzer.Analyze(m.snapshot)

	var b strings.Builder

	dividerLabel := lipgloss.NewStyle().Foreground(colorMuted).Render("  ─── Local Rules (" + fmt.Sprintf("%d checks", 27) + ") ───")
	b.WriteString(dividerLabel)
	b.WriteString("\n")

	if len(insights) == 0 {
		content := styleGood.Render("All checks passed — database looks healthy!")
		b.WriteString(dashBox(innerW).Render(content))
		b.WriteString("\n")
		return b.String()
	}

	// Remaining rows budget after AI section.
	maxRows := (m.height - 20) / 3
	if maxRows < 2 {
		maxRows = 2
	}
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
			if shown >= maxRows {
				remaining := len(insights) - shown
				b.WriteString(lipgloss.NewStyle().Foreground(colorMuted).PaddingLeft(2).Render(
					fmt.Sprintf("... and %d more insights", remaining)))
				b.WriteString("\n")
				return b.String()
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

func aiSeverityIcon(s string) string {
	switch s {
	case "critical":
		return styleBad.Render("●")
	case "warning":
		return styleWarn.Render("●")
	default:
		return lipgloss.NewStyle().Foreground(colorSecondary).Render("●")
	}
}

func aiSeverityStyle(s string) lipgloss.Style {
	switch s {
	case "critical":
		return styleBad
	case "warning":
		return styleWarn
	default:
		return lipgloss.NewStyle().Foreground(colorSecondary).Bold(true)
	}
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
