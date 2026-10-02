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
- **Per-file rule:** a file promotes the keys present in at least a minimum share of its Logs, up to a per-file cap, instead of a fixed top N. Proposed: 5% and 40 (export time grew 75% from 0 to 40 promoted columns; more is unmeasured). Not yet confirmed.

Open: the 5 s day target. The first projection (9–12 s on 4 vCPU for the per-file form) scales a 25 ms query 320×, so it overstates fixed costs; a scaled run (repeated files, under 1 minute) should settle it. Benchmark runs are now limited to under 1 minute each.
