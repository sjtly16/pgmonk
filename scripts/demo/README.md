# Demo database & README media

Everything needed to reproduce the screenshots and demo video in the main
README. Requires Docker and `psql`.

```bash
make demo-db      # PostgreSQL 17 + pg_stat_statements on localhost:55433, seeded with a small "shop" schema
make demo-load    # leave running in another terminal: traffic, an idle-in-transaction session,
                  # a blocking lock chain and a heavy reporting query
make media        # re-records docs/media/*.png, demo.mp4 and demo.gif with VHS (in Docker)
make media-ai     # screenshots the AI panel; needs Ollama running on the host with qwen2.5:7b pulled
make demo-down    # clean up
```

You can also point pg-monk at it directly while `demo-load` runs:

```bash
pg-monk postgresql://postgres:demo@localhost:55433/shop
```

| File | Purpose |
|------|---------|
| `seed.sql` | Schema + data. `events` has autovacuum disabled and heavy churn so it bloats; `order_items` has no index on `order_id`. |
| `load.sh` | Concurrent workload: OLTP traffic, a forgotten open transaction, a row-lock chain, a periodic analytical query. |
| `reset.sh` | Terminates demo sessions and resets `pg_stat_statements` before a recording. |
| `screenshots.tape`, `demo.tape` | [VHS](https://github.com/charmbracelet/vhs) scripts for the README media. |
