package tui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/shashank/pgmonk/internal/model"
)

// renderSessions renders the active sessions view.
func (m Model) renderSessions() string {
	if m.snapshot == nil || len(m.snapshot.Activities) == 0 {
		return renderEmptyState("No Active Sessions", "All backends are idle or disconnected")
	}

	var b strings.Builder
	w := m.contentWidth()
	innerW := w - 7

	// Sort the activities.
	activities := make([]model.Activity, len(m.snapshot.Activities))
	copy(activities, m.snapshot.Activities)
	sortSessions(activities, m.sessionSortBy)

	// Summary counts.
	active, idle, idleTx, waiting := 0, 0, 0, 0
	for _, a := range activities {
		switch a.State {
		case "active":
			active++
		case "idle":
			idle++
		case "idle in transaction", "idle in transaction (aborted)":
			idleTx++
		}
		if a.WaitEventType != "" && a.State == "active" {
			waiting++
		}
	}

	summaryLine := fmt.Sprintf("%s %d   %s %d   %s %d   %s %d   │  Total: %d",
		styleActive.Render("● active:"), active,
		styleIdle.Render("● idle:"), idle,
		styleIdleInTx.Render("● idle-tx:"), idleTx,
		styleWaiting.Render("● waiting:"), waiting,
		len(activities),
	)
	sortLine := lipgloss.NewStyle().Foreground(colorAccent).Render(
		fmt.Sprintf("Sorted by: %s (press 's' to cycle)", m.sessionSortBy))

	summaryContent := summaryLine + "\n" + kvDivider(innerW) + "\n" + sortLine
	b.WriteString(dashBox(innerW).Render(summaryContent))
	b.WriteString("\n")

	// Dynamic column widths based on terminal width.
	showWait := w > 100
	showBackend := w > 120
	showApp := w > 90

	fixedCols := 7 + 1 + 8 + 1 + 12 + 1 + 8 + 1 // PID+Dur+User+State + gaps
	if showApp {
		fixedCols += 15 + 1
	}
	if showWait {
		fixedCols += 18 + 1
	}
	if showBackend {
		fixedCols += 10 + 1
	}
	queryWidth := w - fixedCols - 2
	if queryWidth < 10 {
		queryWidth = 10
	}

	// Table header.
	var headerParts []string
	headerParts = append(headerParts, fmt.Sprintf("%-7s", "PID"))
	headerParts = append(headerParts, fmt.Sprintf("%-8s", "Duration"))
	headerParts = append(headerParts, fmt.Sprintf("%-12s", "User"))
	if showApp {
		headerParts = append(headerParts, fmt.Sprintf("%-15s", "App"))
	}
	headerParts = append(headerParts, fmt.Sprintf("%-8s", "State"))
	if showWait {
		headerParts = append(headerParts, fmt.Sprintf("%-18s", "Wait"))
	}
	if showBackend {
		headerParts = append(headerParts, fmt.Sprintf("%-10s", "Backend"))
	}
	headerParts = append(headerParts, "Query")
	header := "  " + strings.Join(headerParts, " ")
	b.WriteString(styleHeader.Render(header))
	b.WriteString("\n")
	b.WriteString(m.renderDivider())
	b.WriteString("\n")

	// Rows with scrolling.
	max := m.maxRows()
	end := m.scrollOffset + max
	if end > len(activities) {
		end = len(activities)
	}

	for i := m.scrollOffset; i < end; i++ {
		a := activities[i]
		isSelected := i == m.selectedRow

		dur := formatQueryDuration(a.Duration)
		state := stateStyle(a.State).Render(fmt.Sprintf("%-8s", a.State))
		wait := ""
		if a.WaitEventType != "" {
			wait = fmt.Sprintf("%s:%s", a.WaitEventType, a.WaitEvent)
		}
		query := truncate(strings.ReplaceAll(a.Query, "\n", " "), queryWidth)

		var rowParts []string
		rowParts = append(rowParts, fmt.Sprintf("%-7d", a.PID))
		rowParts = append(rowParts, durStyle(a.Duration).Width(8).Render(dur))
		rowParts = append(rowParts, fmt.Sprintf("%-12s", truncate(a.UserName, 12)))
		if showApp {
			rowParts = append(rowParts, fmt.Sprintf("%-15s", truncate(a.ApplicationName, 15)))
		}
		rowParts = append(rowParts, state)
		if showWait {
			rowParts = append(rowParts, fmt.Sprintf("%-18s", truncate(wait, 18)))
		}
		if showBackend {
			rowParts = append(rowParts, fmt.Sprintf("%-10s", truncate(a.BackendType, 10)))
		}
		rowParts = append(rowParts, lipgloss.NewStyle().Foreground(colorText).Render(query))
		line := "  " + strings.Join(rowParts, " ")

		if isSelected {
			line = styleSelectedRow.Render("▸" + line[1:])
		}

		b.WriteString(line + "\n")
	}

	// Scroll indicator.
	b.WriteString(m.renderScrollInfo(len(activities)))
	b.WriteString("\n")

	// Detail panel for selected session.
	if m.showDetail && m.selectedRow < len(activities) {
		b.WriteString(m.renderSessionDetail(activities[m.selectedRow]))
	}

	return b.String()
}

// renderSessionDetail renders the detail panel for a selected session.
func (m Model) renderSessionDetail(a model.Activity) string {
	w := m.contentWidth()
	innerW := w - 7
	if innerW > 100 {
		innerW = 100
	}

	lbl := lipgloss.NewStyle().Foreground(colorMuted)

	var rows []string
	rows = append(rows, fmt.Sprintf("%s %d    %s %s    %s %s    %s %s",
		lbl.Render("PID:"), a.PID,
		lbl.Render("State:"), stateStyle(a.State).Render(a.State),
		lbl.Render("User:"), styleValue.Render(a.UserName),
		lbl.Render("Database:"), styleValue.Render(a.DatabaseName),
	))
	rows = append(rows, fmt.Sprintf("%s %s    %s %s    %s %s",
		lbl.Render("App:"), styleValue.Render(a.ApplicationName),
		lbl.Render("Client:"), styleValue.Render(a.ClientAddr),
		lbl.Render("Backend:"), styleValue.Render(a.BackendType),
	))

	if a.WaitEventType != "" {
		rows = append(rows, fmt.Sprintf("%s %s:%s",
			lbl.Render("Wait Event:"),
			styleWarn.Render(a.WaitEventType),
			styleWarn.Render(a.WaitEvent),
		))
	}

	if a.Duration > 0 {
		rows = append(rows, fmt.Sprintf("%s %s",
			lbl.Render("Duration:"),
			durStyle(a.Duration).Render(formatQueryDuration(a.Duration)),
		))
	}

	if a.Query != "" {
		queryText := strings.ReplaceAll(a.Query, "\n", " ")
		queryText = strings.Join(strings.Fields(queryText), " ")
		maxLineLen := innerW - 2
		queryLines := lbl.Render("Query:")
		for len(queryText) > 0 {
			end := maxLineLen
			if end > len(queryText) {
				end = len(queryText)
			}
			queryLines += "\n  " + lipgloss.NewStyle().Foreground(colorText).Render(queryText[:end])
			queryText = queryText[end:]
		}
		rows = append(rows, queryLines)
	}

	content := joinWithDividers(rows, innerW)
	box := dashBox(innerW).Render(content)
	return "\n" + dashSectionTitle("SESSION DETAIL") + "\n" + box + "\n"
}

func sortSessions(activities []model.Activity, by string) {
	sort.Slice(activities, func(i, j int) bool {
		switch by {
		case "duration":
			return activities[i].Duration > activities[j].Duration
		case "user":
			if activities[i].UserName != activities[j].UserName {
				return activities[i].UserName < activities[j].UserName
			}
			return activities[i].Duration > activities[j].Duration
		case "pid":
			return activities[i].PID < activities[j].PID
		default: // "state" — original ordering: active > idle-tx > idle > other
			si, sj := stateOrder(activities[i].State), stateOrder(activities[j].State)
			if si != sj {
				return si < sj
			}
			return activities[i].Duration > activities[j].Duration
		}
	})
}

func stateOrder(state string) int {
	switch state {
	case "active":
		return 1
	case "idle in transaction", "idle in transaction (aborted)":
		return 2
	case "idle":
		return 3
	default:
		return 4
	}
}
