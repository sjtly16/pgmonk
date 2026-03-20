package tui

import "github.com/charmbracelet/lipgloss"

// Color palette — inspired by popular dark terminal themes.
var (
	colorPrimary   = lipgloss.Color("#7C3AED") // violet
	colorSecondary = lipgloss.Color("#06B6D4") // cyan
	colorSuccess   = lipgloss.Color("#22C55E") // green
	colorWarning   = lipgloss.Color("#F59E0B") // amber
	colorDanger    = lipgloss.Color("#EF4444") // red
	colorMuted     = lipgloss.Color("#6B7280") // gray
	colorText      = lipgloss.Color("#E5E7EB") // light gray
	colorAccent = lipgloss.Color("#A78BFA") // light violet
)

// Layout styles.
var (
	styleSubtitle = lipgloss.NewStyle().
			Foreground(colorAccent).
			Bold(true)

	styleLabel = lipgloss.NewStyle().
			Foreground(colorMuted).
			Width(20)

	styleValue = lipgloss.NewStyle().
			Foreground(colorText).
			Bold(true)

	styleGood = lipgloss.NewStyle().
			Foreground(colorSuccess).
			Bold(true)

	styleWarn = lipgloss.NewStyle().
			Foreground(colorWarning).
			Bold(true)

	styleBad = lipgloss.NewStyle().
			Foreground(colorDanger).
			Bold(true)

	styleActive = lipgloss.NewStyle().
			Foreground(colorSuccess)

	styleIdle = lipgloss.NewStyle().
			Foreground(colorMuted)

	styleIdleInTx = lipgloss.NewStyle().
			Foreground(colorWarning)

	styleWaiting = lipgloss.NewStyle().
			Foreground(colorDanger)

	styleTabActive = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(lipgloss.Color("#1E293B")).
			Padding(0, 2)

	styleTabInactive = lipgloss.NewStyle().
			Foreground(colorMuted).
			Padding(0, 2)

	styleHeader = lipgloss.NewStyle().
			Foreground(colorSecondary).
			Bold(true)

	styleSectionTitle = lipgloss.NewStyle().
				Foreground(colorAccent).
				Bold(true).
				PaddingLeft(1)

	styleKeyBadge = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(lipgloss.Color("#374151")).
			Padding(0, 1).
			Bold(true)

	styleLogo = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorPrimary)

	styleSelectedRow = lipgloss.NewStyle().
				Background(lipgloss.Color("#1E293B"))
)

// stateStyle returns the appropriate style for a session state.
func stateStyle(state string) lipgloss.Style {
	switch state {
	case "active":
		return styleActive
	case "idle":
		return styleIdle
	case "idle in transaction", "idle in transaction (aborted)":
		return styleIdleInTx
	default:
		return styleWaiting
	}
}

// ratioStyle returns a style based on the cache hit ratio value.
func ratioStyle(ratio float64) lipgloss.Style {
	switch {
	case ratio >= 99.0:
		return styleGood
	case ratio >= 95.0:
		return styleWarn
	default:
		return styleBad
	}
}
