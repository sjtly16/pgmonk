package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/shashank/pgmonk/internal/model"
)

// renderTables renders the table health view.
func (m Model) renderTables() string {
	if m.snapshot == nil || len(m.snapshot.Tables) == 0 {
		return renderEmptyState("No Table Statistics", "Stats collector may need time to populate")
	}

	var b strings.Builder
	w := m.contentWidth()
	innerW := w - 7

	// Sort the tables.
	tables := make([]model.TableStat, len(m.snapshot.Tables))
	copy(tables, m.snapshot.Tables)
	sortTables(tables, m.tableSortBy)

	// Summary.
	totalDead := int64(0)
	needsVacuum := 0
	for _, t := range tables {
		totalDead += t.DeadTuples
		if t.DeadTuples > 1000 {
			needsVacuum++
		}
	}
	summaryLine := fmt.Sprintf("Tables: %d  │  Total dead tuples: %s  │  Needs vacuum: %s",
		len(tables),
		styleValue.Render(formatCallCount(totalDead)),
		vacuumCountStyle(needsVacuum).Render(fmt.Sprintf("%d", needsVacuum)),
	)
	sortLine := lipgloss.NewStyle().Foreground(colorAccent).Render(
		fmt.Sprintf("Sorted by: %s (press 's' to cycle)", m.tableSortBy))
	summaryContent := summaryLine + "\n" + kvDivider(innerW) + "\n" + sortLine
	b.WriteString(dashBox(innerW).Render(summaryContent))
	b.WriteString("\n")

	// Dynamic column widths based on terminal width.
	showLive := w > 90
	showIdxScan := w > 100
	showVacuum := w > 115
	nameWidth := 25
	if w > 120 {
		nameWidth = 30
	}

	// Table header.
	hdr := fmt.Sprintf("  %-*s %-10s %-10s", nameWidth, "Table", "Size", "Dead")
	if showLive {
		hdr += fmt.Sprintf(" %-10s", "Live")
	}
	hdr += fmt.Sprintf(" %-12s", "Seq Scan")
	if showIdxScan {
		hdr += fmt.Sprintf(" %-12s", "Idx Scan")
	}
	if showVacuum {
		hdr += " Last Vacuum"
	}
	b.WriteString(styleHeader.Render(hdr))
	b.WriteString("\n")
	b.WriteString(m.renderDivider())
	b.WriteString("\n")

	// Rows with scrolling.
	max := m.maxRows()
	end := m.scrollOffset + max
	if end > len(tables) {
		end = len(tables)
	}

	for i := m.scrollOffset; i < end; i++ {
		t := tables[i]
		isSelected := i == m.selectedRow

		tableName := t.SchemaName + "." + t.TableName
		if t.SchemaName == "public" {
			tableName = t.TableName
		}

		deadStr := formatCallCount(t.DeadTuples)
		deadSt := lipgloss.NewStyle().Foreground(colorText)
		if t.DeadTuples > 10000 {
			deadSt = styleBad
		} else if t.DeadTuples > 1000 {
			deadSt = styleWarn
		}

		// Seq scan vs index scan ratio.
		seqScanStr := formatCallCount(t.SeqScan)
		seqSt := lipgloss.NewStyle().Foreground(colorText)
		if t.SeqScan > 1000 && t.IdxScan == 0 && t.LiveTuples > 10000 {
			seqSt = styleBad
		}

		lastVac := "never"
		if t.LastAutoVacuum != nil {
			lastVac = timeAgo(*t.LastAutoVacuum)
		} else if t.LastVacuum != nil {
			lastVac = timeAgo(*t.LastVacuum)
		}

		line := fmt.Sprintf("  %-*s %-10s %s",
			nameWidth, truncate(tableName, nameWidth),
			formatBytes(t.TotalSize),
			deadSt.Width(10).Render(deadStr),
		)
		if showLive {
			line += " " + fmt.Sprintf("%-10s", formatCallCount(t.LiveTuples))
		}
		line += " " + seqSt.Width(12).Render(seqScanStr)
		if showIdxScan {
			line += " " + fmt.Sprintf("%-12s", formatCallCount(t.IdxScan))
		}
		if showVacuum {
			line += " " + lipgloss.NewStyle().Foreground(colorMuted).Render(lastVac)
		}

		if isSelected {
			line = styleSelectedRow.Render("▸" + line[1:])
		}

		b.WriteString(line + "\n")
	}

	// Scroll indicator.
	b.WriteString(m.renderScrollInfo(len(tables)))
	b.WriteString("\n")

	return b.String()
}

func sortTables(tables []model.TableStat, by string) {
	sort.Slice(tables, func(i, j int) bool {
		switch by {
		case "size":
			return tables[i].TotalSize > tables[j].TotalSize
		case "seq_scan":
			return tables[i].SeqScan > tables[j].SeqScan
		case "last_vacuum":
			ti := lastVacuumTime(tables[i])
			tj := lastVacuumTime(tables[j])
			// Never vacuumed sorts first (most urgent).
			if ti.IsZero() && !tj.IsZero() {
				return true
			}
			if !ti.IsZero() && tj.IsZero() {
				return false
			}
			return ti.Before(tj) // Oldest vacuum first.
		case "name":
			return tables[i].TableName < tables[j].TableName
		default: // "dead_tuples"
			return tables[i].DeadTuples > tables[j].DeadTuples
		}
	})
}

func lastVacuumTime(t model.TableStat) time.Time {
	if t.LastAutoVacuum != nil {
		return *t.LastAutoVacuum
	}
	if t.LastVacuum != nil {
		return *t.LastVacuum
	}
	return time.Time{}
}

func vacuumCountStyle(n int) lipgloss.Style {
	if n > 5 {
		return styleBad
	} else if n > 0 {
		return styleWarn
	}
	return styleGood
}

func timeAgo(t time.Time) string {
	d := time.Since(t)
	switch {
	case d >= 24*time.Hour*30:
		return fmt.Sprintf("%.0fmo ago", d.Hours()/(24*30))
	case d >= 24*time.Hour:
		return fmt.Sprintf("%.0fd ago", d.Hours()/24)
	case d >= time.Hour:
		return fmt.Sprintf("%.0fh ago", d.Hours())
	case d >= time.Minute:
		return fmt.Sprintf("%.0fm ago", d.Minutes())
	default:
		return "just now"
	}
}
