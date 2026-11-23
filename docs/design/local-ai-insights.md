# Design: version-aware AI insights with a local LLM

Status: **implemented** in v0.2 (see `internal/knowledge`, `internal/ai`) · Scope: open-source, runs fully local

## Goal

When pg-monk connects to a database it already knows the exact server version.
Use that to pick a **version-specific knowledge pack** describing the statistics
views (what each column means, what changed in this release), ship it inside the
binary, and use it to ground an LLM running locally (Ollama, LM Studio,
llama.cpp, vLLM) so its insights reference real columns and real behaviour for
*this* server — not a blend of whatever versions it saw in training.

## Research findings

All measurements below were taken on 2026-11-23 against PostgreSQL 12–19 in
Docker and `qwen2.5:3b` on Ollama 0.40 (CPU only, no GPU).

### 1. Small local models do not know version-specific catalog details

Asked about PostgreSQL 18 with no reference material, `qwen2.5:3b`:

| Question | Answer without pack | With PG 18 pack |
|---|---|---|
| Columns of `pg_stat_wal` | Invented 20 columns, none correct | `wal_records, wal_fpi, wal_bytes, wal_buffers_full, stats_reset` — exact |
| How to measure WAL write/fsync time | "use `pg_stat_wal_receiver` … PG 18 is quite old" | Correct only once *version notes* were added (`pg_stat_io WHERE object = 'wal'`) |
| `pg_stat_io` column for I/O size | Partly invented | `read_bytes` / `write_bytes` — correct |

A bigger model does not fix this. `qwen2.5:7b` without the pack also failed all
three (invented 14 `pg_stat_wal` columns, a non-existent `pg_stat_file` view, and
`num_read`/`num_written` on `pg_stat_io`); with the pack and version notes it
answered all three correctly. Both models still occasionally add a wrong caveat
(e.g. tying I/O timing to `pg_stat_statements` instead of `track_io_timing`),
which is what the post-generation column check and the knowledge pack's
interpretation notes are for. Cold-prefix cost on CPU: ~27 s (3B) vs ~54 s (7B).

Newer releases are invisible to models trained before them: PostgreSQL 19
(Nov 2026) adds `pg_stat_lock`, `pg_stat_autovacuum_scores`, `pg_stat_recovery`
and `pg_stat_progress_repack`. This matches the version-drift problem described
by pgEdge and the version-specific retrieval approach in the DBCooker paper.

### 2. Version drift is real and bites pg-monk itself

Diffing the official docs (`doc/src/sgml/monitoring.sgml` per release branch):

- **17 → 18:** `pg_stat_wal` *loses* `wal_write`, `wal_sync`, `wal_write_time`, `wal_sync_time` (now `pg_stat_io` rows with `object = 'wal'`); `pg_stat_io` loses `op_bytes`, gains `read_bytes`/`write_bytes`/`extend_bytes`; `pg_stat_checkpointer` gains `num_done`, `slru_written`; `pg_stat_all_tables` gains `total_*vacuum_time`/`total_*analyze_time`.
- **18 → 19:** four new views (above), `stats_reset` on table/index/function views, `started_by` on progress views, `pg_stat_wal.wal_fpi_bytes`.

While researching this, pg-monk's own collector was found querying the removed
PG 18 columns and PG 18's `num_done` on PG 17 — both failing silently (fixed in
`release/v0.1.0`). The same version profile that grounds the LLM should also
drive collection.

### 3. The catalog tells us *which* columns exist, not *what they mean*

On a live server, `pg_attribute` gives the exact column list for every
`pg_stat_*` view (handles minor versions, forks such as Aurora/Neon, and
extension versions). But `col_description()` is NULL for all 595 of them, so
meaning has to ship with pg-monk.

### 4. Size of a knowledge pack

Distilling column descriptions (first sentence each) from the docs:

| Scope | PG 17 | PG 18 | PG 19 |
|---|---|---|---|
| All stats views | ~7.9k tokens | ~8.1k | ~9.3k |
| Only views pg-monk collects | ~4.7k | ~4.8k | ~5.3k |

Ollama's default context is 4k tokens and it **silently truncates from the
start** (dropping the system prompt). The pack must be trimmed to the columns
pg-monk actually reports (target ≤ 2k tokens) and `num_ctx` set explicitly.

### 5. "Store it, don't resend it": prefix caching does this for free

The alternatives for keeping knowledge out of the per-request cost:

| Option | Verdict |
|---|---|
| Fine-tune / LoRA per PG version | ✗ Per base model, poor at exact facts, redo every release |
| Embedding RAG over the docs | ✗ Unneeded — the lookup key (server version + views in use) is known exactly; adds an embedding model dependency |
| Ollama `Modelfile` with the pack as `SYSTEM` | ~ Works, but needs a setup step per version and is Ollama-only; same runtime cost as below |
| **Stable system-prompt prefix + server KV cache** | ✓ Measured below |

Same 4.1k-token PG 18 pack as the first message, different question each call:

| | Prompt processing |
|---|---|
| Cold (first call / prefix changed by 1 char) | **26.8 s** |
| Warm (prefix identical) | **0.23 s** (~115×) |

The pack is sent each time but processed once per model load. Requirements:
byte-identical prefix (no timestamps or snapshot data in it), knowledge first,
volatile data last, a long `keep_alive`. llama.cpp's server behaves the same
way (`cache_prompt`).

## Design

### Runtime flow (maps 1:1 to the proposal)

```
connect ──▶ detect version (server_version_num, extversion of pg_stat_statements)
        ──▶ select embedded pack for that major version         internal/knowledge/pg{12..19}.md
        ──▶ verify against live catalog (pg_attribute)            drop absent columns, note unknown ones
        ──▶ collect metrics (collector already version-aware)
        ──▶ build prompt:
              [system]  role + output schema + version notes + column reference   ← stable, cached
              [user]    compact snapshot digest + rule findings                   ← changes every call
        ──▶ local LLM (Ollama /api/chat with JSON-schema `format`, or OpenAI-compatible)
        ──▶ validate: every `view.column` mentioned must exist on this server; flag/strip otherwise
        ──▶ render in Insights tab / `diagnose --ai`
```

### Knowledge packs

- One markdown file per major version, embedded with `go:embed`
  (`internal/knowledge/`). Two parts:
  1. **Version notes** — hand-curated, short: what moved/was removed/added in
     this release and its predecessors that matters for diagnosis.
  2. **Column reference** — generated from that branch's `monitoring.sgml`,
     restricted to columns pg-monk reports.
- Generated by `go generate ./internal/knowledge` (fetches each
  `REL_<n>_STABLE` docs file, distils, writes the pack). Output is reviewed and
  committed, so builds stay offline and reproducible.
- New major version = add a branch name, regenerate, write version notes.
- Unknown newer server (e.g. PG 20 before a pack exists): use the newest pack,
  plus the live column list for anything it doesn't describe, and tell the user.

### One version profile for collection *and* AI

Today version gating is ad-hoc `if VersionNum >= …` in six collector files.
Move the facts into a `versionprofile` built at connect time from the live
catalog (which columns/views exist), consumed by both the collector and the
pack selector — so a removed column can never fail silently again.

### Prompt contents

- **System (cached):** DBA role; "use only these columns"; JSON output schema;
  version notes; column reference.
- **User (per call, target ≤ 1.5k tokens):** overview numbers, top N queries
  (normalised text from pg_stat_statements, truncated), unhealthy tables,
  blocking chains, long/idle-in-tx sessions, key settings, and the 27-rule
  findings. The LLM's job is **correlation and root cause** (e.g. one
  idle-in-transaction session blocking two writers *and* preventing vacuum,
  explaining the bloat finding) — not re-deriving what the rules already catch.

### Output

JSON schema enforced via Ollama `format` (constrained decoding) or
`response_format: json_object` on OpenAI-compatible servers:

```json
{ "summary": "…",
  "findings": [ { "severity": "critical|warning|info", "title": "…",
                  "evidence": "…metrics…", "root_cause": "…",
                  "action": "…", "sql": "optional, read-only" } ] }
```

`temperature: 0`. If parsing fails, show the raw text rather than nothing.

### Configuration (`~/.pgmonk.json`, off by default)

```json
"ai": {
  "enabled": true,
  "provider": "ollama",                    // or "openai" for any OpenAI-compatible server
  "base_url": "http://localhost:11434",
  "model": "qwen2.5:7b",
  "num_ctx": 8192,
  "timeout": "180s"
}
```

Flags: `--ai`, `--ai-model`, `--ai-url`. A non-localhost `base_url` prints a
one-time warning that the prompt contains query text.

### UX

- Insights tab: AI panel above the rule findings; runs on entering the tab and
  on `r`, never on a timer (local inference is slow on CPU). Shows model name,
  "warming up" on the first (uncached) call, and elapsed time.
- `pg-monk diagnose --ai`: rules first, then AI section (streamed).
- `pg-monk ai check`: verifies the endpoint, that the model is pulled, and
  context size.

## Non-goals (v0.2)

Fine-tuning, embedding-based RAG, agentic tool-calling (the model running its
own SQL — see Xata Agent for that approach), history/trend storage.

## Open questions

1. Default model: `qwen2.5:7b` (better reasoning, ~4.7 GB) vs `qwen2.5:3b`
   (fits low-RAM laptops). Needs a quality comparison on the demo workload.
2. Packs for PG 12–14 need the older SGML table markup parsed (pre-PG 15 docs
   use a different table structure).
3. Should the column reference be restricted further to *only* views that have
   rows in the current snapshot, at the cost of one cache miss when that set
   changes?

## Sources

- [PostgreSQL 18 release notes](https://www.postgresql.org/docs/current/release-18.html) ·
  [commit removing pg_stat_wal read/sync fields](https://www.postgresql.org/message-id/E1tmMk9-000NUo-2M@gemulon.postgresql.org) ·
  [Per-backend IO stats in PG 18](https://www.red-gate.com/blog/per-backend-io-stats-in-postgresql-18)
- [PostgreSQL 19 released](https://www.postgresql.org/about/news/postgresql-19-released-3449/) ·
  [PG 19 release notes](https://www.postgresql.org/docs/release/19.0/) ·
  [New system views in PG 19](https://clickhouse.com/blog/postgres-19-new-system-views)
- [Ollama structured outputs](https://docs.ollama.com/capabilities/structured-outputs) ·
  [Ollama prompt caching](https://leanpub.com/read/ollama/prompt-caching)
- [pgEdge: Teaching an LLM what it doesn't know about PostgreSQL](https://www.pgedge.com/blog/teaching-an-llm-what-it-doesn-t-know-about-postgresql) ·
  [DBCooker (version-specific retrieval)](https://arxiv.org/pdf/2604.06231)
- Prior art: [Xata Agent](https://github.com/xataio/agent) (tools + playbooks, supports Ollama) ·
  [pgAssistant](https://beh74.github.io/pgassistant-blog/doc/using_llm) (deterministic analysis + optional local LLM)
