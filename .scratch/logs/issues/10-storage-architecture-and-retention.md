# 10: How are logs stored and retained?

Type: grilling
Status: resolved
Blocked by: 05, 09
Map: [SolidTrace Logs](../map.md)

## Question

Given the research and the benchmark, which storage layout do logs use, where hot and cold data live, what the unit of retention is and how per-Project retention and disk budgets work at 3–5 TB/month, and how much burst headroom above the average ingest rate the design must absorb? (The engine line is already decided: DuckDB 2.0; see the map's Notes.)

Input (2026-10-02, from [What query language(s) do users write?](11-query-language.md)): whatever layout is chosen must expose a stable `logs` view (`project_id`, the seven built-in fields, the Attributes) for SQL queries, and changes to the layout must not change that view.

Input (2026-10-02, from [Benchmark the shortlisted storage layouts at reduced scale on the dev box](09-benchmark-storage-layouts.md)): the benchmark now runs about 5 minutes per layout on the dev box, so it gives rankings and settings for the hot-table layout and Parquet file shape, not proof of 3–5 TB/month; S3, DuckLake and the full-scale run are parked as map fog. If DuckLake is chosen, prefer a SQLite catalog in its own file over a DuckDB-file catalog: it lets a separate sandboxed SQL-mode process attach (see [the SQL sandboxing research](04-apl-kql-surface-and-sql-sandboxing.md)), at the cost of bundling DuckDB's `sqlite` extension.

Input (2026-10-02, from [the reduced-scale benchmark](09-benchmark-storage-layouts.md), findings in [research/09](../research/09-storage-bench-reduced.md)):
- **Hot table:** typed MAPs rank first. JSON text is the fallback if a simpler write path matters more than up to 5× faster Attribute queries and about 30% less disk.
- **VARIANT:** not on the 2.0 alpha. Re-check on 2.0.0.
- **Cold tier:** Parquet from the MAP layout, 30k-row row groups. Async read-ahead buys nothing on local disk.
- **Day-long Attribute searches:** need time-bounded defaults or pruning beyond row-group stats (projected 2–4 min per day on 2 cores).
- **Disk:** about 28–30 GB/day of Parquet and 38 GB/day of MAP hot table at 1.9 MB/s.
- **Bursts:** automatic checkpoints stall writes for 2–3 s at 5× bursts, so plan headroom or buffering ahead of DuckDB.

## Answer

Resolved again 2026-10-02 (grilling, after [ticket 14](14-benchmark-storage-design-on-1-5-5.md) reopened it). [ADR 0005](../../../docs/adr/0005-logs-per-project-hot-tables-promote-at-ingest.md) supersedes [ADR 0004](../../../docs/adr/0004-logs-hot-table-and-hourly-parquet.md). Glossary: **Retention** added to `CONTEXT.md`; **Attribute** amended for large whole numbers; **SQL query** amended for the scoped `logs` view. The first resolution's answer is in this file's history (`f604ab4`); the comments below record each decision of the second pass.

**Engine**
- DuckDB 1.5.6 (duckdb-go `v2.10506.0`, bundled), moved in [Move EventStore back to DuckDB 1.5.5](../../upgrade-pebble-duckdb/issues/02-duckdb-2-0-0-release.md). The design uses only 1.5 features; 2.0 and a re-check of VARIANT are v2 fog.

**Layout**
- Built-in fields are columns. Attributes go in four typed MAP columns: `attrs_string`, `attrs_number` (DOUBLE), `attrs_bool`, and `attrs_json` (arrays, and nesting past depth 10, as JSON text).
- A whole number that a DOUBLE cannot hold exactly is stored as a string in `attrs_string`. So `id == 9007199254740993` does not find it; `id == "9007199254740993"` does.
- MAP over JSON text: up to 5× faster Attribute filters and group-bys on 2.0, 2–3× on 1.5.6, smaller on disk, and typed maps match the non-coercing `==` of [the query language](11-query-language.md).

**Promotion**
- A Project's (type, key) pairs are promoted at ingest to typed columns named after the key (`"attrs_string.http.status"`), in the hot table and in Parquet, until it has **100**. Past that, new keys stay in the maps. `attrs_json` keys are never promoted; built-in fields don't count.
- **Popularity** is the number of Logs carrying a key per Project per UTC day, counted by the Log processor (a top-K of about 1,000 keys per Project per day) and stored in `logs.duckdb` with each batch.
- **Daily re-rank:** the first Log of a new day in a Project sets that day's set: yesterday's unchanged if it was not full, else the top 100 from the most recent counted day. Keys that drop out free room for promotion at ingest.
- A Log's day is the UTC day of its `time`. Each (Project, day) has one promoted set, kept as long as Retention keeps the day.
- The **promoted-set table** (Project, day, type, key, time added) lives in `logs.duckdb`, changes in the same transaction as the `ALTER TABLE`s, and is cached in memory. It is the only place the query compiler learns columns from.

**Where Logs live**
- **Ingest:** the handler parses and normalises a request's Logs (the JSON route reports dropped and truncated counts), stores the batch as one Pebble entry, then returns `200`. A Log processor appends batches to DuckDB from a per-Project processing cursor and deletes each Pebble batch once DuckDB has committed it.
- **Hot tables:** one per Project in `logs.duckdb`, created with the Project's first Log, holding the current and previous hour. `logs.duckdb` is attached to the same DuckDB instance as `events.duckdb`, with its own WAL and checkpoints. Promotion is `ALTER TABLE ADD COLUMN` and reopens that Project's appender; demoted columns are dropped at the export that deletes the previous day's last hour (01:00 UTC), in the same transaction.
- **`logs_late`:** one shared, MAP-only table for Logs whose `time` is before the previous hour or at or after the next one. A hot table's rows therefore always hold their keys per their own day's set.
- **Parquet:** every closed hour is exported to zstd Parquet on local disk, one file per Project per hour under `project=<id>/day=<d>/hour=<h>/`, 30k-row row groups, sorted by time, with the day's promoted columns; then deleted from the hot table. Late rows join their hour's file if it is not yet exported, else become `late-<n>.parquet` in its folder, normalised to the day's columns.
- **Reads:** the compiler reads one branch per day: the column for promoted keys, `coalesce(column, map[key])` for keys added during that day (that day only), maps only for `logs_late`. No footer reads, no per-file records.
- **SQL mode's `logs` view** is built per query, as a temporary view, for the explorer's Project and time range: the Project's hot table, its days in Parquet and its `logs_late` rows, with promoted columns merged back into the maps. This changes [ticket 11](11-query-language.md) and [ticket 12](12-query-builder-ux.md) (the Project and time range now apply to SQL mode); [ADR 0003](../../../docs/adr/0003-unrestricted-sql-queries-behind-an-instance-setting.md) still holds, as this is a convenience, not a boundary.
- **Who runs it:** a timer inside EventStore runs the hourly export, retention and the disk-limit check. No Rails job.
- **Not in v1:** S3/GCS (v2 fog), DuckLake (probed with a SQLite catalog in ticket 14; re-check with the S3/GCS tier).

**Retention and disk**
- **Retention** is in whole days: an instance default (30) and an optional per-Project override. Both are Console settings (an instance setting and `projects.log_retention_days`); EventStore reads them from Rails' SQLite the way it reads `project_keys`, so logs add no Go writes to Rails tables.
- Retention deletes a Project's whole `day=` folders and that day's promoted-set and count rows. Deleting a Project drops its hot table, its folders and its rows.
- One instance-wide disk limit for Logs: when Logs go over it, the oldest files are deleted first, from any Project.
- Expected disk at 1.9 MB/s: Parquet about 28–30 GB/day (about 0.9 TB for 30 days); hot tables a few GB; Pebble normally seconds of Logs, up to about 6 GB after a 10-minute 5× burst.

**Bursts**
- EventStore absorbs 5× the average rate for 10 minutes with no 503 and no lost Logs, by buffering in Pebble; checkpoint stalls of 3–4 s (seen on 1.5.6) are absorbed the same way. Search can lag behind ingest during a burst. Above that, the shared 503 + `Retry-After` gate applies.

**Query speed** (target VM: 4 vCPU, 16 GB)
- "Latest N" for any filter and range shows first results within 2 s: the compiler scans newest first, hour by hour, and stops early.
- A filter or group-by on a promoted key over a day finishes within 5 s. Ticket 14 projects 4–7 s for a Project with the instance's whole volume once the hot table promotes too.
- A day-long search on a rare key streams matches newest first and can take a minute or more; a per-query timeout stops it. A search index is v2 fog.

**Events** (asked by [ticket 05](05-prod-readiness-gaps-for-logs.md))
- The mechanism is reusable: the timer, folder deletion, and Pebble range deletes. Doing it for Events stays a prod-readiness gap (§9).

**Verification**
- Not measured: per-Project hot tables, the daily switch, and the checkpoint after each hourly delete at full scale. These go to the first-milestone load test on the target VM (map fog), not to a prerequisite of the spec.

## Comments

### 2026-10-02 reopened by ticket 14

[Benchmark the chosen storage design on DuckDB 1.5.5](14-benchmark-storage-design-on-1-5-5.md) found that promoted columns miss the 5 s day target, because the hot table has none ([research/14](../research/14-storage-design-bench.md)). Decided with the user while working ticket 14:
- Keys are promoted at ingest, in the hot table and in Parquet, up to **100 per Project**; past that, new keys stay in the maps.
- Once the limit is hit, a nightly job re-ranks: the next day's promoted set is the Project's 100 most popular keys, the rest go back to the maps. One promoted set per Project per day.
- The promoted set is a table in `logs.duckdb`, changed in the same transaction as the hot table's `ALTER TABLE` and cached in memory by EventStore.
- Plain Parquet files, no DuckLake.
- EventStore runs DuckDB 1.5.6, not 1.5.5.

To settle here, then write an ADR that supersedes [ADR 0004](../../../docs/adr/0004-logs-hot-table-and-hourly-parquet.md):
- How popularity is counted (proposed: the Log processor counts keys at ingest).
- One hot table per Project, or one shared table (whose columns would be every Project's keys).
- The midnight switch: when demoted columns leave the hot table, and how the compiler reads the hour that still holds newly promoted keys in the maps.
- How the query compiler learns each day's columns (proposed: from the promoted-set table, without reading footers).
- Whether the hourly export, which now writes a day's fixed schema, still needs anything per file.

### 2026-10-02 grilling (second pass)

- **One hot table per Project**, not one shared table. Reasoned, not benchmarked: for a high-volume Project both shapes perform the same; per-Project tables keep a small Project's newest-hours queries and "latest N" from scanning every Project's rows, and write only that Project's columns. A shared table with key-named columns was ruled out (every Project's keys together pass the ingest limit).
- **Columns are named after keys** (`"attrs_string.http.status"`), not fixed typed slots: readable and only the columns a Project uses. Promotion is `ALTER TABLE ADD COLUMN` on that Project's table, which reopens its appender.
- **Popularity:** the number of Logs carrying a key per (Project, UTC day), counted by the Log processor in a top-K of about 1,000 entries per Project per day and stored in `logs.duckdb` with each batch. Query use is not counted.
- **A Log's day** is the UTC day of its `time`; a late Log uses its own day's promoted set. Sets are kept as long as Retention keeps the day.
- **Keys promoted partway through a day** are read as `coalesce(column, map[key])` on that day only. Rule: promote new keys at ingest while the set has room; each night, if the set was full, re-rank.
- **Table lifecycle:** a Project's hot table is created with its first Log; deleting a Project drops its table, its `project=` folders and its promoted-set rows.
- **Logs outside the hot window** (a `time` before the previous hour, or at or after the next hour) go to one shared `logs_late` table, maps only. Each hourly export writes them into their hour's folder, normalised to that day's promoted set: into the main file if the hour is not yet exported, else as an extra `late-<n>.parquet`. So a Project's hot table holds every row's keys exactly per its own day's set.
- **Re-rank trigger:** lazily, when the Log processor first sees a Log for a new day in a Project, before appending, in one transaction with the `ALTER TABLE`s and the promoted-set change. The new day's set: yesterday's unchanged if it was not full; else the top 100 from the most recent day with counts.
- **SQL mode's `logs` view is scoped to the bar's Project** (user's choice over one view across all Projects). This changes [ticket 11](11-query-language.md) ("the explorer's Project selector does not apply") and [ticket 12](12-query-builder-ux.md) ("the Project and time range do not apply"). It is a convenience, not a boundary: ADR 0003 still holds, and raw tables stay reachable.
- **SQL mode's `logs` view is also scoped to the bar's time range.** EventStore builds it per query as a temporary view: the Project's hot table, its days in Parquet and its `logs_late` rows, each with promoted columns merged back into the maps.
- **Dropping demoted columns:** at the export that deletes the previous day's last hour (01:00 UTC), in the same transaction, then the appender is reopened. New columns are added at the lazy re-rank.
- **The 100 counts (type, key) pairs.** `attrs_json` keys are never promoted; built-in fields don't count.
- **The compiler learns columns only from the promoted-set table** (cached in memory; rows of Project, day, type, key, time added). It reads one branch per day: the column for promoted keys, `coalesce(column, map[key])` for keys added that day, maps only for `logs_late`. No footer reads, no per-file records. An hour folder holds `data.parquet` plus any `late-<n>.parquet`.
