//go:build integration

// Integration tests run the real collector against a live PostgreSQL server.
//
//	PGMONK_TEST_DSN=postgresql://postgres:demo@localhost:5432/postgres?sslmode=disable \
//	  go test -tags integration ./internal/collector/
//
// CI runs these against every supported major version.
package collector_test

import (
	"context"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/sjtly16/pgmonk/internal/analyzer"
	"github.com/sjtly16/pgmonk/internal/collector"
)

func testDSN(t *testing.T) string {
	dsn := os.Getenv("PGMONK_TEST_DSN")
	if dsn == "" {
		t.Skip("PGMONK_TEST_DSN not set")
	}
	return dsn
}

func TestCollectAgainstLiveServer(t *testing.T) {
	dsn := testDSN(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	setup, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = setup.Close(context.Background()) }()

	// Best effort: only works if pg_stat_statements is preloaded.
	_, _ = setup.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS pg_stat_statements")
	if _, err := setup.Exec(ctx, `
		DROP TABLE IF EXISTS pgmonk_it;
		CREATE TABLE pgmonk_it (id int PRIMARY KEY, v int);
		INSERT INTO pgmonk_it VALUES (1, 0);`); err != nil {
		t.Fatalf("setup: %v", err)
	}
	t.Cleanup(func() { _, _ = setup.Exec(context.Background(), "DROP TABLE IF EXISTS pgmonk_it") })
	// Make the setup session's table I/O visible now rather than whenever the
	// backend next flushes (PG 15+ flushes lazily; older versions use the
	// stats collector, which the sleeps below cover).
	_, _ = setup.Exec(ctx, "SELECT count(*) FROM pgmonk_it")
	_, _ = setup.Exec(ctx, "SELECT pg_stat_force_next_flush()")
	time.Sleep(600 * time.Millisecond) // PG <= 14 sends stats at most every 500ms
	_, _ = setup.Exec(ctx, "SELECT 1")

	// Holder: takes a row lock and sits idle in transaction.
	holder, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("holder connect: %v", err)
	}
	defer func() { _ = holder.Close(context.Background()) }()
	tx, err := holder.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	if _, err := tx.Exec(ctx, "SELECT * FROM pgmonk_it WHERE id = 1 FOR UPDATE"); err != nil {
		t.Fatalf("lock row: %v", err)
	}
	var holderPID int
	if err := tx.QueryRow(ctx, "SELECT pg_backend_pid()").Scan(&holderPID); err != nil {
		t.Fatalf("holder pid: %v", err)
	}

	// Waiter: blocks on the same row until the test ends.
	waiter, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("waiter connect: %v", err)
	}
	defer func() { _ = waiter.Close(context.Background()) }()
	waitCtx, stopWaiting := context.WithCancel(ctx)
	defer stopWaiting()
	go func() { _, _ = waiter.Exec(waitCtx, "UPDATE pgmonk_it SET v = v + 1 WHERE id = 1") }()

	coll, err := collector.New(ctx, dsn, time.Second)
	if err != nil {
		t.Fatalf("collector.New: %v", err)
	}
	defer coll.Close()

	info := coll.ServerInfo()
	t.Logf("server %d, pg_stat_statements=%v", info.VersionNum, info.HasStatStatements)

	// Give the waiter a moment to actually start waiting, then collect twice
	// so rate-based metrics are populated.
	time.Sleep(time.Second)
	for i := 0; i < 2; i++ {
		if err := coll.CollectOnce(ctx); err != nil {
			t.Fatalf("CollectOnce #%d: %v", i+1, err)
		}
		time.Sleep(500 * time.Millisecond)
	}

	snap := coll.Snapshot()
	if snap == nil {
		t.Fatal("nil snapshot")
	}
	if snap.Overview.MaxConnections == 0 {
		t.Error("overview not populated")
	}
	if len(snap.Activities) == 0 {
		t.Error("no activities collected")
	}
	if len(snap.Tables) == 0 {
		t.Error("no tables collected")
	}
	// Optional collectors swallow errors, so assert each one actually ran on
	// the versions that support it — this is where column drift hides.
	if info.HasStatWAL && snap.WALStats == nil {
		t.Error("pg_stat_wal available but WAL stats missing (column drift?)")
	}
	if info.HasStatCheckpointer && snap.CheckpointerStats == nil {
		t.Error("pg_stat_checkpointer available but checkpointer stats missing (column drift?)")
	}
	if info.HasStatIO && len(snap.IOStats) == 0 {
		t.Error("pg_stat_io available but I/O stats missing (column drift?)")
	}
	if snap.BGWriterStats == nil {
		t.Error("bgwriter stats missing")
	}
	if snap.ArchiverStats == nil {
		t.Error("archiver stats missing")
	}
	if len(snap.TableIOStats) == 0 {
		t.Error("table I/O stats missing")
	}
	if info.HasStatStatements && len(snap.Queries) == 0 {
		t.Error("pg_stat_statements present but no queries collected")
	}
	for _, q := range snap.Queries {
		if len(q.Query) >= 13 && q.Query[:13] == "/* pg-monk */" {
			t.Errorf("pg-monk's own query leaked into Top Queries: %q", q.Query)
		}
	}

	// The waiter must be reported as blocked by the holder.
	found := false
	for _, l := range snap.Locks {
		if !l.Granted && slices.Contains(l.BlockedByPIDs, holderPID) {
			found = true
			if l.Duration <= 0 {
				t.Errorf("waiting lock has no duration: %+v", l)
			}
		}
	}
	if !found {
		t.Errorf("no waiting lock blocked by holder pid %d; locks=%+v", holderPID, snap.Locks)
	}

	// The holder is idle in transaction and must have a sane timestamp.
	for _, a := range snap.Activities {
		if a.PID == holderPID {
			if a.State != "idle in transaction" {
				t.Errorf("holder state = %q, want idle in transaction", a.State)
			}
			if a.XactStart.IsZero() || time.Since(a.XactStart) > time.Hour {
				t.Errorf("holder xact_start looks wrong: %v", a.XactStart)
			}
		}
		if a.Duration > 24*time.Hour {
			t.Errorf("pid %d has implausible duration %v (NULL timestamp?)", a.PID, a.Duration)
		}
	}

	// The analyzer must run cleanly on real data.
	_ = analyzer.Analyze(snap)
}
