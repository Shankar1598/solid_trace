# Logs: Pebble first, a one-hour hot table, then hourly per-Project Parquet with typed MAP Attributes

Status: superseded by [ADR 0005](0005-logs-per-project-hot-tables-promote-at-ingest.md) (promotion at ingest, per-Project hot tables), and since by [ADR 0016](0016-logs-and-events-in-clickhouse.md) (Logs and Events in ClickHouse).

Logs are written to Pebble before the `200`, then to a hot DuckDB table in its own `logs.duckdb` that holds only the current and previous hour, then exported every hour to zstd Parquet on local disk, one file per Project per hour (`project=<id>/day=<d>/hour=<h>/`). Attributes live in four typed MAP columns (string, DOUBLE, bool, JSON text for arrays), and each Parquet file also promotes its most common keys to typed columns of their own. Retention and the disk limit delete whole folders. We chose this because a DuckDB file never shrinks, so only file-per-Project-per-period storage lets per-Project retention free disk; because hourly rather than daily files put the last 24 hours, the explorer's busiest range, in Parquet where promoted columns make common-key searches take seconds on 4 vCPU; and because typed MAPs were up to 5× faster than JSON text for Attribute queries and match the query language's non-coercing `==`.

## Considered options

- **JSON text for Attributes:** simpler, 60% faster writes, but 2.5–5× slower rare-key filters and group-bys.
- **VARIANT:** shredding was unusable on the DuckDB 2.0 alpha (a heap-corruption crash, database-invalidating filter errors). Re-check on 2.0.
- **All Logs in one DuckDB table:** deleted rows never shrink the file, so retention would not free disk.
- **Daily rotation, as Events do:** fewer files and one export a night, but the last 24 hours would stay in the hot table, where promoted columns cannot exist (about 20 s for a common-key day search on 4 vCPU).
- **DuckLake:** not needed; folders by Project and hour need no catalog.
- **Writing straight to DuckDB before the `200`:** checkpoint stalls of 2–3 s at 5× bursts would turn into 503s.

## Consequences

- Each Log is written twice (Pebble, then DuckDB); a Pebble batch is deleted once DuckDB has committed it.
- A whole number too large for a DOUBLE is stored, and must be queried, as a string.
- Parquet files differ in which keys are promoted; the query compiler reads `coalesce(column, map[key])` and DuckDB reads the files with `union_by_name`. The `logs` view ([ADR 0003](0003-unrestricted-sql-queries-behind-an-instance-setting.md)) hides all of this.
- A day-long search on a rare key still scans every map in the day; it streams results newest first until a search index exists.
- The design uses only DuckDB 1.5 features; EventStore runs the released 1.5.5 rather than the 2.0 alpha.

Source: [How are logs stored and retained?](../../.scratch/logs/issues/10-storage-architecture-and-retention.md).
