# pg-monk

**Real-time PostgreSQL performance dashboard for your terminal.**

Think `htop`, but for Postgres. Zero dependencies, single binary, instant insights.

[![CI](https://github.com/sjtly16/pgmonk/actions/workflows/ci.yml/badge.svg)](https://github.com/sjtly16/pgmonk/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/sjtly16/pgmonk)](https://github.com/sjtly16/pgmonk/releases/latest)
![License](https://img.shields.io/badge/license-MIT-blue.svg)
![Go](https://img.shields.io/badge/go-1.22+-00ADD8.svg)

![pg-monk demo](docs/media/demo.gif)

<sub>▶ Full-resolution video: [docs/media/demo.mp4](docs/media/demo.mp4) · recorded against the [demo database](scripts/demo/) with a deliberately unhealthy workload.</sub>

---

## Features

- **7-view TUI dashboard** — live-updating terminal UI with connection gauges, throughput, WAL, replication, and more
- **28 analyzer rules** — real-time insights with configurable thresholds for cache, connections, blocking locks, XID wraparound, replication lag, vacuum health, and more
- **Local AI insights (optional)** — root-cause analysis from a model running on your machine (Ollama or any OpenAI-compatible server), grounded in knowledge of *your exact PostgreSQL version*; suggested SQL is verified against your server. Nothing leaves your machine.
- **Active session monitor** — color-coded view of all database sessions with wait events
- **Top queries analysis** — slowest queries ranked by total/mean time, calls, cache efficiency, WAL generation, and JIT stats (requires `pg_stat_statements`)
- **Table health** — dead tuples, vacuum status, sequential scan warnings, bloat detection, stale statistics
- **Lock monitor** — active locks with blocking chain visualization and lock duration
- **Per-client drill-down** — aggregated connection metrics per client IP, with per-backend thread detail
- **20+ PostgreSQL data sources** — pg_stat_wal, pg_stat_io, pg_stat_archiver, pg_stat_checkpointer, pg_stat_replication, and more
- **Version-aware collection** — supports PostgreSQL 12 through 19, gating PG 13+/14+/15+/16+/17+ features; every major version is integration-tested in CI
- **Snapshot export** — JSON/CSV one-shot reports for automation
- **Health diagnostics** — CLI `diagnose` command with severity-ranked insights
- **Configurable thresholds** — customize analyzer sensitivity via `~/.pgmonk.json`
- **Zero extensions required** — works out of the box; unlocks more with `pg_stat_statements`
- **psql-compatible flags** — `-H`, `-p`, `-U`, `-d` just work

---

## Quick Start

### Install

**Prebuilt binaries** for Linux, macOS and Windows (amd64/arm64) are on the [releases page](https://github.com/sjtly16/pgmonk/releases/latest):

```bash
# macOS (Apple Silicon) — swap darwin_arm64 for linux_amd64, linux_arm64, darwin_amd64, ...
VERSION=0.1.0
curl -sSL "https://github.com/sjtly16/pgmonk/releases/download/v${VERSION}/pg-monk_${VERSION}_darwin_arm64.tar.gz" | tar xz
sudo mv pg-monk /usr/local/bin/
```

**With Go** (1.22+):

```bash
go install github.com/sjtly16/pgmonk/cmd/pg-monk@latest
```

**From source:**

```bash
git clone https://github.com/sjtly16/pgmonk.git
cd pgmonk && make build
```

> **Try it without a database of your own:** `make demo-db && make demo-load`, then in another terminal
> `pg-monk postgresql://postgres:demo@localhost:55433/shop`. See [scripts/demo](scripts/demo/).

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

## Dashboard Views

### 1. Overview Dashboard

Connection gauge, throughput, cache hit ratio, WAL stats, checkpoints, XID wraparound age, and the longest-running active queries.

![Dashboard](docs/media/dashboard.png)

### 2. Active Sessions

Live `pg_stat_activity` — color-coded by state (active, idle, idle in transaction, waiting) with wait events, application, duration and query. Press `Enter` for a detail panel, `s` to change sort.

![Sessions](docs/media/sessions.png)

### 3. Top Queries *(requires pg_stat_statements)*

Sortable by total/mean time, calls and rows, with per-query cache hit ratio, planning time share and temp block usage. Version-aware: WAL records (PG 13+), JIT stats (PG 15+). pg-monk's own monitoring queries are filtered out.

![Top queries](docs/media/queries.png)

### 4. Table Health

Dead tuples, live rows, sequential vs index scans, size and last vacuum — sortable, with tables that need vacuuming highlighted.

![Tables](docs/media/tables.png)

### 5. Lock Monitor

Blocking chains built from `pg_blocking_pids()`: the session holding the lock (often one that is *idle in transaction*) at the root, with everything queued behind it nested underneath — plus how long each has been waiting and on what.

![Locks](docs/media/locks.png)

### 6. Clients

Connections aggregated per client address — active / idle / idle-in-tx / waiting counts and longest query and transaction. Press `Enter` to drill into individual backends.

![Clients](docs/media/clients.png)

### 7. Insights

28 rule-based checks run on every refresh, ranked critical → warning → info, each with a concrete suggestion. Thresholds are configurable (see [Configuration](#configuration)).

![Insights](docs/media/insights.png)

The same analysis is available non-interactively — handy for cron jobs and CI:

```bash
pg-monk diagnose -H localhost -U monitor -d mydb
```

![Diagnose](docs/media/diagnose.png)

---

## AI Insights (local LLM)

The rule engine tells you *what* is wrong. With `--ai`, a language model running on your own machine explains *why* — connecting findings into root causes (e.g. "one session idle in a transaction is blocking two writers **and** stopping vacuum, which is why `products` bloats right after autovacuum") and ranking what to fix first.

```bash
# 1. Install Ollama (https://ollama.com) and pull a model
ollama pull qwen2.5:7b

# 2. Check the setup (server, model, and the knowledge pack for your database)
pg-monk ai check postgresql://monitor@localhost/mydb

# 3. Use it
pg-monk --ai postgresql://monitor@localhost/mydb          # Insights tab (7); press r to re-analyze
pg-monk diagnose --ai postgresql://monitor@localhost/mydb # one-shot, rules + AI
```

![AI insights](docs/media/ai-insights.png)

<sub>`qwen2.5:7b` on the demo database. Checks marked ✓ were planned with EXPLAIN on the server; the ℹ note is pg-monk correcting the model (a restart-only setting can't be applied with `pg_reload_conf()`).</sub>

**How it stays accurate.** Small local models confidently invent `pg_stat_*` columns, and they don't know releases newer than their training data (in testing, `qwen2.5` 3B and 7B both listed non-existent `pg_stat_wal` columns for PostgreSQL 18). pg-monk grounds the model instead of trusting it:

1. **Version detection** — on connect, pg-monk reads the server version *and* the live catalog of every statistics view and column.
2. **Version knowledge pack** — pg-monk ships a pack per major version (12–19), generated from that release's official documentation plus curated notes on what moved between releases (e.g. PG 18 moved WAL write/fsync timing from `pg_stat_wal` to `pg_stat_io`). Only columns that exist on your server are included. Tests verify every note against the docs, and CI verifies the docs against real servers.
3. **Verification** — every `view.column` the model mentions is checked against your catalog, every suggested `SELECT` is planned with `EXPLAIN` in a read-only transaction, and settings that need a restart are flagged when the model suggests a reload. pg-monk never runs anything the model suggests.

**Fast after the first run.** The version knowledge is a byte-identical prefix on every request, so the model server reuses its prompt cache; only the snapshot digest is processed each time.

**Privacy.** The prompt contains statistics and normalized query text. With the default `localhost` server nothing leaves your machine; pg-monk warns if the AI server is elsewhere. Run `pg-monk ai prompt` to see exactly what would be sent.

**Models.** `qwen2.5:7b` (default) gives the best results; `qwen2.5:3b` works on machines with less memory but is noticeably weaker. Any OpenAI-compatible server works too:

```bash
pg-monk --ai --ai-provider openai --ai-url http://localhost:1234/v1 --ai-model <model>   # LM Studio, llama.cpp, vLLM, ...
```

Inference speed depends on your hardware. Measured with `qwen2.5:7b` on the demo database: about **15–18 s** per analysis with native Ollama on an Apple M5 Pro, versus **75–90 s** CPU-only (Ollama in Docker on the same Mac). Like any small model it can be wrong or vary between runs — treat its output as a starting point and check the ✓-verified queries yourself.

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

Supported servers: **PostgreSQL 12 – 19** (older, end-of-life versions are untested).

pg-monk automatically detects your PostgreSQL version and enables features accordingly:

| Feature | Minimum PG Version | Configuration Needed |
|---------|-------------------|---------------------|
| Core dashboard, sessions, locks, tables | 12+ | None |
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

## Commands

```bash
pg-monk [connection-string]              # Launch interactive dashboard
pg-monk -i 5s [connection]               # Custom refresh interval
pg-monk --config ./my-config.json        # Custom config

pg-monk snapshot [connection]            # JSON to stdout
pg-monk snapshot -f csv -o report.csv    # CSV to file

pg-monk diagnose [connection]            # One-shot health check
pg-monk diagnose -H db -U monitor -d app # psql-style flags work on every subcommand

pg-monk init-config                      # Write default config to ~/.pgmonk.json

pg-monk --ai [connection]                # Dashboard with AI insights (local LLM)
pg-monk diagnose --ai [connection]       # Health check + AI root-cause analysis
pg-monk ai check [connection]            # Verify AI server, model and knowledge pack
pg-monk ai prompt [connection]           # Print the exact prompt that would be sent
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
  },
  "ai": {
    "enabled": false,
    "provider": "ollama",
    "base_url": "http://localhost:11434",
    "model": "qwen2.5:7b",
    "num_ctx": 8192,
    "keep_alive": "30m",
    "timeout": "5m"
  }
}
```

AI settings can also come from `PGMONK_AI_PROVIDER`, `PGMONK_AI_URL`, `PGMONK_AI_MODEL`, `PGMONK_AI_API_KEY` (or `OLLAMA_HOST`), and the `--ai*` flags.

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
├── cmd/pg-monk/main.go             # CLI (Cobra)
├── internal/
│   ├── analyzer/rules.go           # 28 diagnostic rules
│   ├── ai/                         # Local LLM insights: digest, prompt, providers, validation
│   ├── knowledge/                  # Per-version stats-view knowledge (generated from PG docs)
│   ├── collector/                   # PostgreSQL metric collection (11 files)
│   ├── config/config.go            # JSON config loader
│   ├── model/metrics.go            # Data structures (20+ types)
│   ├── tui/                        # Terminal UI — 8 views (Bubble Tea)
│   └── export/export.go            # JSON/CSV export
├── scripts/demo/                   # Demo database, workload and VHS recording scripts
├── docs/media/                     # README screenshots and demo video
├── .github/workflows/              # CI + Release
├── .goreleaser.yml
├── Makefile
└── go.mod
```

**Tech Stack:** Go, pgx, Bubble Tea, Lip Gloss, Cobra

---

## Development

```bash
git clone https://github.com/sjtly16/pgmonk.git
cd pgmonk
make build       # Build binary
make test        # Run tests
make check       # fmt + vet + test + build
make snapshot    # GoReleaser snapshot
make help        # All targets

# Demo database + README media (Docker)
make demo-db     # seeded PostgreSQL 17 on :55433
make demo-load   # realistic, slightly unhealthy workload
make media       # re-record docs/media with VHS
```

---

## Roadmap

- [x] 7-view TUI with 16+ pg_stat views
- [x] 28-rule analyzer with configurable thresholds
- [x] Blocking-chain lock monitor
- [x] AI insights from a **local LLM**, grounded in version-specific knowledge — nothing leaves your machine
- [ ] Query plan visualization
- [ ] Historical trend storage

---

## License

MIT — see [LICENSE](LICENSE) for details.
