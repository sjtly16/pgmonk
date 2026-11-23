package analyzer_test

import (
	"testing"
	"time"

	"github.com/sjtly16/pgmonk/internal/analyzer"
	"github.com/sjtly16/pgmonk/internal/model"
)

func filterByCategory(insights []analyzer.Insight, cat string) []analyzer.Insight {
	var out []analyzer.Insight
	for _, i := range insights {
		if i.Category == cat {
			out = append(out, i)
		}
	}
	return out
}

func TestAnalyze_NilSnapshot(t *testing.T) {
	if got := analyzer.Analyze(nil); got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestAnalyze_HealthyDatabase(t *testing.T) {
	snap := &model.Snapshot{
		Overview: model.Overview{CacheHitRatio: 99.95, TotalConnections: 10, MaxConnections: 200},
	}
	if insights := analyzer.Analyze(snap); len(insights) != 0 {
		t.Errorf("expected 0 insights, got %d: %v", len(insights), insights)
	}
}

func TestSeverity_String(t *testing.T) {
	tests := []struct {
		sev  analyzer.Severity
		want string
	}{
		{analyzer.Info, "INFO"}, {analyzer.Warning, "WARN"}, {analyzer.Critical, "CRIT"},
	}
	for _, tt := range tests {
		if got := tt.sev.String(); got != tt.want {
			t.Errorf("%d.String() = %q, want %q", tt.sev, got, tt.want)
		}
	}
	var unknown analyzer.Severity = 99
	if got := unknown.String(); got != "UNKNOWN" {
		t.Errorf("unknown severity = %q, want UNKNOWN", got)
	}
}

func TestCacheHitRatio(t *testing.T) {
	cases := []struct {
		name    string
		ratio   float64
		wantLen int
		wantSev analyzer.Severity
	}{
		{"zero", 0, 0, 0},
		{"excellent", 99.5, 0, 0},
		{"warning", 97.5, 1, analyzer.Warning},
		{"critical", 85.0, 1, analyzer.Critical},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			snap := &model.Snapshot{Overview: model.Overview{CacheHitRatio: tc.ratio}}
			ins := filterByCategory(analyzer.Analyze(snap), "cache")
			if len(ins) != tc.wantLen {
				t.Fatalf("got %d insights, want %d", len(ins), tc.wantLen)
			}
			if tc.wantLen > 0 && ins[0].Severity != tc.wantSev {
				t.Errorf("severity = %v, want %v", ins[0].Severity, tc.wantSev)
			}
		})
	}
}

func TestConnections(t *testing.T) {
	cases := []struct {
		name       string
		total, max int
		wantSev    analyzer.Severity
		wantEmpty  bool
	}{
		{"critical", 90, 100, analyzer.Critical, false},
		{"warning", 75, 100, analyzer.Warning, false},
		{"healthy", 50, 100, 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			snap := &model.Snapshot{Overview: model.Overview{
				TotalConnections: tc.total, MaxConnections: tc.max, CacheHitRatio: 99.9,
			}}
			ins := filterByCategory(analyzer.Analyze(snap), "connections")
			if tc.wantEmpty && len(ins) > 0 {
				t.Fatalf("expected empty, got %d", len(ins))
			}
			if !tc.wantEmpty {
				if len(ins) == 0 {
					t.Fatal("expected insights, got none")
				}
				if ins[0].Severity != tc.wantSev {
					t.Errorf("severity = %v, want %v", ins[0].Severity, tc.wantSev)
				}
			}
		})
	}
}

func TestDeadTuples(t *testing.T) {
	snap := &model.Snapshot{
		Overview: model.Overview{CacheHitRatio: 99.9},
		Tables: []model.TableStat{{
			SchemaName: "public", TableName: "orders",
			LiveTuples: 100000, DeadTuples: 30000,
		}},
	}
	ins := filterByCategory(analyzer.Analyze(snap), "vacuum")
	if len(ins) != 1 || ins[0].Severity != analyzer.Critical {
		t.Errorf("expected 1 critical vacuum insight, got %v", ins)
	}
}

func TestUnusedIndexes(t *testing.T) {
	snap := &model.Snapshot{
		Overview: model.Overview{CacheHitRatio: 99.9},
		Indexes: []model.IndexStat{{
			SchemaName: "public", TableName: "users", IndexName: "idx_legacy",
			IndexScan: 0, IndexSize: 5 * 1024 * 1024, IsUnique: false,
		}},
	}
	ins := filterByCategory(analyzer.Analyze(snap), "indexes")
	if len(ins) == 0 {
		t.Fatal("expected unused index insight")
	}
	// Unique indexes should not be flagged
	snap2 := &model.Snapshot{
		Overview: model.Overview{CacheHitRatio: 99.9},
		Indexes:  []model.IndexStat{{IndexScan: 0, IndexSize: 5 * 1024 * 1024, IsUnique: true}},
	}
	ins2 := filterByCategory(analyzer.Analyze(snap2), "indexes")
	if len(ins2) != 0 {
		t.Errorf("unique index should not be flagged, got %v", ins2)
	}
}

func TestXIDWraparound(t *testing.T) {
	cases := []struct {
		name      string
		age       int64
		wantSev   analyzer.Severity
		wantEmpty bool
	}{
		{"critical", 1_600_000_000, analyzer.Critical, false},
		{"warning", 600_000_000, analyzer.Warning, false},
		{"healthy", 100_000_000, 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			snap := &model.Snapshot{Overview: model.Overview{
				CacheHitRatio: 99.9, DataFrozenXIDAge: tc.age, OldestXactDB: "mydb",
			}}
			ins := filterByCategory(analyzer.Analyze(snap), "xid_wraparound")
			if tc.wantEmpty && len(ins) > 0 {
				t.Fatalf("expected empty, got %d", len(ins))
			}
			if !tc.wantEmpty && (len(ins) != 1 || ins[0].Severity != tc.wantSev) {
				t.Errorf("got %v, want 1 insight with severity %v", ins, tc.wantSev)
			}
		})
	}
}

func TestCheckpointFrequency(t *testing.T) {
	snap := &model.Snapshot{Overview: model.Overview{
		CacheHitRatio: 99.9, CheckpointsReq: 80, CheckpointsTimed: 20,
	}}
	if ins := filterByCategory(analyzer.Analyze(snap), "checkpoints"); len(ins) != 1 {
		t.Errorf("expected 1 checkpoint insight, got %d", len(ins))
	}
	snap2 := &model.Snapshot{Overview: model.Overview{
		CacheHitRatio: 99.9, CheckpointsReq: 10, CheckpointsTimed: 90,
	}}
	if ins := filterByCategory(analyzer.Analyze(snap2), "checkpoints"); len(ins) != 0 {
		t.Errorf("expected 0 checkpoint insights, got %d", len(ins))
	}
}

func TestArchiverFailures(t *testing.T) {
	recentFail := time.Now().Add(-5 * time.Minute)
	snap := &model.Snapshot{
		Overview: model.Overview{CacheHitRatio: 99.9},
		ArchiverStats: &model.ArchiverStats{
			FailedCount: 5, LastFailedWAL: "000000010000000100000042", LastFailedTime: &recentFail,
		},
	}
	ins := filterByCategory(analyzer.Analyze(snap), "archiver")
	if len(ins) != 1 || ins[0].Severity != analyzer.Critical {
		t.Errorf("expected 1 critical archiver insight, got %v", ins)
	}
}

func TestReplicationLag(t *testing.T) {
	critLag := 10 * time.Minute
	warnLag := 45 * time.Second
	snap1 := &model.Snapshot{
		Overview:    model.Overview{CacheHitRatio: 99.9},
		Replication: []model.ReplicationStat{{ApplicationName: "r1", ClientAddr: "10.0.0.2", ReplayLag: &critLag}},
	}
	ins := filterByCategory(analyzer.Analyze(snap1), "replication")
	if len(ins) != 1 || ins[0].Severity != analyzer.Critical {
		t.Errorf("expected critical, got %v", ins)
	}
	snap2 := &model.Snapshot{
		Overview:    model.Overview{CacheHitRatio: 99.9},
		Replication: []model.ReplicationStat{{ApplicationName: "r1", ClientAddr: "10.0.0.2", ReplayLag: &warnLag}},
	}
	ins2 := filterByCategory(analyzer.Analyze(snap2), "replication")
	if len(ins2) != 1 || ins2[0].Severity != analyzer.Warning {
		t.Errorf("expected warning, got %v", ins2)
	}
}

func TestRollbackRatio(t *testing.T) {
	snap := &model.Snapshot{Overview: model.Overview{CacheHitRatio: 99.9, RollbackRatio: 8.5}}
	if ins := filterByCategory(analyzer.Analyze(snap), "transactions"); len(ins) == 0 {
		t.Error("expected rollback ratio insight")
	}
}

func TestTempFileUsage(t *testing.T) {
	snap := &model.Snapshot{Overview: model.Overview{
		CacheHitRatio: 99.9, TempBytes: 2 * 1024 * 1024 * 1024, TempFiles: 50,
	}}
	if ins := filterByCategory(analyzer.Analyze(snap), "temp_files"); len(ins) != 1 {
		t.Errorf("expected 1 temp file insight, got %d", len(ins))
	}
}

func TestLongRunningTransactions(t *testing.T) {
	snap := &model.Snapshot{
		Overview: model.Overview{CacheHitRatio: 99.9},
		Activities: []model.Activity{{
			PID: 1234, UserName: "app", ClientAddr: "10.0.0.1",
			State: "idle in transaction", XactStart: time.Now().Add(-2 * time.Hour),
		}},
	}
	if ins := filterByCategory(analyzer.Analyze(snap), "transactions"); len(ins) == 0 {
		t.Error("expected long tx insight")
	}
}

func TestBGWriterPressure(t *testing.T) {
	snap := &model.Snapshot{
		Overview:      model.Overview{CacheHitRatio: 99.9},
		BGWriterStats: &model.BGWriterStats{MaxWrittenClean: 500},
	}
	if ins := filterByCategory(analyzer.Analyze(snap), "bgwriter"); len(ins) != 1 {
		t.Errorf("expected 1 bgwriter insight, got %d", len(ins))
	}
}

func TestWALBuffersFull(t *testing.T) {
	snap := &model.Snapshot{
		Overview: model.Overview{CacheHitRatio: 99.9},
		WALStats: &model.WALStats{WALBuffersFull: 5000},
	}
	if ins := filterByCategory(analyzer.Analyze(snap), "wal"); len(ins) == 0 {
		t.Error("expected WAL insight")
	}
}

func TestStandbyConflicts(t *testing.T) {
	snap := &model.Snapshot{
		Overview:      model.Overview{CacheHitRatio: 99.9},
		ConflictStats: &model.ConflictStats{ConflSnapshot: 80, ConflLock: 30, ConflBufferpin: 10},
	}
	if ins := filterByCategory(analyzer.Analyze(snap), "replication"); len(ins) == 0 {
		t.Error("expected standby conflict insight")
	}
}
