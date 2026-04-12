package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/shashank/pgmonk/internal/model"
)

// View represents a TUI screen tab.
type View int

const (
	ViewDashboard View = iota
	ViewSessions
	ViewQueries
	ViewTables
	ViewLocks
	ViewClients
	ViewInsights
)

var viewNames = []string{"Dashboard", "Sessions", "Queries", "Tables", "Locks", "Clients", "Insights"}

// SnapshotMsg is sent when a new metric snapshot arrives from the collector.
type SnapshotMsg struct {
	Snapshot *model.Snapshot
}

// Model is the main Bubble Tea application model.
type Model struct {
	snapshot       *model.Snapshot
	prevSnapshot   *model.Snapshot
	serverInfo     model.ServerInfo
	currentView    View
	width          int
	height         int
	ready          bool
	querySortBy    string
	tableSortBy    string
	clientSortBy   string
	sessionSortBy  string
	expandedClient string
	hasStatStmts   bool
	showHelpModal  bool
	showDetail     bool
	scrollOffset   int
	selectedRow    int

}

// NewModel creates a new TUI model.
func NewModel(serverInfo model.ServerInfo) Model {
	return Model{
		serverInfo:    serverInfo,
		currentView:   ViewDashboard,
		querySortBy:   "total_time",
		tableSortBy:   "dead_tuples",
		clientSortBy:  "connections",
		sessionSortBy: "state",
		hasStatStmts:  serverInfo.HasStatStatements,
	}
}

// contentWidth returns the usable content width.
func (m Model) contentWidth() int {
	if m.width <= 0 {
		return 80 // fallback before WindowSizeMsg
	}
	w := m.width - 4
	if w < 40 {
		w = 40
	}
	return w
}

// maxRows returns the number of data rows that fit in the view.
func (m Model) maxRows() int {
	overhead := 12
	if m.showDetail {
		overhead = 22
	}
	rows := m.height - overhead
	if rows < 5 {
		rows = 5
	}
	return rows
}

// dataLen returns the number of data items for the current view.
func (m Model) dataLen() int {
	if m.snapshot == nil {
		return 0
	}
	switch m.currentView {
	case ViewSessions:
		return len(m.snapshot.Activities)
	case ViewQueries:
		return len(m.snapshot.Queries)
	case ViewTables:
		return len(m.snapshot.Tables)
	case ViewLocks:
		return len(m.snapshot.Locks)
	default:
		return 0
	}
}

// clampScroll ensures selectedRow and scrollOffset are within bounds.
func (m *Model) clampScroll() {
	total := m.dataLen()
	if total == 0 {
		m.selectedRow = 0
		m.scrollOffset = 0
		return
	}
	if m.selectedRow >= total {
		m.selectedRow = total - 1
	}
	if m.selectedRow < 0 {
		m.selectedRow = 0
	}
	max := m.maxRows()
	if m.selectedRow < m.scrollOffset {
		m.scrollOffset = m.selectedRow
	}
	if m.selectedRow >= m.scrollOffset+max {
		m.scrollOffset = m.selectedRow - max + 1
	}
	if m.scrollOffset < 0 {
		m.scrollOffset = 0
	}
}

// resetScroll resets scroll state.
func (m *Model) resetScroll() {
	m.scrollOffset = 0
	m.selectedRow = 0
	m.showDetail = false
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.KeyMsg:
		// Help modal takes precedence.
		if m.showHelpModal {
			switch msg.String() {
			case "?", "esc", "enter", "q":
				m.showHelpModal = false
			}
			return m, nil
		}

		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "?":
			m.showHelpModal = true
		case "1":
			m.currentView = ViewDashboard
			m.resetScroll()
		case "2":
			m.currentView = ViewSessions
			m.resetScroll()
		case "3":
			if m.hasStatStmts {
				m.currentView = ViewQueries
				m.resetScroll()
			}
		case "4":
			m.currentView = ViewTables
			m.resetScroll()
		case "5":
			m.currentView = ViewLocks
			m.resetScroll()
		case "6":
			m.currentView = ViewClients
			m.expandedClient = ""
			m.resetScroll()
		case "7":
			m.currentView = ViewInsights
			m.resetScroll()
		case "tab":
			next := int(m.currentView) + 1
			if next >= len(viewNames) {
				next = 0
			}
			if View(next) == ViewQueries && !m.hasStatStmts {
				next++
			}
			if next >= len(viewNames) {
				next = 0
			}
			m.currentView = View(next)
			m.resetScroll()
		case "shift+tab":
			prev := int(m.currentView) - 1
			if prev < 0 {
				prev = len(viewNames) - 1
			}
			if View(prev) == ViewQueries && !m.hasStatStmts {
				prev--
			}
			if prev < 0 {
				prev = len(viewNames) - 1
			}
			m.currentView = View(prev)
			m.resetScroll()
		// Sort keys for specific views.
		case "s":
			switch m.currentView {
			case ViewQueries:
				switch m.querySortBy {
				case "total_time":
					m.querySortBy = "mean_time"
				case "mean_time":
					m.querySortBy = "calls"
				case "calls":
					m.querySortBy = "rows"
				default:
					m.querySortBy = "total_time"
				}
				m.resetScroll()
			case ViewSessions:
				switch m.sessionSortBy {
				case "state":
					m.sessionSortBy = "duration"
				case "duration":
					m.sessionSortBy = "user"
				case "user":
					m.sessionSortBy = "pid"
				default:
					m.sessionSortBy = "state"
				}
				m.resetScroll()
			case ViewTables:
				switch m.tableSortBy {
				case "dead_tuples":
					m.tableSortBy = "size"
				case "size":
					m.tableSortBy = "seq_scan"
				case "seq_scan":
					m.tableSortBy = "last_vacuum"
				case "last_vacuum":
					m.tableSortBy = "name"
				default:
					m.tableSortBy = "dead_tuples"
				}
				m.resetScroll()
			case ViewClients:
				switch m.clientSortBy {
				case "connections":
					m.clientSortBy = "active"
				case "active":
					m.clientSortBy = "duration"
				default:
					m.clientSortBy = "connections"
				}
			}
		// Row selection and detail panel.
		case "enter":
			switch m.currentView {
			case ViewSessions, ViewQueries:
				if m.dataLen() > 0 {
					m.showDetail = !m.showDetail
					m.clampScroll()
				}
			case ViewClients:
				if m.expandedClient == "" {
					if m.snapshot != nil && len(m.snapshot.Clients) > 0 {
						m.expandedClient = m.snapshot.Clients[0].ClientAddr
					}
				}
			}
		case "esc", "backspace":
			if m.showDetail {
				m.showDetail = false
			} else if m.currentView == ViewClients && m.expandedClient != "" {
				m.expandedClient = ""
			}
		// Scroll and navigate with j/k or arrow keys.
		case "j", "down":
			switch m.currentView {
			case ViewSessions, ViewQueries, ViewTables, ViewLocks:
				m.selectedRow++
				m.clampScroll()
			case ViewClients:
				if m.expandedClient != "" {
					m.expandedClient = m.nextClient(m.expandedClient)
				}
			}
		case "k", "up":
			switch m.currentView {
			case ViewSessions, ViewQueries, ViewTables, ViewLocks:
				m.selectedRow--
				m.clampScroll()
			case ViewClients:
				if m.expandedClient != "" {
					m.expandedClient = m.prevClient(m.expandedClient)
				}
			}
		case "g":
			m.selectedRow = 0
			m.clampScroll()
		case "G":
			m.selectedRow = m.dataLen() - 1
			m.clampScroll()
		}

	case SnapshotMsg:
		m.prevSnapshot = m.snapshot
		m.snapshot = msg.Snapshot
		m.ready = true
		m.clampScroll()

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	}

	return m, nil
}

// View implements tea.Model.
func (m Model) View() string {
	if !m.ready {
		return "\n  Connecting to PostgreSQL and collecting metrics...\n"
	}

	// Help modal overlay.
	if m.showHelpModal {
		return m.renderHelpModal()
	}

	var b strings.Builder

	// Header bar.
	b.WriteString(m.renderHeader())
	b.WriteString("\n")

	// Tab bar.
	b.WriteString(m.renderTabs())
	b.WriteString("\n")

	// Count header lines for available height calculation.
	headerStr := b.String()
	headerLines := strings.Count(headerStr, "\n")

	// Footer.
	footer := m.renderHelp()

	// Available height for content (header + content + footer = m.height).
	availableHeight := m.height - headerLines - 1
	if availableHeight < 5 {
		availableHeight = 5
	}

	// Render main content area.
	var content string
	switch m.currentView {
	case ViewDashboard:
		content = m.renderDashboard()
	case ViewSessions:
		content = m.renderSessions()
	case ViewQueries:
		content = m.renderQueries()
	case ViewTables:
		content = m.renderTables()
	case ViewLocks:
		content = m.renderLocks()
	case ViewClients:
		content = m.renderClients()
	case ViewInsights:
		content = m.renderInsights()
	}

	contentLines := strings.Split(content, "\n")

	// Truncate to available height.
	if len(contentLines) > availableHeight {
		contentLines = contentLines[:availableHeight]
	}
	b.WriteString(strings.Join(contentLines, "\n"))

	// Pad remaining height with newlines.
	usedLines := headerLines + len(contentLines)
	for i := usedLines; i < m.height-1; i++ {
		b.WriteString("\n")
	}

	// Footer.
	b.WriteString("\n")
	b.WriteString(footer)

	// Use lipgloss.Place to enforce exact dimensions.
	// This prevents ANY line from wrapping and fills the full terminal.
	if m.width > 0 && m.height > 0 {
		return lipgloss.Place(m.width, m.height, lipgloss.Left, lipgloss.Top, b.String())
	}
	return b.String()
}

// renderHeader renders the top header bar with server info.
func (m Model) renderHeader() string {
	logo := styleLogo.Render("pg-monk")
	dot := lipgloss.NewStyle().Foreground(colorMuted).Render(" · ")
	dbName := styleValue.Render(m.snapshot.Overview.DatabaseName)

	// Extract short version string (e.g. "PostgreSQL 14.19" from longer string).
	ver := m.serverInfo.Version
	if idx := strings.Index(ver, " on "); idx > 0 {
		ver = ver[:idx]
	}
	version := lipgloss.NewStyle().Foreground(colorMuted).Render(ver)
	timestamp := lipgloss.NewStyle().Foreground(colorMuted).Render(m.snapshot.CollectedAt.Format("15:04:05"))

	left := logo + "  " + dbName + dot + version
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(timestamp) - 1
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + timestamp
}

// renderTabs renders the navigation tab bar.
func (m Model) renderTabs() string {
	// Calculate even tab width based on terminal width.
	tabCount := len(viewNames)
	tabW := 0
	if m.width > 0 && tabCount > 0 {
		tabW = m.width / tabCount
	}
	if tabW < 12 {
		tabW = 12
	}

	var tabs []string
	for i, name := range viewNames {
		label := fmt.Sprintf("%d %s", i+1, name)
		if View(i) == ViewQueries && !m.hasStatStmts {
			tabs = append(tabs, lipgloss.NewStyle().
				Foreground(lipgloss.Color("#374151")).
				Width(tabW).
				Align(lipgloss.Center).
				Render(label))
			continue
		}
		if View(i) == m.currentView {
			tabs = append(tabs, lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#FFFFFF")).
				Background(lipgloss.Color("#1E293B")).
				Width(tabW).
				Align(lipgloss.Center).
				Render(label))
		} else {
			tabs = append(tabs, lipgloss.NewStyle().
				Foreground(colorMuted).
				Width(tabW).
				Align(lipgloss.Center).
				Render(label))
		}
	}
	divW := m.width - 1
	if divW < 0 {
		divW = 0
	}
	divider := lipgloss.NewStyle().Foreground(lipgloss.Color("#374151")).
		Render(strings.Repeat("─", divW))
	return lipgloss.JoinHorizontal(lipgloss.Top, tabs...) + "\n" + divider
}

// renderHelp renders the footer help text.
func (m Model) renderHelp() string {
	sep := lipgloss.NewStyle().Foreground(lipgloss.Color("#374151")).Render(" │ ")
	key := func(k, desc string) string {
		return styleKeyBadge.Render(k) + " " + lipgloss.NewStyle().Foreground(colorMuted).Render(desc)
	}

	parts := []string{
		key("1-7", "view"),
		key("Tab", "cycle"),
		key("?", "help"),
		key("q", "quit"),
	}
	switch m.currentView {
	case ViewSessions, ViewTables:
		parts = append(parts, key("j/k", "scroll"), key("s", "sort"))
	case ViewQueries:
		parts = append(parts, key("j/k", "scroll"), key("s", "sort"), key("Enter", "detail"))
	case ViewLocks:
		parts = append(parts, key("j/k", "scroll"))
	case ViewClients:
		parts = append(parts, key("s", "sort"), key("Enter", "expand"), key("Esc", "back"))
	}
	if m.showDetail {
		parts = append(parts, key("Esc", "close"))
	}
	return " " + strings.Join(parts, sep)
}

// renderScrollInfo renders scroll position indicator.
func (m Model) renderScrollInfo(total int) string {
	if total == 0 {
		return ""
	}
	max := m.maxRows()
	if total <= max {
		return ""
	}
	from := m.scrollOffset + 1
	to := m.scrollOffset + max
	if to > total {
		to = total
	}
	return lipgloss.NewStyle().Foreground(colorMuted).Render(
		fmt.Sprintf("  rows %d-%d of %d", from, to, total))
}

// renderDivider renders a horizontal divider line matching the box border color.
func (m Model) renderDivider() string {
	w := m.contentWidth()
	if m.width > 0 && w > m.width-2 {
		w = m.width - 2
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color("#4B5563")).Render("  " + strings.Repeat("─", w))
}

// --- Help Modal ---

// renderHelpModal renders a keyboard shortcuts overlay.
func (m Model) renderHelpModal() string {
	title := styleLogo.Render(" pg-monk  Keyboard Shortcuts")

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorPrimary).
		Padding(1, 3).
		Width(62)

	keyStyle := lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Width(20)
	descStyle := lipgloss.NewStyle().Foreground(colorText)
	sectionStyle := lipgloss.NewStyle().Foreground(colorPrimary).Bold(true)

	bindings := [][2]string{
		{"1 - 7", "Switch to view by number"},
		{"Tab / Shift+Tab", "Cycle through views"},
		{"j / k  or  Up/Down", "Scroll through rows"},
		{"g / G", "Jump to top / bottom"},
		{"s", "Change sort order"},
		{"Enter", "Show detail / expand client"},
		{"Esc / Backspace", "Close detail / collapse"},
		{"?", "Toggle this help"},
		{"q / Ctrl+C", "Quit"},
	}

	var lines []string
	lines = append(lines, "")
	lines = append(lines, sectionStyle.Render("  Navigation"))
	lines = append(lines, "")
	for _, b := range bindings {
		lines = append(lines, "  "+keyStyle.Render(b[0])+descStyle.Render(b[1]))
	}

	lines = append(lines, "")
	lines = append(lines, sectionStyle.Render("  Views"))
	lines = append(lines, "")
	views := [][2]string{
		{"1 Dashboard", "Overview metrics, throughput, health"},
		{"2 Sessions", "Active pg_stat_activity sessions"},
		{"3 Queries", "Top queries (pg_stat_statements)"},
		{"4 Tables", "Table health, dead tuples, vacuum"},
		{"5 Locks", "Lock monitor, blocking chains"},
		{"6 Clients", "Per-client aggregation & drill-down"},
		{"7 Insights", "Real-time analyzer recommendations"},
	}
	for _, v := range views {
		lines = append(lines, "  "+keyStyle.Render(v[0])+descStyle.Render(v[1]))
	}

	lines = append(lines, "")
	lines = append(lines, lipgloss.NewStyle().Foreground(colorMuted).Render("  Press ? or Esc to close"))

	content := strings.Join(lines, "\n")
	box := boxStyle.Render(content)

	// Center the box.
	var sb strings.Builder
	sb.WriteString("\n")
	sb.WriteString(title)
	sb.WriteString("\n\n")
	sb.WriteString(box)
	return sb.String()
}

// --- Utility functions ---

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}

func formatBytes(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
		TB = GB * 1024
	)
	switch {
	case bytes >= TB:
		return fmt.Sprintf("%.1f TB", float64(bytes)/float64(TB))
	case bytes >= GB:
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(GB))
	case bytes >= MB:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(MB))
	case bytes >= KB:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(KB))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

func formatDuration(ms float64) string {
	switch {
	case ms >= 60000:
		return fmt.Sprintf("%.1fm", ms/60000)
	case ms >= 1000:
		return fmt.Sprintf("%.1fs", ms/1000)
	case ms >= 1:
		return fmt.Sprintf("%.0fms", ms)
	default:
		return fmt.Sprintf("%.2fms", ms)
	}
}

// nextClient returns the client address after the given one.
func (m Model) nextClient(current string) string {
	if m.snapshot == nil || len(m.snapshot.Clients) == 0 {
		return current
	}
	for i, c := range m.snapshot.Clients {
		if c.ClientAddr == current {
			if i+1 < len(m.snapshot.Clients) {
				return m.snapshot.Clients[i+1].ClientAddr
			}
			return current
		}
	}
	return current
}

// prevClient returns the client address before the given one.
func (m Model) prevClient(current string) string {
	if m.snapshot == nil || len(m.snapshot.Clients) == 0 {
		return current
	}
	for i, c := range m.snapshot.Clients {
		if c.ClientAddr == current {
			if i-1 >= 0 {
				return m.snapshot.Clients[i-1].ClientAddr
			}
			return current
		}
	}
	return current
}
