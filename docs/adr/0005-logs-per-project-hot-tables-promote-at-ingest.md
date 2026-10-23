# Logs: per-Project hot tables that promote up to 100 Attribute keys at ingest, re-ranked daily

Supersedes [ADR 0004](0004-logs-hot-table-and-hourly-parquet.md).

Status: superseded by [ADR 0016](0016-logs-and-events-in-clickhouse.md) (Logs and Events in ClickHouse, with Attributes in one JSON column and no promotion). Before that, its "holding the current and previous hour, then exported every hour …, one file per Project per hour" and its late Logs' "before the previous hour or at or after the next one" are superseded by [ADR 0013](0013-busy-projects-logs-exported-every-quarter-hour.md) (busy Projects' quarter-hours exported to their own files, and a hot window that starts at the quarter cutoff).

Logs are written to Pebble before the `200`, then to a hot DuckDB table **per Project** in `logs.duckdb` holding the current and previous hour, then exported every hour to zstd Parquet on local disk, one file per Project per hour (`project=<id>/day=<d>/hour=<h>/`). Attributes live in four typed MAP columns (string, DOUBLE, bool, JSON text for arrays). A Project's (type, key) pairs are **promoted at ingest** to typed columns named after the key (`"attrs_string.http.status"`), in the hot table and in Parquet alike, until the Project has 100. When the set is full, the first Log of a new UTC day re-ranks it: the day's set is the 100 keys carried by the most Logs on the most recent counted day. The promoted set is a table in `logs.duckdb`, the only place the query compiler learns columns from. We chose this because the benchmark ([ticket 14](../../.scratch/logs/issues/14-benchmark-storage-design-on-1-5-5.md)) missed the 5 s common-key day search with promotion only in Parquet: the hot table's two MAP-only hours cost more than 22 promoted Parquet hours, and promoting in the hot table made them 12–15× faster. 100 columns cost about 10% of ingest; 1000 columns could not keep up with the target rate.

## Considered options

- **Each Parquet file promotes its own top keys at export (ADR 0004):** the hot table stays MAP-only and misses the day target; files differ in columns, so reads need `coalesce` across files (4–6× slower than the column) or footer reads.
- **Promote every key up to 1000 per Project, dropping all-NULL columns nightly (as Axiom does):** 2.6k Logs/s with 1000 columns, below the 3.2k target.
- **One shared hot table with key-named columns:** its columns would be every Project's keys together, past the ingest limit.
- **Fixed typed slot columns (`str_01`…) mapped to keys per Project and day:** no DDL, but unreadable without the mapping, a fixed split across types, and always 100 columns written. Key-named columns were preferred.
- **One shared table rather than one per Project:** about as fast for a Project with most of the volume, but a small Project's newest-hours queries and "latest N" would scan every Project's rows.
- **Counting popularity by query use:** a key nobody has queried yet would never be promoted.
- **DuckLake (SQLite catalog, probed on 1.5.6):** it records which file holds which column but still reads keys through `coalesce`, and mixing local and object-store locations needs internal-table edits. Plain files; re-check with the S3/GCS tier.

## Consequences

- A Log's day is the UTC day of its `time`; each (Project, day) has one promoted set, kept as long as Retention keeps the day.
- The Log processor counts Logs per key per Project per day (a top-K of about 1,000 keys) with each batch. A burst of junk keys (`user.<uuid>`) can fill a Project's first day; the first re-rank removes them.
- Promotion is `ALTER TABLE ADD COLUMN` on the Project's table in the same transaction as the promoted-set change, and reopens that Project's appender. Demoted columns are dropped at the export that deletes the previous day's last hour (01:00 UTC).
- Logs whose `time` is before the previous hour or at or after the next one go to a shared, MAP-only `logs_late` table, so a hot table's rows always follow their own day's set. The export writes them into their hour's folder with that day's columns: into the main file, or as `late-<n>.parquet` once the hour is exported.
- A key promoted partway through a day is read as `coalesce(column, map[key])` on that day only, so the compiler needs no footer reads and no record of each file's columns. Which files exist is recorded in a manifest ([ADR 0007](0007-log-files-listed-in-a-manifest.md)).
- `attrs_json` keys are never promoted; built-in fields don't count toward the 100.
- SQL mode's `logs` view ([ADR 0003](0003-unrestricted-sql-queries-behind-an-instance-setting.md)) is built per query for the explorer's Project and time range, with promoted columns merged back into the maps, so its Attribute filters run at map speed. This is a convenience, not a boundary: raw tables stay reachable.
- A whole number too large for a DOUBLE is stored, and must be queried, as a string. Rare keys stay in the maps; a day-long search on one streams newest first until a search index exists.
- Unmeasured until the first-milestone load test: per-Project tables, the daily switch and the checkpoint after each hourly delete at full scale.

Source: [How are logs stored and retained?](../../.scratch/logs/issues/10-storage-architecture-and-retention.md).
