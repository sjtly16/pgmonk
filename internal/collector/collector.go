package collector

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sjtly16/pgmonk/internal/model"
)

// Collector gathers PostgreSQL metrics at regular intervals.
type Collector struct {
	pool       *pgxpool.Pool
	serverInfo model.ServerInfo
	interval   time.Duration

	mu       sync.RWMutex
	snapshot *model.Snapshot

	// For computing per-second rates we keep the previous absolute counters.
	prevStats *dbAbsoluteStats
	prevTime  time.Time
}

// dbAbsoluteStats holds the raw cumulative counters from pg_stat_database.
type dbAbsoluteStats struct {
	XactCommit   int64
	XactRollback int64
	TupInserted  int64
	TupUpdated   int64
	TupDeleted   int64
	TupFetched   int64
	BlksHit      int64
	BlksRead     int64
}

// SnapshotResult is exported for test files (used previously).
type SnapshotResult = model.Snapshot

// New creates a new Collector connected to the given DSN.
func New(ctx context.Context, dsn string, interval time.Duration) (*Collector, error) {
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("invalid connection string: %w", err)
	}

	// Keep the pool small — we're a monitoring tool, not an application.
	config.MaxConns = 3
	config.MinConns = 1

	// Identify ourselves in pg_stat_activity unless the DSN already does.
	if _, ok := config.ConnConfig.RuntimeParams["application_name"]; !ok {
		config.ConnConfig.RuntimeParams["application_name"] = "pg-monk"
	}

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("unable to connect to database: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("unable to ping database: %w", err)
	}

	c := &Collector{
		pool:     pool,
		interval: interval,
	}

	info, err := c.collectServerInfo(ctx)
	if err != nil {
		pool.Close()
		return nil, fmt.Errorf("unable to collect server info: %w", err)
	}
	c.serverInfo = info

	return c, nil
}

// selfTag prefixes every query pg-monk runs so its own monitoring traffic can
// be excluded from pg_stat_statements results (which keep leading comments).
const selfTag = "/* pg-monk */ "

func (c *Collector) query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return c.pool.Query(ctx, selfTag+sql, args...)
}

func (c *Collector) queryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return c.pool.QueryRow(ctx, selfTag+sql, args...)
}

// leaderPIDColumn returns a select expression for pg_stat_activity.leader_pid,
// which only exists from PostgreSQL 13.
func (c *Collector) leaderPIDColumn() string {
	return c.colOr("pg_stat_activity", "leader_pid", 130000, "COALESCE(leader_pid, 0)", "0")
}

// ServerInfo returns the detected server metadata.
func (c *Collector) ServerInfo() model.ServerInfo {
	return c.serverInfo
}

// Snapshot returns the latest collected snapshot (thread-safe).
func (c *Collector) Snapshot() *model.Snapshot {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.snapshot
}

// CollectOnce runs a single collection cycle and stores the result.
func (c *Collector) CollectOnce(ctx context.Context) error {
	snap, err := c.collect(ctx)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.snapshot = snap
	c.mu.Unlock()
	return nil
}

// Run starts the collector loop, sending snapshots on the channel.
func (c *Collector) Run(ctx context.Context, snapCh chan<- *model.Snapshot) error {
	if err := c.collectAndSend(ctx, snapCh); err != nil {
		return err
	}

	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := c.collectAndSend(ctx, snapCh); err != nil {
				fmt.Printf("collection error: %v\n", err)
			}
		}
	}
}

func (c *Collector) collectAndSend(ctx context.Context, ch chan<- *model.Snapshot) error {
	snap, err := c.collect(ctx)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.snapshot = snap
	c.mu.Unlock()

	select {
	case ch <- snap:
	default:
	}
	return nil
}

// collect performs the actual data gathering across all sources.
func (c *Collector) collect(ctx context.Context) (*model.Snapshot, error) {
	snap := &model.Snapshot{
		CollectedAt: time.Now(),
		ServerInfo:  c.serverInfo,
	}

	// --- Core metrics (always collected) ---

	// Overview / database-level stats.
	overview, absStats, err := c.collectOverview(ctx)
	if err != nil {
		return nil, fmt.Errorf("overview: %w", err)
	}
	snap.Overview = overview

	// Compute per-second rates from deltas.
	if c.prevStats != nil {
		elapsed := time.Since(c.prevTime).Seconds()
		if elapsed > 0 {
			snap.Overview.TxCommitRate = float64(absStats.XactCommit-c.prevStats.XactCommit) / elapsed
			snap.Overview.TxRollbackRate = float64(absStats.XactRollback-c.prevStats.XactRollback) / elapsed
			snap.Overview.TupInsertRate = float64(absStats.TupInserted-c.prevStats.TupInserted) / elapsed
			snap.Overview.TupUpdateRate = float64(absStats.TupUpdated-c.prevStats.TupUpdated) / elapsed
			snap.Overview.TupDeleteRate = float64(absStats.TupDeleted-c.prevStats.TupDeleted) / elapsed
			snap.Overview.TupFetchRate = float64(absStats.TupFetched-c.prevStats.TupFetched) / elapsed
		}
	}
	c.prevStats = absStats
	c.prevTime = time.Now()

	// Active sessions.
	activities, err := c.collectActivities(ctx)
	if err != nil {
		return nil, fmt.Errorf("activities: %w", err)
	}
	snap.Activities = activities

	// Top queries (if pg_stat_statements is available).
	if c.serverInfo.HasStatStatements {
		queries, err := c.collectQueries(ctx)
		if err != nil {
			snap.Queries = nil
		} else {
			snap.Queries = queries
		}
	}

	// Table stats.
	tables, err := c.collectTables(ctx)
	if err != nil {
		return nil, fmt.Errorf("tables: %w", err)
	}
	snap.Tables = tables

	// Index stats.
	indexes, err := c.collectIndexes(ctx)
	if err != nil {
		return nil, fmt.Errorf("indexes: %w", err)
	}
	snap.Indexes = indexes

	// Locks.
	locks, err := c.collectLocks(ctx)
	if err != nil {
		return nil, fmt.Errorf("locks: %w", err)
	}
	snap.Locks = locks

	// Replication info.
	repl, err := c.collectReplication(ctx)
	if err != nil {
		snap.Replication = nil
	} else {
		snap.Replication = repl
	}

	// Per-client / per-thread metrics.
	clients, err := c.collectClients(ctx)
	if err != nil {
		snap.Clients = nil
	} else {
		snap.Clients = clients
	}

	// --- New: additional monitoring data ---

	// BGWriter stats (always available).
	bgw, err := c.collectBGWriterStats(ctx)
	if err == nil {
		snap.BGWriterStats = bgw
	}

	// Archiver stats (always available).
	arch, err := c.collectArchiverStats(ctx)
	if err == nil {
		snap.ArchiverStats = arch
	}

	// Table I/O stats from pg_statio_user_tables (always available).
	tio, err := c.collectTableIOStats(ctx)
	if err == nil {
		snap.TableIOStats = tio
	}

	// Conflict stats from pg_stat_database_conflicts (always available, mainly useful on standbys).
	if c.serverInfo.IsStandby {
		cs, err := c.collectConflictStats(ctx)
		if err == nil {
			snap.ConflictStats = cs
		}
	}

	// Vacuum progress (PG 9.6+, always try).
	vp, err := c.collectVacuumProgress(ctx)
	if err == nil {
		snap.VacuumProgress = vp
	}

	// WAL stats (PG 14+).
	if c.serverInfo.HasStatWAL {
		ws, err := c.collectWALStats(ctx)
		if err == nil {
			snap.WALStats = ws
		}
	}

	// Checkpointer stats (PG 17+).
	if c.serverInfo.HasStatCheckpointer {
		cps, err := c.collectCheckpointerStats(ctx)
		if err == nil {
			snap.CheckpointerStats = cps
		}
	}

	// I/O stats (PG 16+).
	if c.serverInfo.HasStatIO {
		ios, err := c.collectIOStats(ctx)
		if err == nil {
			snap.IOStats = ios
		}
	}

	return snap, nil
}

// Close shuts down the collector's database connection pool.
func (c *Collector) Close() {
	c.pool.Close()
}
