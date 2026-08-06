# Migrations

Numbered `NNNN_name.sql`, applied in order at startup inside a single transaction
each (`../migrate.go`), forward-only, tracked in `schema_migrations`. Split one
migration per task/change rather than batching.

## Backfills — read before writing one

The migration runs at boot **holding the single write lock** (`SetMaxOpenConns(1)`,
`busy_timeout=10000`). A slow backfill blocks all writes and, past the busy
timeout, surfaces `SQLITE_BUSY` to the recorder.

- **Backfills must be single-pass.** The `0022` event-seq backfill used a
  correlated subquery (`COUNT(*)` over `events e2` per row) — that is **O(n²)**:
  1.6 s at 4,910 events. The same pattern over `segments` (~59k rows) would run
  **minutes** at boot. Prefer a window function or a single ordered pass
  (e.g. `ROW_NUMBER() OVER (ORDER BY …)` via `UPDATE … FROM`), not a per-row
  correlated subquery.
- **Any backfill touching `segments` (the large table) must have its runtime
  measured on a copy first** — online-backup the production DB and time the
  migration via `store.Open` on the copy before deploying (see the 1.B dry-run).
- Keep everything else additive and metadata-only where possible (`ADD COLUMN`
  with a constant default is a no-rewrite operation; `CREATE TABLE`/`CREATE INDEX`
  are cheap on small tables). Additive migrations let a code revert roll back
  without a schema rollback.
