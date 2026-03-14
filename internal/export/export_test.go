package export_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/shashank/pgmonk/internal/export"
	"github.com/shashank/pgmonk/internal/model"
)

func testSnapshot() *model.Snapshot {
	return &model.Snapshot{
		CollectedAt: time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
		Overview: model.Overview{
			DatabaseName: "testdb", ActiveConnections: 5, TotalConnections: 20, MaxConnections: 100,
			CacheHitRatio: 99.5, TxCommitRate: 150, TxRollbackRate: 2,
			Deadlocks: 0, TempFiles: 3, TempBytes: 1024 * 1024, DatabaseSize: 5 * 1024 * 1024 * 1024,
		},
		Queries: []model.QueryStat{
			{QueryID: 1, Query: "SELECT * FROM users", Calls: 1000, TotalExecTime: 5000, MeanExecTime: 5, Rows: 50000, CacheHitRatio: 99.8},
			{QueryID: 2, Query: "UPDATE orders SET status=$1", Calls: 500, TotalExecTime: 3000, MeanExecTime: 6, Rows: 500, CacheHitRatio: 98.5},
		},
	}
}

func TestWriteJSON(t *testing.T) {
	var buf bytes.Buffer
	if err := export.WriteJSON(&buf, testSnapshot()); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}
	var decoded model.Snapshot
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Overview.DatabaseName != "testdb" {
		t.Errorf("got %q, want testdb", decoded.Overview.DatabaseName)
	}
	if len(decoded.Queries) != 2 {
		t.Errorf("got %d queries, want 2", len(decoded.Queries))
	}
}

func TestWriteCSVOverview(t *testing.T) {
	var buf bytes.Buffer
	if err := export.WriteCSVOverview(&buf, testSnapshot()); err != nil {
		t.Fatalf("WriteCSVOverview: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines (header+data), got %d", len(lines))
	}
	if !strings.Contains(lines[0], "database") {
		t.Error("header missing 'database'")
	}
	if !strings.Contains(lines[1], "testdb") {
		t.Error("data missing 'testdb'")
	}
}

func TestWriteCSVQueries(t *testing.T) {
	var buf bytes.Buffer
	if err := export.WriteCSVQueries(&buf, testSnapshot()); err != nil {
		t.Fatalf("WriteCSVQueries: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines (header+2 queries), got %d", len(lines))
	}
}
