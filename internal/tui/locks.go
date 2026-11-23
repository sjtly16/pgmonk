package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/sjtly16/pgmonk/internal/model"
)

// lockChain is one node of a blocking tree: a backend plus the backends that
// are waiting on it.
type lockChain struct {
	PID      int
	Lock     *model.LockInfo // the ungranted lock this PID waits on (nil for root blockers)
	Children []*lockChain
}

// buildLockChains turns the flat pg_locks list into blocking trees rooted at
// the backends that hold locks others wait on but are not waiting themselves.
func buildLockChains(locks []model.LockInfo) []*lockChain {
	// One waiting lock per PID (a backend waits on at most one lock at a time).
	waiting := map[int]*model.LockInfo{}
	for i := range locks {
		l := &locks[i]
		if !l.Granted && len(l.BlockedByPIDs) > 0 {
			if _, ok := waiting[l.PID]; !ok {
				waiting[l.PID] = l
			}
		}
	}

	// blocker PID -> PIDs waiting on it.
	waitersOf := map[int][]int{}
	for pid, l := range waiting {
		for _, b := range l.BlockedByPIDs {
			waitersOf[b] = append(waitersOf[b], pid)
		}
	}
	for b := range waitersOf {
		sort.Ints(waitersOf[b])
	}

	// Roots: blockers that aren't themselves waiting. If every blocker is also
	// waiting (a cycle about to be resolved by the deadlock detector), fall back
	// to the lowest PID so the chain is still visible.
	var roots []int
	for b := range waitersOf {
		if _, isWaiting := waiting[b]; !isWaiting {
			roots = append(roots, b)
		}
	}
	if len(roots) == 0 && len(waitersOf) > 0 {
		lowest := -1
		for b := range waitersOf {
			if lowest == -1 || b < lowest {
				lowest = b
			}
		}
		roots = append(roots, lowest)
	}
	sort.Ints(roots)

	visited := map[int]bool{}
	var build func(pid int) *lockChain
	build = func(pid int) *lockChain {
		visited[pid] = true
		node := &lockChain{PID: pid, Lock: waiting[pid]}
		for _, w := range waitersOf[pid] {
			if !visited[w] {
				node.Children = append(node.Children, build(w))
			}
		}
		return node
	}

	chains := make([]*lockChain, 0, len(roots))
	for _, r := range roots {
		chains = append(chains, build(r))
	}
	return chains
}

// renderLocks renders the lock monitor view.
func (m Model) renderLocks() string {
	if m.snapshot == nil || len(m.snapshot.Locks) == 0 {
		return renderEmptyState("No Active Locks", "All clear — no locks detected")
	}

	var b strings.Builder
	w := m.contentWidth()
	innerW := w - 7

	granted, waiting := 0, 0
	for _, l := range m.snapshot.Locks {
		if l.Granted {
			granted++
		} else {
			waiting++
		}
	}
	chains := buildLockChains(m.snapshot.Locks)

	summaryLine := fmt.Sprintf("Granted: %s  │  Waiting: %s  │  Blocking sessions: %s",
		styleGood.Render(fmt.Sprintf("%d", granted)),
		waitingStyle(waiting).Render(fmt.Sprintf("%d", waiting)),
		waitingStyle(len(chains)).Render(fmt.Sprintf("%d", len(chains))),
	)
	b.WriteString(dashBox(innerW).Render(summaryLine))
	b.WriteString("\n")

	if len(chains) > 0 {
		activity := map[int]model.Activity{}
		for _, a := range m.snapshot.Activities {
			activity[a.PID] = a
		}

		b.WriteString(dashSectionTitle("BLOCKING CHAINS") + "\n")
		header := fmt.Sprintf("  %-20s %-16s %-9s %-30s %s",
			"PID", "Application", "For", "Waiting On", "Query")
		b.WriteString(styleHeader.Render(header))
		b.WriteString("\n")
		b.WriteString(m.renderDivider())
		b.WriteString("\n")

		queryW := w - 82
		if queryW < 20 {
			queryW = 20
		}
		for _, c := range chains {
			renderLockChain(&b, c, activity, "", true, true, queryW)
		}
		b.WriteString("\n")
	}

	// All locks summary by type, in a stable order.
	locksByType := map[string]int{}
	for _, l := range m.snapshot.Locks {
		locksByType[l.LockType]++
	}
	types := make([]string, 0, len(locksByType))
	for lt := range locksByType {
		types = append(types, lt)
	}
	sort.Strings(types)

	b.WriteString(dashSectionTitle("LOCK SUMMARY BY TYPE") + "\n")
	lockRows := make([]string, 0, len(types))
	for _, lt := range types {
		lockRows = append(lockRows, kvRow(lt, fmt.Sprintf("%d", locksByType[lt]), innerW))
	}
	b.WriteString(dashBox(innerW).Render(joinWithDividers(lockRows, innerW)))
	b.WriteString("\n")

	return b.String()
}

// renderLockChain writes one tree node and recurses into its waiters.
func renderLockChain(b *strings.Builder, c *lockChain, activity map[int]model.Activity,
	prefix string, isLast, isRoot bool, queryW int) {
	a := activity[c.PID]
	muted := lipgloss.NewStyle().Foreground(colorMuted)

	var label string
	if isRoot {
		label = styleBad.Render("▲ ") + styleValue.Render(fmt.Sprintf("%d", c.PID))
	} else {
		branch := "├─ "
		if isLast {
			branch = "└─ "
		}
		label = muted.Render(prefix+branch) + styleWarn.Render(fmt.Sprintf("%d", c.PID))
	}

	app := a.ApplicationName
	if app == "" {
		app = a.UserName
	}

	var forDur time.Duration
	var onWhat string
	onStyle := lipgloss.NewStyle().Foreground(colorText)
	if c.Lock != nil {
		forDur = c.Lock.Duration
		target := c.Lock.RelationName
		if target == "" {
			target = c.Lock.LockType
		}
		onWhat = fmt.Sprintf("%s on %s", strings.TrimSuffix(c.Lock.Mode, "Lock"), target)
	} else {
		// Root blocker: show what it is doing rather than what it waits on.
		if !a.StateChange.IsZero() {
			forDur = time.Since(a.StateChange)
		}
		onWhat = "— holding · " + a.State
		onStyle = styleIdleInTx
	}

	fmt.Fprintf(b, "  %s %s %s %s %s\n",
		lipgloss.NewStyle().Width(20).Render(label),
		lipgloss.NewStyle().Width(16).Render(truncate(app, 15)),
		durStyle(forDur).Width(9).Render(formatShortDuration(forDur)),
		onStyle.Width(30).Render(truncate(onWhat, 29)),
		muted.Render(truncate(a.Query, queryW)),
	)

	childPrefix := prefix
	if !isRoot {
		if isLast {
			childPrefix += "   "
		} else {
			childPrefix += "│  "
		}
	}
	for i, child := range c.Children {
		renderLockChain(b, child, activity, childPrefix, i == len(c.Children)-1, false, queryW)
	}
}

func waitingStyle(n int) lipgloss.Style {
	if n > 0 {
		return styleBad
	}
	return styleGood
}
