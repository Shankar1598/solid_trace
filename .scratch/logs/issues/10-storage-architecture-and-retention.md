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

Resolved 2026-10-02 (grilling). [ADR 0004](../../../docs/adr/0004-logs-hot-table-and-hourly-parquet.md). Glossary: **Retention** added to `CONTEXT.md`; **Attribute** amended for large whole numbers.

**Engine**
- EventStore goes back to the released DuckDB 1.5.5 (duckdb-go `v2.10505.0`, bundled static library) now. DuckDB 2.0 becomes a later upgrade, not a dependency: the design below uses only features 1.5 already has. The move is [Move EventStore back to DuckDB 1.5.5](../../upgrade-pebble-duckdb/issues/02-duckdb-2-0-0-release.md).
- No VARIANT. Re-checking it on 2.0 is v2 fog.

**Layout**
- Built-in fields are columns. Attributes go in four typed MAP columns: `attrs_string`, `attrs_number` (DOUBLE), `attrs_bool`, and `attrs_json` (arrays, and nesting past depth 10, as JSON text).
- A whole number that a DOUBLE cannot hold exactly is stored as a string in `attrs_string`. So `id == 9007199254740993` does not find it; `id == "9007199254740993"` does.
- MAP over JSON text because filtering and grouping by Attributes is the explorer's main work, where MAP was up to 5× faster, and typed maps match the non-coercing `==` of [the query language](11-query-language.md). JSON's faster writes are not needed: MAP wrote about 6× the target rate on 2 cores.

**Where Logs live**
- **Ingest:** the handler parses and normalises a request's Logs (the JSON route reports dropped and truncated counts), stores the batch as one Pebble entry, then returns `200`. A Log processor appends batches to DuckDB from a per-Project processing cursor, as Event processing does, and deletes each Pebble batch once DuckDB has committed it.
- **Hot table:** in its own `logs.duckdb`, attached to the same DuckDB instance as `events.duckdb`. It has its own WAL and checkpoints, so log checkpoints do not stall Event writes; the instance memory limit is shared. It holds the current and previous hour. Freed space is reused, so the file should level off at about 3–4 GB at 1.9 MB/s (to be checked).
- **Parquet:** every closed hour is exported to zstd Parquet on local disk, one file per Project per hour under `project=<id>/day=<d>/hour=<h>/`, 30k-row row groups, sorted by time, then deleted from the hot table. Each file's top N keys are promoted to typed columns of their own; the rest stay in the MAPs. N is set by the verification task. Files with different columns are read with `union_by_name`, and the query compiler reads `coalesce(column, map[key])`.
- **Late Logs** (a `time` in an hour already exported) wait in the hot table and become an extra file in that hour's folder at the next export. [Ticket 07](07-log-domain-model.md)'s replacement of timestamps older than retention keeps Logs out of deleted folders.
- **`logs` view:** hot table `UNION ALL` the Parquet files, stable as [ADR 0003](../../../docs/adr/0003-unrestricted-sql-queries-behind-an-instance-setting.md) requires.
- **Who runs it:** a timer inside EventStore runs the hourly export, retention and the disk-limit check. No Rails job.
- **Not in v1:** S3/GCS (v2 fog; local disk only), DuckLake (hourly files need no catalog).

**Retention and disk**
- **Retention** is in whole days: an instance default (30) and an optional per-Project override. Both are Console settings (an instance setting and `projects.log_retention_days`). EventStore reads them from Rails' SQLite the way it reads `project_keys`, so logs still add no Go writes to Rails tables.
- Retention deletes a Project's whole `day=` folders.
- One instance-wide disk limit for Logs: when Logs go over it, the oldest files are deleted first, from any Project.
- Expected disk at 1.9 MB/s: Parquet about 28–30 GB/day (about 0.9 TB for 30 days); hot table about 3–4 GB; Pebble normally seconds of Logs, up to about 6 GB after a 10-minute 5× burst.

**Bursts**
- EventStore absorbs 5× the average rate for 10 minutes with no 503 and no lost Logs, by buffering in Pebble. Search can lag behind ingest during a burst. Above that, the shared 503 + `Retry-After` gate applies.

**Query speed** (target VM decided here: 4 vCPU, 16 GB)
- "Latest N" for any filter and range shows first results within 2 s: the compiler scans newest first, hour by hour, and stops early.
- A filter or group-by on a common (promoted) key over a day finishes within 5 s.
- A day-long search on a rare key streams matches newest first and can take a minute or more; a per-query timeout stops it. A search index is v2 fog.
- Rejected: daily rotation (Events' pattern). It would keep the last 24 hours in the hot table, where promoted columns cannot exist, leaving common-key day searches at about 20 s.

**Events** (asked by [ticket 05](05-prod-readiness-gaps-for-logs.md))
- The mechanism is reusable: the timer, folder deletion, and Pebble range deletes. Instance-wide Event retention could delete `event_date=` folders and Pebble Events by age; per-Project Event retention first needs the Event export to write `project=` folders. Doing it stays a prod-readiness gap (§9).

**Verification**
- [Benchmark the chosen storage design on DuckDB 1.5.5](14-benchmark-storage-design-on-1-5-5.md) (task, AFK) checks these targets and blocks the v1 spec. If promoted columns miss the 5 s target, this ticket reopens.
- The full-scale benchmark is not a prerequisite for the spec. It becomes a first-milestone load test of this design on the target VM.
