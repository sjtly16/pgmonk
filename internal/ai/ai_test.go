package ai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sjtly16/pgmonk/internal/model"
)

func testSnapshot() *model.Snapshot {
	now := time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC)
	xact := now.Add(-11 * time.Minute)
	cat := model.Catalog{}
	for _, c := range []string{"pid", "state", "query", "xact_start"} {
		cat.Add("pg_stat_activity", c)
	}
	for _, c := range []string{"relname", "n_dead_tup", "n_live_tup", "last_autovacuum"} {
		cat.Add("pg_stat_user_tables", c)
	}
	return &model.Snapshot{
		CollectedAt: now,
		ServerInfo: model.ServerInfo{
			Version: "PostgreSQL 17.9 (Debian 17.9-1) on aarch64", VersionNum: 170009,
			StartTime: now.Add(-3 * time.Hour), HasStatStatements: true, Catalog: cat,
		},
		Overview: model.Overview{
			DatabaseName: "shop", TotalConnections: 9, MaxConnections: 50, ActiveConnections: 6,
			IdleInTxConnections: 2, CacheHitRatio: 99.99, TxCommitRate: 418, DatabaseSize: 150 << 20,
		},
		Settings: []model.Setting{{Name: "work_mem", Value: "4096", Unit: "kB", Context: "user"}, {Name: "wal_buffers", Value: "512", Unit: "8kB", Context: "postmaster"}},
		Activities: []model.Activity{
			{PID: 9402, ApplicationName: "admin-console", State: "idle in transaction", XactStart: xact, BackendXID: "777",
				Query: "SELECT *\n  FROM customers WHERE id = 7 FOR UPDATE", BackendType: "client backend"},
			{PID: 9502, ApplicationName: "signup-api", State: "active", Duration: 11 * time.Minute,
				WaitEventType: "Lock", WaitEvent: "transactionid", Query: "UPDATE customers SET email = email WHERE id = 7", BackendType: "client backend"},
			{PID: 1, ApplicationName: "pg-monk", State: "active", Duration: time.Hour, BackendType: "client backend"},
		},
		Locks: []model.LockInfo{
			{PID: 9502, Granted: false, LockType: "transactionid", Mode: "ShareLock", BlockedByPIDs: []int{9402}, Duration: 11 * time.Minute},
		},
		Queries: []model.QueryStat{
			{Query: "SELECT country, count(*) FROM orders o JOIN customers c ON c.id = o.customer_id GROUP BY country",
				Calls: 13300, TotalExecTime: 354000, MeanExecTime: 27, Rows: 66000, CacheHitRatio: 100},
		},
		Tables: []model.TableStat{
			{SchemaName: "public", TableName: "events", LiveTuples: 163700, DeadTuples: 166700, TotalSize: 93 << 20},
		},
	}
}

func TestDigest(t *testing.T) {
	d := Digest(testSnapshot(), nil)
	for _, want := range []string{
		`Database "shop", server PostgreSQL 17.9`,
		"Connections: 9/50",
		"work_mem=4096kB, wal_buffers=5128kB*",
		`pid 9402 app=admin-console state="idle in transaction" for 11.0m: SELECT * FROM customers WHERE id = 7 FOR UPDATE`,
		"pid 9502 (signup-api) waiting 11.0m for ShareLock on transactionid, blocked by pid 9402 (admin-console, idle in transaction)",
		"public.events: 163.7K live, 166.7K dead",
		"None — all checks passed.",
		"## Vacuum horizon\nHeld back by pid 9402 (app=admin-console",
	} {
		if !strings.Contains(d, want) {
			t.Errorf("digest missing %q\n---\n%s", want, d)
		}
	}
	if strings.Contains(d, "app=pg-monk") {
		t.Error("digest should not include pg-monk's own sessions")
	}
	if n := len(d) / 4; n > 2000 {
		t.Errorf("digest ~%d tokens, want <= 2000", n)
	}
}

func TestSystemPromptStable(t *testing.T) {
	snap := testSnapshot()
	e1, err := NewEngine(&fakeProvider{}, snap.ServerInfo, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	e2, _ := NewEngine(&fakeProvider{}, snap.ServerInfo, nil, nil)
	if e1.system != e2.system {
		t.Fatal("system prompt must be byte-identical for the same server (KV-cache reuse)")
	}
	if !strings.Contains(e1.system, "## PostgreSQL 17 knowledge") {
		t.Error("system prompt missing version knowledge")
	}
}

func TestParseResponse(t *testing.T) {
	in := "Sure! Here it is:\n```json\n{\"summary\":\"s\",\"findings\":[{\"severity\":\"CRITICAL\",\"title\":\"t\",\"sql\":\"  SELECT 1 \"},{\"severity\":\"urgent\",\"title\":\"u\"}]}\n```"
	r, err := parseResponse(in)
	if err != nil {
		t.Fatal(err)
	}
	if r.Summary != "s" || len(r.Findings) != 2 {
		t.Fatalf("unexpected %+v", r)
	}
	if r.Findings[0].Severity != "critical" || r.Findings[0].SQL != "SELECT 1" || r.Findings[1].Severity != "info" {
		t.Errorf("normalisation failed: %+v", r.Findings)
	}
	if _, err := parseResponse("no json here"); err == nil {
		t.Error("expected error for non-JSON")
	}
}

func TestUnknownColumns(t *testing.T) {
	cat := testSnapshot().ServerInfo.Catalog
	got := UnknownColumns("check pg_stat_activity.state and pg_stat_activity.wait_start, pg_stat_wal.wal_write, pg_stat_activity.state", cat)
	want := []string{"pg_stat_activity.wait_start", "pg_stat_wal.wal_write"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("got %v want %v", got, want)
	}
}

// ---- providers ----

func TestOllamaRequestAndModelNotFound(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Errorf("path %s", r.URL.Path)
		}
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got)
		if got["model"] == "missing" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"model 'missing' not found"}`))
			return
		}
		_, _ = w.Write([]byte(`{"message":{"content":"{\"summary\":\"ok\",\"findings\":[]}"},"prompt_eval_count":2100,"prompt_eval_duration":230000000,"eval_count":40,"total_duration":900000000}`))
	}))
	defer srv.Close()

	o := &Ollama{BaseURL: srv.URL, Model: "qwen2.5:7b", NumCtx: 8192, KeepAlive: "30m"}
	text, usage, err := o.Complete(context.Background(), "SYS", "USER")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, `"summary":"ok"`) || usage.PromptTokens != 2100 || usage.PromptEval != 230*time.Millisecond {
		t.Errorf("text=%q usage=%+v", text, usage)
	}
	if got["format"] == nil || got["keep_alive"] != "30m" || got["stream"] != false {
		t.Errorf("request missing schema/keep_alive/stream: %v", got)
	}
	if opts := got["options"].(map[string]any); opts["num_ctx"] != float64(8192) || opts["temperature"] != float64(0) {
		t.Errorf("options = %v", opts)
	}
	msgs := got["messages"].([]any)
	if msgs[0].(map[string]any)["role"] != "system" || msgs[0].(map[string]any)["content"] != "SYS" {
		t.Error("system prompt must be the first message")
	}

	o.Model = "missing"
	_, _, err = o.Complete(context.Background(), "s", "u")
	if !errors.Is(err, ErrModelNotFound) || !strings.Contains(err.Error(), "ollama pull missing") {
		t.Errorf("want ErrModelNotFound with pull hint, got %v", err)
	}
}

func TestOpenAIRetriesWithoutResponseFormat(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer k" {
			t.Errorf("missing auth header")
		}
		var body map[string]any
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &body)
		if _, has := body["response_format"]; has {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"response_format not supported"}`))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"summary\":\"x\",\"findings\":[]}"}}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`))
	}))
	defer srv.Close()

	o := &OpenAI{BaseURL: srv.URL + "/v1", Model: "m", APIKey: "k"}
	text, usage, err := o.Complete(context.Background(), "s", "u")
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || usage.PromptTokens != 10 || !strings.Contains(text, `"summary":"x"`) {
		t.Errorf("calls=%d usage=%+v text=%q", calls, usage, text)
	}
}

func TestIsLocal(t *testing.T) {
	for u, want := range map[string]bool{
		"http://localhost:11434":      true,
		"http://127.0.0.1:1234/v1":    true,
		"http://[::1]:8080":           true,
		"https://api.example.com/v1":  false,
		"http://gpu-box.internal:800": false,
	} {
		if got := IsLocal(u); got != want {
			t.Errorf("IsLocal(%s) = %v", u, got)
		}
	}
}

// ---- engine ----

type fakeProvider struct {
	reply  string
	system string
	user   string
}

func (f *fakeProvider) Name() string { return "fake/model" }
func (f *fakeProvider) Complete(_ context.Context, system, user string) (string, Usage, error) {
	f.system, f.user = system, user
	return f.reply, Usage{PromptTokens: 1}, nil
}

var errNotRO = errors.New("not read only")

func TestEngineValidatesFindings(t *testing.T) {
	snap := testSnapshot()
	fp := &fakeProvider{reply: `{"summary":"Idle transaction blocks writers.","findings":[
		{"severity":"critical","title":"Idle tx holds lock","evidence":"pg_stat_activity.state idle 11m","root_cause":"r","action":"terminate","sql":"SELECT pid, state FROM pg_stat_activity WHERE state = 'idle in transaction'"},
		{"severity":"warning","title":"WAL","evidence":"pg_stat_wal.wal_write high","root_cause":"r","action":"a","sql":"SELECT wal_write FROM pg_stat_wal"},
		{"severity":"info","title":"Vacuum","evidence":"e","root_cause":"r","action":"VACUUM events","sql":"VACUUM events"}]}`}
	verify := func(_ context.Context, sql string) error {
		switch {
		case strings.HasPrefix(sql, "VACUUM"):
			return errNotRO
		case strings.Contains(sql, "wal_write"):
			return errors.New(`column "wal_write" does not exist`)
		}
		return nil
	}
	e, err := NewEngine(fp, snap.ServerInfo, verify, errNotRO)
	if err != nil {
		t.Fatal(err)
	}
	res, err := e.Analyze(context.Background(), snap)
	if err != nil {
		t.Fatal(err)
	}
	if res.Response == nil {
		t.Fatalf("parse failed: %v", res.ParseError)
	}
	f := res.Response.Findings
	if !f[0].SQLVerified || len(f[0].Unknown) != 0 {
		t.Errorf("finding 0 should verify cleanly: %+v", f[0])
	}
	if f[1].SQLVerified || !strings.Contains(f[1].SQLError, "does not exist") || len(f[1].Unknown) != 1 {
		t.Errorf("finding 1 should be flagged: %+v", f[1])
	}
	if f[2].SQLVerified || !f[2].SQLIsFix || f[2].SQLError != "" {
		t.Errorf("finding 2 should be marked unverified: %+v", f[2])
	}
	if !strings.Contains(fp.user, "## Rule-based findings") || fp.system != e.system {
		t.Error("engine did not send digest + stable system prompt")
	}
}

func TestRestartNotes(t *testing.T) {
	r := &Response{Findings: []Finding{
		{Action: "Increase wal_buffers", SQL: "ALTER SYSTEM SET wal_buffers = '16MB'; SELECT pg_reload_conf();"},
		{Action: "ALTER SYSTEM SET work_mem = '64MB'; SELECT pg_reload_conf();"},
		{Action: "Raise shared_buffers and restart the server"},
	}}
	addRestartNotes(r, []model.Setting{
		{Name: "wal_buffers", Context: "postmaster"}, {Name: "shared_buffers", Context: "postmaster"}, {Name: "work_mem", Context: "user"},
	})
	if len(r.Findings[0].Notes) != 1 || !strings.Contains(r.Findings[0].Notes[0], "wal_buffers only takes effect after a server restart") {
		t.Errorf("wal_buffers + reload should get a note: %v", r.Findings[0].Notes)
	}
	if len(r.Findings[1].Notes) != 0 || len(r.Findings[2].Notes) != 0 {
		t.Errorf("no note expected for reloadable setting or for advice that already says restart")
	}
}

func TestEngineKeepsRawOnBadJSON(t *testing.T) {
	snap := testSnapshot()
	e, _ := NewEngine(&fakeProvider{reply: "I think the database is fine."}, snap.ServerInfo, nil, nil)
	res, err := e.Analyze(context.Background(), snap)
	if err != nil {
		t.Fatal(err)
	}
	if res.Response != nil || res.ParseError == nil || res.Raw != "I think the database is fine." {
		t.Errorf("want raw fallback, got %+v", res)
	}
}
