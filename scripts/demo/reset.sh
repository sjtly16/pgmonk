#!/usr/bin/env bash
# Terminates demo sessions and clears pg_stat_statements so a fresh recording
# starts from a clean slate.
set -euo pipefail
DSN=${DSN:-postgresql://postgres:demo@localhost:55433/shop}
psql "$DSN" -X -qAt \
  -c "SELECT count(pg_terminate_backend(pid)) FROM pg_stat_activity WHERE pid <> pg_backend_pid() AND backend_type = 'client backend';" \
  -c "SELECT pg_stat_statements_reset();" >/dev/null
echo "demo database reset"
