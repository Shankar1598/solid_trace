# 11: How do the filter bar and SQL mode query logs?

Type: grilling
Status: open
Blocked by: 04, 07
Map: [SolidTrace Logs](../map.md)

## Question

The MVP query surface is a filter bar (Project, time range, free-text search) plus SQL mode (decided 2026-09-27; APL and the query builder are on the map's Future list). What does free-text search match (message only, or every field) and how, given no term index? How does a filter-bar query become SQL, and where is it built (Console or EventStore)? What does SQL mode expose to the user: table and column names for logs, and how the JSON fields are reached?

## Input: SQL mode (decided 2026-09-27)

Decided with the user while working [Which production-readiness gaps must be fixed before or alongside logs?](05-prod-readiness-gaps-for-logs.md). This settles whether SQL mode ships and under what safety rules.

- **SQL mode ships in the MVP.** The threat model is one self-hosted Organization. Any member may read every Project's logs, so the Project-scoping escape routes in [What APL/KQL surface is worth supporting, and can DuckDB SQL mode be sandboxed?](04-apl-kql-surface-and-sql-sandboxing.md) do not apply.
- **Queries are read-only.** EventStore accepts one SELECT statement only (checked with `json_serialize_sql`) and runs it on a separate DuckDB instance from the one log ingest writes through.
- **Accepted risks for the MVP** (the user accepted both explicitly):
  - A large query can exhaust memory and take down EventStore. That stops log ingest and Event ingest, so error tracking is down too, and Sentry SDKs drop Events until EventStore restarts. No resource limits in the MVP.
  - A read-only query can read any file EventStore can see, including Console's SQLite. No host-file confinement in the MVP.

### Open items, deferred until SQL mode is built

Not part of the MVP spec. Revisit once the feature exists.

- **Host file reads:** a read-only SELECT can still read any file the process can see (`FROM '/path'`, `read_csv`), including Console's SQLite. Candidate fix: `enable_external_access=false` with `allowed_directories` set to the log data.
- **Resource limits on the query instance:**
  - Row cap as SQL: wrap as `SELECT * FROM (<query>) LIMIT 10001`. The Go driver materialises the whole result outside `memory_limit` before the first row is read, so a cap in the Go row loop is too late.
  - `memory_limit` (for example 25% of RAM) and `max_temp_directory_size`, so a spill cannot fill the log disk.
  - `threads` below the core count, so log ingest keeps CPU.
  - Timeout by Go context cancel (for example 30 s) and at most 2 concurrent user queries.
- **Checks before relying on those limits:**
  - Does the outer `LIMIT` bound materialised memory? Re-run the research's 8M-row SELECT with the wrapper.
  - Does the DuckDB 2.0 driver cancel during preparation (the duckdb-go v2.5.5 fix)?
  - Do these settings behave the same on DuckDB 2.0 as on 1.4.3?
