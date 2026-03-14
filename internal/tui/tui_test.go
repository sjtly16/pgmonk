package tui_test

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/shashank/pgmonk/internal/model"
	"github.com/shashank/pgmonk/internal/tui"
)

func testServerInfo() model.ServerInfo {
	return model.ServerInfo{
		Version: "PostgreSQL 16.2", VersionNum: 160002,
		HasStatStatements: true, HasStatWAL: true,
	}
}

func testSnapshot() *model.Snapshot {
	return &model.Snapshot{
		CollectedAt: time.Now(),
		Overview: model.Overview{
			DatabaseName: "testdb", ActiveConnections: 5, IdleConnections: 10,
			TotalConnections: 15, MaxConnections: 100, CacheHitRatio: 99.5,
			TxCommitRate: 150, TxRollbackRate: 2,
			DatabaseSize: 5 * 1024 * 1024 * 1024, Deadlocks: 0,
		},
		Activities: []model.Activity{
			{PID: 100, State: "active", UserName: "app", Query: "SELECT 1", Duration: time.Second},
			{PID: 101, State: "idle", UserName: "app"},
		},
	}
}

func TestNewModel(t *testing.T) {
	m := tui.NewModel(testServerInfo())
	view := m.View()
	if !strings.Contains(view, "Connecting") {
		t.Error("expected loading message before first snapshot")
	}
}

func TestModel_SnapshotUpdate(t *testing.T) {
	m := tui.NewModel(testServerInfo())
	msg := tui.SnapshotMsg{Snapshot: testSnapshot()}
	updated, _ := m.Update(msg)
	view := updated.View()
	if !strings.Contains(view, "testdb") {
		t.Error("expected database name in view after snapshot")
	}
}

func TestModel_TabNavigation(t *testing.T) {
	m := tui.NewModel(testServerInfo())
	m.Update(tui.SnapshotMsg{Snapshot: testSnapshot()})

	// Navigate via number keys
	for _, key := range []string{"1", "2", "3", "4", "5", "6", "7"} {
		updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
		view := updated.View()
		if view == "" {
			t.Errorf("empty view for key %s", key)
		}
	}
}

func TestModel_HelpModal(t *testing.T) {
	m := tui.NewModel(testServerInfo())
	// Must send snapshot first so model is ready.
	m2, _ := m.Update(tui.SnapshotMsg{Snapshot: testSnapshot()})
	// Then trigger help.
	updated, _ := m2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	view := updated.View()
	if !strings.Contains(view, "Keyboard Shortcuts") {
		t.Error("expected help modal to show")
	}
	// Dismiss with Esc
	updated2, _ := updated.Update(tea.KeyMsg{Type: tea.KeyEsc})
	view2 := updated2.View()
	if strings.Contains(view2, "Keyboard Shortcuts") {
		t.Error("expected help modal to be dismissed")
	}
}

func TestModel_WindowResize(t *testing.T) {
	m := tui.NewModel(testServerInfo())
	m.Update(tui.SnapshotMsg{Snapshot: testSnapshot()})
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	if updated.View() == "" {
		t.Error("view should not be empty after resize")
	}
}

func TestModel_Quit(t *testing.T) {
	m := tui.NewModel(testServerInfo())
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if cmd == nil {
		t.Error("expected quit command")
	}
}
