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
