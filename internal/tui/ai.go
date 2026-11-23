package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/sjtly16/pgmonk/internal/ai"
)

// aiStaleAfter is how old an AI result can be before entering the Insights
// tab triggers a new analysis. Local inference is slow, so never on a timer.
const aiStaleAfter = 5 * time.Minute

// Option configures the TUI model.
type Option func(*Model)

// WithAI enables AI insights on the Insights tab.
func WithAI(engine *ai.Engine, timeout time.Duration) Option {
	return func(m *Model) {
		m.ai = engine
		m.aiTimeout = timeout
	}
}

type aiResultMsg struct {
	res *ai.Result
	err error
}

type aiTickMsg time.Time

// startAI launches an analysis of the current snapshot.
func (m *Model) startAI() tea.Cmd {
	if m.ai == nil || m.aiLoading || m.snapshot == nil {
		return nil
	}
	m.aiLoading = true
	m.aiErr = ""
	m.aiStarted = time.Now()
	m.aiNow = m.aiStarted

	engine, snap, timeout := m.ai, m.snapshot, m.aiTimeout
	analyze := func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		res, err := engine.Analyze(ctx, snap)
		return aiResultMsg{res: res, err: err}
	}
	return tea.Batch(analyze, aiTick())
}

func aiTick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return aiTickMsg(t) })
}

// maybeAutoAI starts an analysis when the Insights tab is shown and there is
// no recent result.
func (m *Model) maybeAutoAI() tea.Cmd {
	if m.currentView != ViewInsights || m.ai == nil || m.aiLoading {
		return nil
	}
	if m.aiResult != nil && time.Since(m.aiResult.At) < aiStaleAfter {
		return nil
	}
	if m.aiResult == nil && m.aiErr != "" {
		return nil // don't hammer a broken endpoint; 'r' retries
	}
	return m.startAI()
}

// renderAISection renders the AI panel at the top of the Insights tab.
func (m Model) renderAISection(innerW int) string {
	var b strings.Builder
	muted := lipgloss.NewStyle().Foreground(colorMuted)
	header := lipgloss.NewStyle().Foreground(colorPrimary).Bold(true).Render("AI INSIGHTS")

	if m.ai == nil {
		b.WriteString(" " + header + muted.Render("  · off") + "\n")
		msg := muted.Render("Get root-cause analysis from a local LLM — nothing leaves your machine.\n") +
			lipgloss.NewStyle().Foreground(colorText).Render("ollama pull qwen2.5:7b") + muted.Render("  then restart with  ") +
			lipgloss.NewStyle().Foreground(colorText).Render("pg-monk --ai ...")
		b.WriteString(dashBox(innerW).Render(msg))
		b.WriteString("\n")
		return b.String()
	}

	p := m.ai.Pack()
	knowledge := fmt.Sprintf("PG %d knowledge", p.RefMajor)
	if !p.Exact {
		knowledge = fmt.Sprintf("PG %d knowledge (nearest to %d)", p.RefMajor, p.Major)
	}
	meta := muted.Render("  · " + m.ai.ModelName() + " · " + knowledge)

	switch {
	case m.aiLoading:
		b.WriteString(" " + header + meta + "\n")
		elapsed := m.aiNow.Sub(m.aiStarted).Round(time.Second)
		line := lipgloss.NewStyle().Foreground(colorAccent).Render(fmt.Sprintf("Analyzing… %s", elapsed))
		hint := ""
		if m.aiResult == nil {
			hint = "\n" + muted.Render("The first analysis loads the model and version knowledge; later ones are much faster.")
		}
		b.WriteString(dashBox(innerW).Render(line + hint))
		b.WriteString("\n")
		return b.String()

	case m.aiErr != "":
		b.WriteString(" " + header + meta + "\n")
		b.WriteString(dashBox(innerW).Render(styleWarn.Render("⚠ "+m.aiErr) + "\n" + muted.Render("Press r to retry · pg-monk ai check diagnoses the setup")))
		b.WriteString("\n")
		return b.String()

	case m.aiResult == nil:
		b.WriteString(" " + header + meta + "\n")
		b.WriteString(dashBox(innerW).Render(muted.Render("Press r to analyze the current snapshot.")))
		b.WriteString("\n")
		return b.String()
	}

	res := m.aiResult
	ago := time.Since(res.At).Round(time.Second)
	meta += muted.Render(fmt.Sprintf(" · %s ago in %s · r to refresh", ago, res.Elapsed.Round(100*time.Millisecond)))
	b.WriteString(" " + header + meta + "\n")

	textW := innerW - 4
	if textW < 40 {
		textW = 40
	}
	wrap := func(s string) string { return lipgloss.NewStyle().Width(textW).Render(s) }

	if res.Response == nil {
		body := styleWarn.Render("The model's answer wasn't valid JSON — showing it as-is:") + "\n" +
			lipgloss.NewStyle().Foreground(colorText).Render(wrap(strings.TrimSpace(res.Raw)))
		b.WriteString(dashBox(innerW).Render(body))
		b.WriteString("\n")
		return b.String()
	}

	r := res.Response
	summary := r.Summary
	if summary == "" {
		summary = "No summary."
	}
	b.WriteString(dashBox(innerW).Render(lipgloss.NewStyle().Foreground(colorText).Bold(true).Render(wrap(summary))))
	b.WriteString("\n")

	label := func(s string) string { return lipgloss.NewStyle().Foreground(colorSecondary).Render(s) }
	for _, f := range r.Findings {
		var lines []string
		lines = append(lines, aiSeverityIcon(f.Severity)+" "+aiSeverityStyle(f.Severity).Render(f.Title))
		if f.Evidence != "" {
			lines = append(lines, wrap(label("Evidence  ")+f.Evidence))
		}
		if f.RootCause != "" {
			lines = append(lines, wrap(label("Cause     ")+f.RootCause))
		}
		if f.Action != "" {
			lines = append(lines, wrap(label("Action    ")+f.Action))
		}
		if f.SQL != "" {
			sqlLabel, status := "Check     ", styleGood.Render("✓ verified on this server")
			switch {
			case f.SQLIsFix:
				sqlLabel, status = "Fix       ", muted.Render("review before running — pg-monk never runs changes")
			case !f.SQLVerified:
				status = styleWarn.Render("⚠ " + f.SQLError)
			}
			lines = append(lines, label(sqlLabel)+lipgloss.NewStyle().Foreground(colorText).Render(truncate(f.SQL, textW-12)))
			lines = append(lines, "          "+status)
		}
		for _, n := range f.Notes {
			lines = append(lines, styleWarn.Render("ℹ pg-monk: "+n))
		}
		if len(f.Unknown) > 0 {
			lines = append(lines, styleWarn.Render("⚠ mentions columns that don't exist on this server: "+strings.Join(f.Unknown, ", ")))
		}
		b.WriteString(dashBox(innerW).Render(strings.Join(lines, "\n")))
		b.WriteString("\n")
	}
	if len(r.Findings) == 0 {
		b.WriteString(dashBox(innerW).Render(styleGood.Render("No additional issues found.")))
		b.WriteString("\n")
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
