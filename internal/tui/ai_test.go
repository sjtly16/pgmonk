package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/sjtly16/pgmonk/internal/ai"
	"github.com/sjtly16/pgmonk/internal/model"
)

type stubProvider struct{}

func (stubProvider) Name() string { return "ollama/test-model" }
func (stubProvider) Complete(context.Context, string, string) (string, ai.Usage, error) {
	return `{"summary":"ok","findings":[]}`, ai.Usage{}, nil
}

func aiTestModel(t *testing.T) Model {
	t.Helper()
	info := model.ServerInfo{VersionNum: 170009}
	engine, err := ai.NewEngine(stubProvider{}, info, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	m := NewModel(info, WithAI(engine, time.Minute))
	m.width, m.height, m.ready = 140, 60, true
	m.snapshot = &model.Snapshot{ServerInfo: info, CollectedAt: time.Now()}
	m.currentView = ViewInsights
	return m
}

func TestAISectionStates(t *testing.T) {
	// Disabled: explains how to turn it on.
	off := NewModel(model.ServerInfo{})
	off.width, off.height = 140, 60
	if s := off.renderAISection(120); !strings.Contains(s, "ollama pull") || !strings.Contains(s, "--ai") {
		t.Errorf("disabled state should explain setup:\n%s", s)
	}

	m := aiTestModel(t)
	m.aiLoading, m.aiStarted, m.aiNow = true, time.Now().Add(-7*time.Second), time.Now()
	if s := m.renderAISection(120); !strings.Contains(s, "Analyzing… 7s") || !strings.Contains(s, "PG 17 knowledge") {
		t.Errorf("loading state:\n%s", s)
	}

	m.aiLoading = false
	m.aiResult = &ai.Result{At: time.Now(), Elapsed: 12 * time.Second, Response: &ai.Response{
		Summary: "An idle transaction blocks writers and vacuum.",
		Findings: []ai.Finding{
			{Severity: "critical", Title: "Idle transaction blocks vacuum", Evidence: "pid 42 idle 16m",
				SQL: "SELECT pid FROM pg_stat_activity", SQLVerified: true},
			{Severity: "warning", Title: "WAL buffers", Action: "ALTER SYSTEM SET wal_buffers = '16MB'; SELECT pg_reload_conf();",
				Notes: []string{"wal_buffers only takes effect after a server restart; pg_reload_conf() will not apply it."}},
			{Severity: "info", Title: "Index", SQL: "CREATE INDEX CONCURRENTLY i ON t (c)", SQLIsFix: true},
			{Severity: "info", Title: "Bad check", SQL: "SELECT x FROM y", SQLError: `relation "y" does not exist`,
				Unknown: []string{"pg_stat_wal.wal_write"}},
		},
	}}
	s := m.renderAISection(120)
	for _, want := range []string{
		"An idle transaction blocks writers and vacuum.",
		"✓ verified on this server",
		"ℹ pg-monk: wal_buffers only takes effect after a server restart",
		"review before running",
		`⚠ relation "y" does not exist`,
		"don't exist on this server: pg_stat_wal.wal_write",
		"r to refresh",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("result state missing %q", want)
		}
	}
}

func TestInsightsTabTriggersAIOnce(t *testing.T) {
	m := aiTestModel(t)
	m.currentView = ViewDashboard

	next, cmd := m.Update(keyMsg("7"))
	m = next.(Model)
	if cmd == nil || !m.aiLoading {
		t.Fatal("entering Insights should start an analysis")
	}

	// A fresh result means re-entering the tab doesn't re-run it.
	m.aiLoading = false
	m.aiResult = &ai.Result{At: time.Now()}
	m.currentView = ViewDashboard
	next, cmd = m.Update(keyMsg("7"))
	m = next.(Model)
	if cmd != nil || m.aiLoading {
		t.Error("fresh result should not trigger another analysis")
	}

	// 'r' always re-runs.
	next, cmd = m.Update(keyMsg("r"))
	if cmd == nil || !next.(Model).aiLoading {
		t.Error("'r' should start an analysis")
	}
}

func keyMsg(k string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)} }

func TestAIFieldHangingIndent(t *testing.T) {
	m := aiTestModel(t)
	m.aiResult = &ai.Result{At: time.Now(), Response: &ai.Response{Summary: "s", Findings: []ai.Finding{
		{Severity: "warning", Title: "t", Action: strings.Repeat("word ", 40)},
	}}}
	out := m.renderAISection(60)
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "word") && !strings.Contains(line, "Action") && !strings.Contains(line, "          word") {
			t.Errorf("continuation line not indented under the text: %q", line)
		}
	}
}
