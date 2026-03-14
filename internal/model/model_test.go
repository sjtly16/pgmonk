package model_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/shashank/pgmonk/internal/model"
)

func TestSnapshot_ZeroValue(t *testing.T) {
	snap := model.Snapshot{}
	if !snap.CollectedAt.IsZero() {
		t.Error("expected zero CollectedAt")
	}
	if snap.Activities != nil {
		t.Error("expected nil Activities")
	}
}

func TestSnapshot_JSON(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	fiveMin := 5 * time.Minute
	snap := model.Snapshot{
		CollectedAt: now,
		ServerInfo:  model.ServerInfo{Version: "PostgreSQL 16.2", VersionNum: 160002},
		Overview:    model.Overview{DatabaseName: "test", CacheHitRatio: 99.5, TotalConnections: 10, MaxConnections: 100},
		Replication: []model.ReplicationStat{{
			ApplicationName: "replica1", State: "streaming", ReplayLag: &fiveMin,
		}},
	}
	data, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded model.Snapshot
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.Overview.DatabaseName != "test" {
		t.Errorf("expected test, got %s", decoded.Overview.DatabaseName)
	}
	if decoded.ServerInfo.VersionNum != 160002 {
		t.Errorf("expected 160002, got %d", decoded.ServerInfo.VersionNum)
	}
}

func TestOverview_Fields(t *testing.T) {
	o := model.Overview{
		ActiveConnections: 5, IdleConnections: 10,
		TxCommitRate: 100, TxRollbackRate: 2,
		CacheHitRatio: 99.9, DatabaseSize: 1024 * 1024 * 1024,
	}
	if o.CacheHitRatio != 99.9 {
		t.Errorf("cache hit = %f, want 99.9", o.CacheHitRatio)
	}
}

func TestActivity_Duration(t *testing.T) {
	a := model.Activity{PID: 123, State: "active", Duration: 5 * time.Second}
	if a.Duration != 5*time.Second {
		t.Errorf("duration = %v, want 5s", a.Duration)
	}
}

func TestQueryStat_CacheRatio(t *testing.T) {
	q := model.QueryStat{SharedBlksHit: 990, SharedBlksRead: 10}
	ratio := float64(q.SharedBlksHit) / float64(q.SharedBlksHit+q.SharedBlksRead) * 100
	if ratio != 99.0 {
		t.Errorf("ratio = %f, want 99.0", ratio)
	}
}

func TestLockInfo_Duration(t *testing.T) {
	now := time.Now()
	ws := now.Add(-30 * time.Second)
	li := model.LockInfo{PID: 1, WaitStart: &ws, Granted: false}
	li.Duration = time.Since(*li.WaitStart)
	if li.Duration < 29*time.Second {
		t.Errorf("expected ~30s duration, got %v", li.Duration)
	}
}
