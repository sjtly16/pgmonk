package tui

import (
	"testing"

	"github.com/sjtly16/pgmonk/internal/model"
)

func TestBuildLockChains(t *testing.T) {
	locks := []model.LockInfo{
		{PID: 10, Granted: true, LockType: "relation"},
		{PID: 20, Granted: false, LockType: "transactionid", BlockedByPIDs: []int{10}},
		{PID: 30, Granted: false, LockType: "tuple", BlockedByPIDs: []int{20}},
		{PID: 40, Granted: false, LockType: "tuple", BlockedByPIDs: []int{10}},
		{PID: 50, Granted: true, LockType: "relation"},
	}
	chains := buildLockChains(locks)
	if len(chains) != 1 || chains[0].PID != 10 {
		t.Fatalf("expected single root 10, got %+v", chains)
	}
	root := chains[0]
	if root.Lock != nil {
		t.Errorf("root blocker should have no waiting lock")
	}
	if len(root.Children) != 2 || root.Children[0].PID != 20 || root.Children[1].PID != 40 {
		t.Fatalf("unexpected children: %+v", root.Children)
	}
	if len(root.Children[0].Children) != 1 || root.Children[0].Children[0].PID != 30 {
		t.Fatalf("expected 30 nested under 20")
	}
}

func TestBuildLockChainsCycle(t *testing.T) {
	locks := []model.LockInfo{
		{PID: 1, Granted: false, BlockedByPIDs: []int{2}},
		{PID: 2, Granted: false, BlockedByPIDs: []int{1}},
	}
	chains := buildLockChains(locks)
	if len(chains) != 1 || chains[0].PID != 1 || len(chains[0].Children) != 1 {
		t.Fatalf("cycle should render once from lowest PID, got %+v", chains)
	}
}

func TestTruncateCollapsesWhitespaceAndRunes(t *testing.T) {
	if got := truncate("SELECT\n    a,\n\tb FROM t", 50); got != "SELECT a, b FROM t" {
		t.Errorf("whitespace not collapsed: %q", got)
	}
	if got := truncate("héllo wörld", 8); got != "héllo..." {
		t.Errorf("rune truncate wrong: %q", got)
	}
}
