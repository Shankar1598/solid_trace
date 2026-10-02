# 14: Benchmark the chosen storage design on DuckDB 1.5.5

Type: task
Status: claimed
Blocked by: 10
Waits on (outside this map): [Move EventStore back to DuckDB 1.5.5](../../upgrade-pebble-duckdb/issues/02-duckdb-2-0-0-release.md)
Map: [SolidTrace Logs](../map.md)

## Question

Does the storage design from [How are logs stored and retained?](10-storage-architecture-and-retention.md) meet its targets on DuckDB 1.5.5? Extend the `logsbench` harness (`event_store/bench/logslayouts`, branch `feature/logging`) and run it at reduced scale on the dev box, as [ticket 09](09-benchmark-storage-layouts.md) did: a few minutes per run, about 10 minutes in all. Findings go in `research/14-…` on `feature/logging`, with a pointer from this ticket.

1. **MAP vs JSON text on 1.5.5**, with the four-map layout (`attrs_string`, `attrs_number`, `attrs_bool`, `attrs_json`). Do the 2–3 s checkpoint stalls and the heap-corruption crash seen on the 2.0 alpha also happen on 1.5.5?
2. **Promoted columns:** hourly Parquet with each file's top N keys as typed columns vs MAP-only Parquet. Project to a day on 4 vCPU against the targets: "latest N" first results within 2 s, common-key filter and group-by over a day within 5 s. Recommend N.
3. **Hot-table plateau:** over many insert → export → delete cycles, does `logs.duckdb` stop growing?
4. **Hourly export time** for an hour of Logs, and its effect on ingest running at the same time.

Resolved when each point has a measured answer. If promoted columns miss the 5 s target, reopen ticket 10.

## Comments

### 2026-10-02 first run and decisions so far

Runs on DuckDB **1.5.6** (`v2.10506.0`, the newest 1.5.x tag; see [Move EventStore back to DuckDB 1.5.5](../../upgrade-pebble-duckdb/issues/02-duckdb-2-0-0-release.md)). Harness extended on `feature/logging`; raw results in [research/14-bench](../research/14-bench/). The findings doc is not written yet.

Decided with the user:
- **Plain Parquet files, no DuckLake** (as ADR 0004). `TestDuckLakeProbe` (SQLite catalog) showed DuckLake records which file holds which promoted column, but reads the table as one, so keys still go through the slower `coalesce(column, map[key])`; moving every file to a new location is easy (copy, change one `data_path` row), mixing locations is not supported. DuckLake's catalog pruning is worth a re-check with the v2 S3/GCS tier. Output: [ducklake-probe.log](../research/14-bench/ducklake-probe.log).
- **Promotion limit:** at most 1000 distinct promoted keys across a Project's files; past it, new keys stay in the maps.
- **Per-file rule:** a file promotes every key present in at least 5% of its Logs, with no per-file cap, instead of a fixed top N. The synthetic data gives 46–47 promoted keys per file. Export time grew 75% from 0 to 40 promoted columns.
- Benchmark runs are limited to under 1 minute each.

### 2026-10-02 scaled run: the 5 s day target

`TestScaledQueries` ([scale.log](../research/14-bench/scale.log), 55 s) timed the full scans at two sizes to separate fixed cost from per-row cost: a per-file Parquet file of Project 1 read 2 and 8 times (2.2M and 8.9M rows; that run capped promotion at 40 keys, which covered the queried keys), and the hot table at 0.4M and 1.6M rows. Projected for a Project that has the instance's whole volume, a day being 22 hours of Parquet (253M Logs) plus the 2 hours the hot table holds (23M Logs), on 4 vCPU using the 1→2-thread speedup measured on this box (1.07–1.35):

| Query | Parquet, 22 h | Hot table, 2 h | Day |
|---|---|---|---|
| Common-key per-minute histogram | 6.6 s | 9.3 s | ~16 s |
| Group-by on a common key | 4.1 s | 5.5 s | ~10 s |
| Count by service per minute (built-in columns) | 7.3 s | 0.4 s | ~8 s |
| Rare key, latest 100 (scans the maps) | 18 s | 14 s | streams |

- **The 5 s target is missed**, mainly because of the hot table: its 2 hours of MAPs cost more than the 22 hours of promoted Parquet. Parquet alone is at or just over 5 s, and under it if 4 vCPU scale better than this shared 2-CPU box did.
- The ticket says to reopen [How are logs stored and retained?](10-storage-architecture-and-retention.md) when promoted columns miss 5 s. Levers: export more often (a hot table of minutes, not hours), promote columns in the hot table too, or relax the target for day-long scans. Waiting on the user.

### 2026-10-02 promotion redesigned (replaces the per-file rules above)

Probes, each under a minute: [hot-promotion.log](../research/14-bench/hot-promotion.log), [wide-columns.log](../research/14-bench/wide-columns.log), [wide-column-limits.log](../research/14-bench/wide-column-limits.log).

- Promoted columns help the hot table as much as Parquet: common-key scans 12–15× faster with 45 keys promoted. `ADD COLUMN` takes ~3 ms but invalidates open appenders.
- Promoting every key (1000 columns) makes scans 6–15× faster but ingest 11× slower (2.6k Logs/s with the appender, below the 3.2k target) and the hourly export 15× slower. 100 keys cost almost nothing: appender 28k vs 31k Logs/s, export 1.9× MAP-only, files 20% smaller. 250 keys: appender 12.8k Logs/s, export 3.9×.

Decided with the user:
- **Promotion at ingest, 100 keys per Project.** Keys are promoted as they first appear, in the hot table and in Parquet, until the Project has 100. Past that, new keys stay in the maps.
- **Nightly re-ranking once the limit is hit:** the next day's promoted set is the Project's 100 most popular keys (Logs carrying them that day); the rest go back to the maps. One promoted set per Project per day, so a day's files share a schema.
- **The promoted set is a table in `logs.duckdb`** (Project, day, map, key), changed in the same DuckDB transaction as the hot table's `ALTER TABLE`, and cached in memory by EventStore. Not SQLite: SQLite is faster per lookup and per commit, but neither is on the hot path, and a second database cannot commit together with the hot table.
- Plain Parquet files, no DuckLake (unchanged).

Proposed, not yet decided: popularity counted by the Log processor at ingest; one hot table per Project; demoted columns dropped from the hot table once the day's last hour is exported. These replace parts of ADR 0004, so [How are logs stored and retained?](10-storage-architecture-and-retention.md) needs reopening and a new ADR.
