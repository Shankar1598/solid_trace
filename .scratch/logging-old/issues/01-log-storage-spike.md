# 01: Log storage spike: hot table, manifest and field catalog in DuckDB

**What to build:** EventStore can persist canonical Log records into a `logs_hot` DuckDB table (typed attribute maps, time-ordered UUID id), maintain a `log_files` Parquet manifest table and a per-Project `log_fields` catalog, and read them back. A throughput check on the dev VM decides whether the typed-map layout is fast enough before anything is built on it.

**Blocked by:** None (can start immediately)

**Status:** wontfix

- [ ] Tables `logs_hot`, `log_files`, `log_fields` are created idempotently when EventStore opens DuckDB, alongside the existing events tables
- [ ] A writer appends a batch of Log records with the DuckDB Appender (timestamps as `time.Time`, id as a 16-byte UUID, attributes as three typed maps) and the rows are visible after flush
- [ ] The field catalog is upserted from a batch's key/kind set and can be listed per Project
- [ ] Unit tests use temporary directories and cover write, read-back, catalog upsert and the JSON-serialised fallback for arrays/deep objects
- [ ] A throwaway benchmark appends 1M synthetic Logs in 10k batches and reports rows/s; the ticket records the number and a go/no-go against 20k rows/s sustained

## Comments

2026-09-27: Closed as wontfix. This plan was superseded by the [logs map](../../logs/map.md). The file stays here as reference only.
