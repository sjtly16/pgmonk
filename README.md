# pg-monk

**Real-time PostgreSQL performance dashboard for your terminal.**

Think `htop`, but for Postgres. Zero dependencies, single binary, instant insights.

![License](https://img.shields.io/badge/license-MIT-blue.svg)
![Go](https://img.shields.io/badge/go-1.22+-00ADD8.svg)

---

## Features

- **7-view TUI dashboard** — live-updating terminal UI with connection gauges, throughput, WAL, replication, and more
- **27 analyzer rules** — real-time insights with configurable thresholds for cache, connections, XID wraparound, replication lag, vacuum health, and more
- **Active session monitor** — color-coded view of all database sessions with wait events
- **Top queries analysis** — slowest queries ranked by total/mean time, calls, cache efficiency, WAL generation, and JIT stats (requires `pg_stat_statements`)
- **Table health** — dead tuples, vacuum status, sequential scan warnings, bloat detection, stale statistics
- **Lock monitor** — active locks with blocking chain visualization and lock duration
- **Per-client drill-down** — aggregated connection metrics per client IP, with per-backend thread detail
- **20+ PostgreSQL data sources** — pg_stat_wal, pg_stat_io, pg_stat_archiver, pg_stat_checkpointer, pg_stat_replication, and more
- **Version-aware collection** — automatically adapts queries for PG 12–17+, gating PG 14+/16+/17+ features
- **Snapshot export** — JSON/CSV one-shot reports for automation
- **Health diagnostics** — CLI `diagnose` command with severity-ranked insights
- **Configurable thresholds** — customize analyzer sensitivity via `~/.pgmonk.json`
- **Zero extensions required** — works out of the box; unlocks more with `pg_stat_statements`
- **psql-compatible flags** — `-H`, `-p`, `-U`, `-d` just work

---

## Quick Start

### Install

```bash
# From source
go install github.com/sjtly16/pg-monk/cmd/pgmonk@latest

# Or clone and build
git clone https://github.com/sjtly16/pg-monk.git
cd pg-monk && make build
```

### Connect

```bash
# Connection string
pg-monk "postgresql://monitor:pass@localhost:5432/mydb"

# psql-style flags
pg-monk -H localhost -p 5432 -U monitor -d mydb

# Environment variable
export PGMONK_DSN="postgresql://monitor:pass@localhost:5432/mydb"
pg-monk
```

### Navigate

| Key | Action |
|-----|--------|
| `1` – `7` | Switch to Dashboard / Sessions / Queries / Tables / Locks / Clients / Insights |
| `Tab` / `Shift+Tab` | Cycle through views |
| `?` | Show keyboard shortcuts help |
| `s` | Change sort order (Queries / Clients view) |
| `Enter` | Expand client to see per-thread details |
| `Esc` / `Backspace` | Collapse back to summary |
| `j` / `k` or `↑` / `↓` | Navigate in expanded views |
| `q` / `Ctrl+C` | Quit |

---

## Database Setup & Configuration

pg-monk is a read-only monitoring tool. It needs a PostgreSQL user with monitoring privileges and optionally some server-side configuration to unlock all features.

### 1. Create a Monitoring User

```sql
-- Create a dedicated monitoring user
CREATE USER pgmonk_monitor WITH PASSWORD 'your_secure_password';

-- Grant the pg_monitor role (provides read access to all stats views)
GRANT pg_monitor TO pgmonk_monitor;
```

The built-in `pg_monitor` role (available since PG 10) grants read access to all `pg_stat_*` views, `pg_locks`, replication stats, and more — no superuser needed.

### 2. Enable `pg_stat_statements` (Recommended)

The **Top Queries** view and several analyzer rules require the `pg_stat_statements` extension. Without it, pg-monk still works but the Queries tab will be unavailable.

**Step 1:** Add to `shared_preload_libraries` in `postgresql.conf`:

```ini
shared_preload_libraries = 'pg_stat_statements'
```

**Step 2:** Restart PostgreSQL (this setting requires a restart):

```bash
sudo systemctl restart postgresql
```

**Step 3:** Create the extension in your database:

```sql
CREATE EXTENSION IF NOT EXISTS pg_stat_statements;
```

**Step 4 (optional):** Tune `pg_stat_statements` settings in `postgresql.conf`:

```ini
pg_stat_statements.max = 10000          # max number of tracked statements (default: 5000)
pg_stat_statements.track = all          # track all statements including nested (default: top)
pg_stat_statements.track_utility = on   # track utility commands like COPY, CREATE (default: on)
pg_stat_statements.track_planning = on  # track planning time — PG 13+ (default: off)
```

### 3. Enable I/O Timing (Recommended)

For accurate I/O timing data in the dashboard and analyzer:

```ini
# postgresql.conf
track_io_timing = on          # enables I/O timing in pg_stat_statements and pg_stat_io
track_wal_io_timing = on      # enables WAL I/O timing — PG 14+
```

> These add minimal overhead (~2-5%) and are widely recommended for production monitoring.

### 4. Increase Query Text Tracking

By default PostgreSQL truncates query text at 1024 bytes. To see full queries in the Sessions and Queries views:

```ini
# postgresql.conf
track_activity_query_size = 4096    # default: 1024, increase to capture longer queries
```

### 5. PostgreSQL Version-Gated Features

pg-monk automatically detects your PostgreSQL version and enables features accordingly:

| Feature | Minimum PG Version | Configuration Needed |
|---------|-------------------|---------------------|
| Core dashboard, sessions, locks, tables | 9.6+ | None |
| WAL generation stats (`pg_stat_wal`) | **14+** | None |
| Session time tracking (active/idle time) | **14+** | None |
| WAL records per query | **13+** | `pg_stat_statements` |
| JIT stats per query | **15+** | `pg_stat_statements` |
| Logical slot conflict stats | **15+** | None |
| Per-backend I/O stats (`pg_stat_io`) | **16+** | None |
| Separate checkpointer stats (`pg_stat_checkpointer`) | **17+** | None |

### 6. Recommended `postgresql.conf` for Full Monitoring

A summary of all settings that unlock pg-monk's full capabilities:

```ini
# --- Required for Top Queries view ---
shared_preload_libraries = 'pg_stat_statements'

# --- Recommended for better insights ---
track_io_timing = on
track_wal_io_timing = on                  # PG 14+
track_activity_query_size = 4096

# --- pg_stat_statements tuning ---
pg_stat_statements.max = 10000
pg_stat_statements.track = all
pg_stat_statements.track_planning = on    # PG 13+

# --- Useful timeouts (monitored by analyzer) ---
idle_in_transaction_session_timeout = 300000   # 5 min, kills idle-in-tx sessions
log_min_duration_statement = 1000              # log queries slower than 1s
```

> After changing `shared_preload_libraries`, a PostgreSQL **restart** is required. All other settings can be applied with `SELECT pg_reload_conf();` or `pg_ctl reload`.

---

## Dashboard Views

### 1. Overview Dashboard

Connection gauge, throughput, cache hit ratio, WAL stats, archiver status, replication lag, checkpoint info, XID wraparound age, quick insights summary.

### 2. Active Sessions

Live `pg_stat_activity` view — color-coded by state with wait events, query text, PID, and duration.

### 3. Top Queries *(requires pg_stat_statements)*

Sortable by total/mean time, calls, rows. Per-query cache hit ratio, temp block usage. Version-aware: WAL records (PG 13+), JIT stats (PG 15+).

### 4. Table Health

Dead tuples, vacuum status, seq vs index scan ratios, bloat indicators, overdue vacuum detection.

### 5. Lock Monitor

Granted/waiting locks, blocking chains via `pg_blocking_pids()`, lock duration, summary by type.

### 6. Clients

Per-client IP aggregation with drill-down into individual backend threads.

### 7. Insights

Real-time 27-rule analyzer, severity-coded (critical -> warning -> info), configurable thresholds.

---

## Commands

```bash
pg-monk [connection-string]              # Launch interactive dashboard
pg-monk -i 5s [connection]               # Custom refresh interval
pg-monk --config ./my-config.json        # Custom config

pg-monk snapshot [connection]            # JSON to stdout
pg-monk snapshot -f csv -o report.csv    # CSV to file

pg-monk diagnose [connection]            # One-shot health check

pg-monk init-config                      # Write default config to ~/.pgmonk.json
```

---

## Configuration

Generate a default config with `pg-monk init-config`, then edit `~/.pgmonk.json`:

```json
{
  "interval": "2s",
  "thresholds": {
    "cache_hit_warn": 99.0,
    "cache_hit_critical": 95.0,
    "connection_warn": 70.0,
    "connection_critical": 85.0,
    "dead_tuples_warn": 10000,
    "dead_tuples_critical": 100000,
    "idle_in_tx_warn": "30s",
    "idle_in_tx_critical": "5m",
    "xid_warn": 500000000,
    "xid_critical": 1500000000,
    "replication_lag_warn": "30s",
    "replication_lag_critical": "5m",
    "rollback_ratio_warn": 5.0,
    "temp_bytes_warn": 1073741824,
    "long_tx_warn": "1h",
    "long_tx_critical": "6h"
  }
}
```

---

## PostgreSQL Views Used

| View | Data | Version |
|------|------|---------|
| `pg_stat_activity` | Live sessions, wait events | 9.6+ |
| `pg_stat_database` | Transaction rates, cache, deadlocks | 9.6+ |
| `pg_stat_user_tables` | Scan stats, dead tuples, vacuum | 9.6+ |
| `pg_stat_user_indexes` | Index usage | 9.6+ |
| `pg_statio_user_tables` | Table I/O cache ratios | 9.6+ |
| `pg_stat_bgwriter` | Background writer & checkpoints | 9.6+ |
| `pg_locks` | Locks & blocking chains | 9.6+ |
| `pg_stat_replication` | Replication lag & status | 9.6+ |
| `pg_stat_wal` | WAL generation stats | **14+** |
| `pg_stat_archiver` | WAL archiver stats | 9.6+ |
| `pg_stat_io` | Per-backend I/O | **16+** |
| `pg_stat_checkpointer` | Checkpointer stats | **17+** |
| `pg_stat_database_conflicts` | Standby conflicts | 9.6+ |
| `pg_stat_progress_vacuum` | Vacuum progress | 9.6+ |
| `pg_stat_statements` | Query performance (optional) | 9.6+ |

---

## Architecture

```
pgmonk/
├── cmd/pgmonk/main.go              # CLI (Cobra)
├── internal/
│   ├── analyzer/rules.go           # 27 diagnostic rules
│   ├── collector/                   # PostgreSQL metric collection (11 files)
│   ├── config/config.go            # JSON config loader
│   ├── model/metrics.go            # Data structures (20+ types)
│   ├── tui/                        # Terminal UI — 8 views (Bubble Tea)
│   └── export/export.go            # JSON/CSV export
├── .github/workflows/              # CI + Release
├── .goreleaser.yml
├── Makefile
└── go.mod
```

**Tech Stack:** Go, pgx, Bubble Tea, Lip Gloss, Cobra

---

## Development

```bash
git clone https://github.com/sjtly16/pg-monk.git
cd pg-monk
make build       # Build binary
make test        # Run tests
make check       # fmt + vet + test + build
make snapshot    # GoReleaser snapshot
make help        # All targets
```

---

## Roadmap

- [x] 7-view TUI with 16+ pg_stat views
- [x] 27-rule analyzer with configurable thresholds
- [ ] Query plan visualization
- [ ] Historical trend storage

---

## License

MIT — see [LICENSE](LICENSE) for details.
