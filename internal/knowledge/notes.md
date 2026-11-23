# Version notes for the AI knowledge pack.
#
# Format: one note per line, "[from-to] text" or "[from-] text" (inclusive
# major versions). Lines starting with # are comments.
#
# Every `view.column` in backticks is checked by TestNotesMatchReference
# against the generated column reference for each version the note covers.
# Prefix a reference with ~ (e.g. `~pg_stat_wal.wal_write`) to assert that the
# column does NOT exist in those versions. The ~ is stripped when rendered.

# --- checkpoints & background writer
[12-16] Checkpoint counters live in pg_stat_bgwriter: `pg_stat_bgwriter.checkpoints_timed`, `pg_stat_bgwriter.checkpoints_req`, `pg_stat_bgwriter.checkpoint_write_time`, `pg_stat_bgwriter.checkpoint_sync_time`, `pg_stat_bgwriter.buffers_checkpoint`. Backends writing their own buffers: `pg_stat_bgwriter.buffers_backend`, `pg_stat_bgwriter.buffers_backend_fsync`.
[17-] Checkpoint counters moved to pg_stat_checkpointer: `pg_stat_checkpointer.num_timed`, `pg_stat_checkpointer.num_requested`, `pg_stat_checkpointer.write_time`, `pg_stat_checkpointer.sync_time`, `pg_stat_checkpointer.buffers_written`. pg_stat_bgwriter no longer has `~pg_stat_bgwriter.checkpoints_timed`, `~pg_stat_bgwriter.checkpoints_req` or `~pg_stat_bgwriter.buffers_backend`; backend writes are in pg_stat_io.
[18-] `pg_stat_checkpointer.num_done` counts checkpoints that actually completed (num_timed + num_requested also counts skipped ones).

# --- WAL
[12-13] There is no pg_stat_wal view; estimate WAL volume from pg_current_wal_lsn() differences.
[13-] Per-query WAL generation: `pg_stat_statements.wal_records`, `pg_stat_statements.wal_fpi`, `pg_stat_statements.wal_bytes`.
[14-17] WAL activity is in pg_stat_wal including write/fsync counts and timing: `pg_stat_wal.wal_write`, `pg_stat_wal.wal_sync`, `pg_stat_wal.wal_write_time`, `pg_stat_wal.wal_sync_time` (timing requires track_wal_io_timing = on). `pg_stat_wal.wal_buffers_full` > 0 means wal_buffers is too small.
[18-] pg_stat_wal NO LONGER has `~pg_stat_wal.wal_write`, `~pg_stat_wal.wal_sync`, `~pg_stat_wal.wal_write_time` or `~pg_stat_wal.wal_sync_time`. WAL write/fsync counts and timing are the pg_stat_io rows WHERE object = 'wal': `pg_stat_io.writes`, `pg_stat_io.fsyncs`, `pg_stat_io.write_time`, `pg_stat_io.fsync_time`. `pg_stat_wal.wal_buffers_full` > 0 still means wal_buffers is too small.
[19-] `pg_stat_wal.wal_fpi_bytes` reports bytes of full-page images (a high share after checkpoints suggests checkpoints are too frequent).

# --- I/O
[12-15] There is no pg_stat_io view; use pg_statio_* views and `pg_stat_database.blks_read` / `pg_stat_database.blks_hit`.
[16-] pg_stat_io breaks I/O down by backend_type, object and context: `pg_stat_io.reads`, `pg_stat_io.writes`, `pg_stat_io.extends`, `pg_stat_io.hits`, `pg_stat_io.evictions`, `pg_stat_io.fsyncs`. Timing columns are only non-zero with track_io_timing = on.
[16-17] I/O sizes are given per operation by `pg_stat_io.op_bytes`.
[18-] `~pg_stat_io.op_bytes` was removed; use `pg_stat_io.read_bytes`, `pg_stat_io.write_bytes`, `pg_stat_io.extend_bytes`.

# --- pg_stat_statements
[12-12] pg_stat_statements timing columns are `pg_stat_statements.total_time`, `pg_stat_statements.mean_time`, `pg_stat_statements.max_time` (there is no planning-time split).
[13-] pg_stat_statements splits execution and planning: `pg_stat_statements.total_exec_time`, `pg_stat_statements.mean_exec_time`, `pg_stat_statements.total_plan_time` (planning is only tracked with pg_stat_statements.track_planning = on). `~pg_stat_statements.total_time` does not exist.
[15-] JIT cost per query: `pg_stat_statements.jit_functions`, `pg_stat_statements.jit_generation_time`.
[12-16] Block I/O time per query: `pg_stat_statements.blk_read_time`, `pg_stat_statements.blk_write_time` (requires track_io_timing).
[17-] Block I/O time per query was renamed: `pg_stat_statements.shared_blk_read_time`, `pg_stat_statements.shared_blk_write_time` (requires track_io_timing); `~pg_stat_statements.blk_read_time` no longer exists.

# --- sessions, locks, tables
[13-] `pg_stat_activity.leader_pid` identifies parallel workers' leader.
[14-] `pg_stat_activity.query_id` links sessions to pg_stat_statements (requires compute_query_id).
[14-] Lock waits: pg_locks.waitstart gives when a backend started waiting.
[14-] Session time accounting: `pg_stat_database.session_time`, `pg_stat_database.active_time`, `pg_stat_database.idle_in_transaction_time`, `pg_stat_database.sessions_abandoned`, `pg_stat_database.sessions_killed`.
[13-] `pg_stat_all_tables.n_ins_since_vacuum` drives insert-triggered autovacuum (autovacuum_vacuum_insert_threshold).
[16-] `pg_stat_all_tables.last_seq_scan` / `pg_stat_all_tables.last_idx_scan` show when a table/index was last scanned — useful to confirm an index is unused.
[18-] Cumulative maintenance time: `pg_stat_all_tables.total_vacuum_time`, `pg_stat_all_tables.total_autovacuum_time`, `pg_stat_all_tables.total_analyze_time`.

# --- vacuum
[12-16] Vacuum progress dead-tuple memory: `pg_stat_progress_vacuum.max_dead_tuples`, `pg_stat_progress_vacuum.num_dead_tuples`.
[17-] Vacuum progress reports memory in bytes: `pg_stat_progress_vacuum.max_dead_tuple_bytes`, `pg_stat_progress_vacuum.dead_tuple_bytes`; `~pg_stat_progress_vacuum.max_dead_tuples` no longer exists.
[19-] `pg_stat_autovacuum_scores.relid` lists tables autovacuum considers and their priority scores.

# --- new in 19
[19-] `pg_stat_lock.locktype`, `pg_stat_lock.waits`, `pg_stat_lock.wait_time` give cumulative lock-wait statistics per lock type.
