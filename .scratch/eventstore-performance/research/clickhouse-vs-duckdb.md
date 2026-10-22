# ClickHouse 25.8 against DuckDB 1.5.6 on the same Logs, 2 CPUs

Asked for by the user on 2026-10-22, while weighing ClickHouse against the current DuckDB design and the 5-minute export with an hourly merge. Run on the dev box on 2026-10-23. Scripts, data and raw outputs are in `~/.cache/solid-trace/ch-vs-duck/` (`scripts/`, `results/`).

## Setup

- **Data.** 2M synthetic Logs shaped like `tagalys` (`scripts/gen.sql`), one hour, in arrival order:
  - 70% `worker`, mostly `sidekiq.log`;
  - `sidekiq_metrics.log` 0.09%;
  - `message` about 250 bytes, with a 24-character hex jid and an md5;
  - promoted columns plus a map of two rare keys;
  - "zephyrquartz" in 1 Log in 10,000, "completed" in 30%.

  2M Logs is about 9 minutes of `tagalys`, the size of a busy hot table under a 5-minute export.
- **Machine.** Both engines pinned to CPUs 2,3 with 2.5 GB: DuckDB with `taskset` and `threads = 2`, ClickHouse in Docker with `--cpuset-cpus`. ClickHouse's system log tables were off, except `query_log`. The dev box was shared, so expect some noise.
- **DuckDB layouts.**
  - Hot: a native table in arrival order, as the hot table is.
  - File: Parquet sorted by `service, file, time`, zstd, 122,880-row groups, as the export writes it.
- **ClickHouse layout.** MergeTree `ORDER BY (service, file, time)`, LowCardinality strings, `message` in ZSTD(1). Tables were built with and without an n-gram index on `lower(message)`, plus a token index added later.
- **Ingest.** 556 commits of 3,600 Logs each, one second of `tagalys` per commit, each read from its own Parquet file. The cost of reading those files alone was measured and subtracted.
- **Queries.** Raw SQL, not SolidTrace's compiler. Times are the best of 3 after a first run, with ClickHouse's query condition cache off.

## Ingest

| | Wall | CPU, file reads subtracted | CPU a Log |
|---|---|---|---|
| DuckDB hot table | 28.5 s | 29.7 s | 15 µs |
| DuckDB export (sort to Parquet) | 4.4 s | 8.4 s | 4 µs |
| ClickHouse, no index, merges included | 14.4 s + 8 s merging | 30.3 s | 15 µs |
| ClickHouse with n-gram index, merges included | 31.4 s + 10 s merging | 68 s | 34 µs |

- **CPU:** the same for both engines without the n-gram index. The index doubles ClickHouse's cost.
- **Wall time:** ClickHouse uses both CPUs; DuckDB's appends use about one.
- **At `tagalys`'s 3,600 Logs/s:**
  - DuckDB with its export: about 7% of one CPU (two sorts, with an hourly merge, is about 8%);
  - ClickHouse: about 5% without the index, 12% with it.
- **Size on disk:**
  - DuckDB hot table: 337 MB;
  - DuckDB file: 175 MB;
  - ClickHouse: 179 MB, plus 2.9 MB of n-gram index or 7.7 MB of token index.
- **Memory:**
  - DuckDB: 0.66 GB to ingest, 2.1 GB to sort the export;
  - ClickHouse: 0.8–0.9 GB after ingest.

## Reads (ms)

| Query | DuckDB hot | DuckDB file | ClickHouse, n-gram | ClickHouse, plain |
|---|---|---|---|---|
| Newest page (200) | **13** | 113 | 296; 45 with lazy materialization to 200 | 296 |
| Newest errors (200) | **14** | 119 | 325; 52 lazy | 331 |
| Chart: `file == sidekiq_metrics.log` (0.1%) | 4 | 2 | 3 | 7 |
| Chart: worker `sidekiq.log` done, count, avg, p95 (54%) | 63 | 66 | **37** | 38 |
| Histogram, count by minute | 27 | 45 | 22 | 21 |
| Count: `message` contains rare term | 325 (1,232 first) | 579 | **150** | 253 |
| Page: `message` contains rare term | 338 | 696 | **206** | 319 |
| Count: `message` contains "completed" (30%) | 376 | 663 | 290 | **264** |
| Count by service, level | **13** | **13** | 35 | 31 |
| Chart: `shop == s500` (not in the sort key) | **6** | 21 | 20 | 21 |
| Count: map key `k123 == v7` | 83 | 58 | 45 | **44** |
| One Log's jid, substring | ~460 | ~690 | ~300 | ~300 |
| One Log's jid, `hasToken` with a token index | | | | **~65** (221k rows read) |

## What it shows

- **The unsorted hot table is not slow at this size.** At 2M Logs, a `file ==` filter takes 4 ms and the newest page 13 ms. The 13–30M-Log hot tables before the quarter-hour export were the problem, and the 5-minute export keeps a busy hot table near this size.
- **ClickHouse is faster at searching `message`:**
  - 1.5–2× DuckDB's hot table;
  - 2.5–3.5× DuckDB's Parquet file;
  - about 7–10× for a needle such as an id, through a token index and `hasToken`.

  A token index only matches whole tokens, so SolidTrace's substring `contains` can't use it. The language would need a token operator.
- **The n-gram index pruned little.**
  - A term in 1 Log in 10,000 is in about 57% of 8,192-row blocks, and the index read 58% of rows. It still cut the rare-term count from 253 ms to 150 ms.
  - A hex id defeats 3-grams: every 3-character hex combination is in every block.
  - Its cost was double the ingest CPU.
- **DuckDB is faster for the newest page and small group-bys.** ClickHouse reads every row for `ORDER BY time DESC LIMIT 200` unless lazy materialization is allowed past its default of 10 rows, and even then it took 45 ms.
- **Filtered charts and aggregations are about even.** ClickHouse is about 1.5× faster on the 54% chart.
- **DuckDB reads its Parquet file 1.5–2× slower than its own table** for `message` scans: zstd and Parquet decoding. That supports looking again at `lz4_raw` ([06](../issues/06-lz4-raw-log-files-probe.md)) for the newest files.
- **ClickHouse's query condition cache** made repeated identical filters almost free (a needle went from 300 ms to 6–14 ms). Dashboards repeat their Queries every 15 seconds, so this is a real advantage there. It was off for the table above.

## Caveats

- **The data is synthetic:** a fixed vocabulary, and random hex ids that hurt n-grams.
- **Only one hour of 2M Logs.** File times grow about linearly with Logs, so a 13M-Log hourly file would take roughly 6.5× the DuckDB file column.
- **Raw SQL.** It doesn't include SolidTrace's maps-and-coalesce, the UNION over hot, late and files, or Go's work.
- **Warm page cache, one ingest run each, a shared box.**
