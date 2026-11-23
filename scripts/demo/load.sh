#!/usr/bin/env bash
# Generates a realistic, slightly unhealthy workload against the demo database
# so every pg-monk view has something to show. Runs until interrupted.
#
#   DSN=postgresql://postgres:demo@localhost:55433/shop ./scripts/demo/load.sh
set -uo pipefail

DSN=${DSN:-postgresql://postgres:demo@localhost:55433/shop}

# run APP_NAME psql-args...  — one psql invocation tagged with an application name.
run() { local app=$1; shift; PGAPPNAME=$app psql "$DSN" -X -q "$@" >/dev/null 2>&1 || true; }

# hold_open APP_NAME SQL... — run statements, then keep the session open so it
# sits "idle in transaction" instead of exiting.
hold_open() {
  local app=$1; shift
  { printf '%s\n' "$@"; sleep 100000; } | PGAPPNAME=$app psql "$DSN" -X -q >/dev/null 2>&1 || true
}

pids=()
cleanup() { kill "${pids[@]}" 2>/dev/null; pkill -P $$ 2>/dev/null; wait 2>/dev/null; }
trap cleanup EXIT INT TERM

# 1. OLTP-ish traffic from several app connections.
for app in checkout-api catalog-api reporting; do
  (
    while true; do
      run "$app" \
        -c "SELECT * FROM orders WHERE customer_id = (random()*49999)::int + 1;" \
        -c "SELECT count(*) FROM order_items WHERE order_id = (random()*199999)::int + 1;" \
        -c "UPDATE products SET stock = stock - 1 WHERE id = (random()*4999)::int + 1;" \
        -c "SELECT country, count(*), sum(o.total) FROM orders o JOIN customers c ON c.id = o.customer_id GROUP BY country;" \
        -c "INSERT INTO events (kind, payload) VALUES ('view', repeat('y', 200));"
    done
  ) &
  pids+=($!)
done

# 2. A worker that forgets to commit (idle in transaction).
hold_open billing-worker "BEGIN;" "UPDATE orders SET status = 'paid' WHERE id = 42;" &
pids+=($!)

# 3. Lock chain: holder grabs a row lock and sits idle, two waiters queue up.
hold_open admin-console "BEGIN;" "SELECT * FROM customers WHERE id = 7 FOR UPDATE;" &
pids+=($!)
sleep 1
for app in signup-api email-sync; do
  run "$app" -c "UPDATE customers SET email = email WHERE id = 7;" &
  pids+=($!)
done

# 4. A periodic heavy analytical query.
(
  while true; do
    run nightly-report -c "SELECT o.status, p.name, sum(oi.qty) FROM order_items oi JOIN orders o ON o.id = oi.order_id JOIN products p ON p.id = oi.product_id GROUP BY 1,2 ORDER BY 3 DESC;"
    sleep 5
  done
) &
pids+=($!)

echo "demo load running — Ctrl+C to stop"
wait
