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

## On a production quarter-hour (2026-10-23)

With the user's OK, `tagalys`'s `day=2026-10-22/hour=21/data-15.parquet` was copied off production and deleted afterwards. That is 3.16M Logs, 108 columns and 133 MB, as production stores them. Scripts, timings and the compiled SQL (not the Logs) are in `~/.cache/solid-trace/prod-quarter/`.

- **Reads.**
  - The 14 slowest MCP Queries of the last 7 days, and 16 of Dashboard 1's 18 Charts. Charts 5 and 6 read only the last 70 seconds.
  - Each was run through MCP over that file's 15 minutes, so production's EventStore logged its compiled SQL (`log_read_sql`).
- **Four ways of running them**, each on CPUs 2,3 with 3GB:
  - DuckDB running production's own SQL: the hour's other files dropped, the hot table and `logs_late` empty.
  - DuckDB running hand-written SQL with the same meaning.
  - ClickHouse running the same hand-written SQL, on a MergeTree with the file's 108 columns sorted by `service, file, time`, and a per-minute count view.
  - ClickHouse again, with its query condition cache on.

**CPU for one pass, warm** (`cpu.txt`; DuckDB's figures include about 0.05 s of process start):

| | DuckDB, production SQL | DuckDB, hand SQL | ClickHouse | ClickHouse, repeated, with its cache |
|---|---|---|---|---|
| 14 slow reads | 15.8 s | 15.0 s | 7.5 s | 1.0 s |
| Dashboard 1 refresh (16 Charts) | 0.61 s | 0.41 s | 0.20 s | 0.22 s |

**Times, best of 3 (ms):**

| Read | DuckDB, production SQL | DuckDB, hand SQL | ClickHouse | ClickHouse, cache |
|---|---|---|---|---|
| Newest page | 72 (3 Logs, two steps) | 165 (200) | 250 (200, lazy) | 243 |
| region + file + `message` startswith | 780 | 844 | **4** | 2 |
| `message` contains "lounge" | 917 | 877 | 374 | 65 |
| `search` "burton-na-staging" | 1,492 | 1,508 | 996 | 10 |
| `search` "lounge" | 1,588 | 1,475 | 1,025 | 95 |
| `search` "timeout", page | 1,447 | 2,074 | 1,087 | 47 |
| `tostring(tags.store_id) in (12 ids)` | 856 | **70** | 33 | 16 |
| region, service, level, not `message` contains | 402 | 374 | 79 | 85 |
| `file` contains "webhook" | 227 | **20** | 20 | 19 |
| `tags.host` contains "lounge" | 177 | **21** | 9 | 4 |
| Count by minute | 87 | 80 | 43; **2** from the view | |
| A Dashboard 1 Chart (each of 16) | 20–27 | 12–30 | 4–9 | 4–8 |

**What this adds to the synthetic run:**

- **On real data, ClickHouse used about half DuckDB's CPU for the slow reads and a half to a third for a Dashboard refresh.**
- **With its query condition cache, a repeated read used a seventh as much CPU, or less.** That covers a Dashboard's refreshes and every Monitor run. The refresh above gained nothing, because filtering on `file` already skips all but a few blocks.
- **Filters that combine sort-order columns with OR are where DuckDB falls furthest behind** (780 ms against 4 ms). ClickHouse's primary key still finds the few blocks; DuckDB reads the file's row groups and lowercases `message` across them.
- **DuckDB still wins the newest page.** On ClickHouse it needs lazy materialization allowed up to 200 rows.
- **The Query compiler's SQL costs up to 12× on some reads, whatever the engine:**
  - `tostring()` compares a coalesce of four typed values once per `in` value (856 against 70 ms);
  - Charts group by a struct of four typed values;
  - `contains` on a promoted string also checks the maps (227 against 20 ms, 177 against 21 ms).

  These are fixable in the compiler now.
- **Production's own times for the same SQL were 1.1–19× the local DuckDB times.** Up to 7 of these reads ran at once there, beside ingest, Rails and Vector, on arm64. So they aren't like-for-like.

**Not covered:**
- many reads at once;
- a 6-hour range, where DuckDB's bin cache serves exported hours' bins for time Charts;
- ClickHouse's recent data split across more parts while ingesting (this table was loaded in bulk and merged to 5 parts);
- its JSON type in place of promoted columns and maps.

## Caveats (the synthetic run)

- **The data is synthetic:** a fixed vocabulary, and random hex ids that hurt n-grams.
- **Only one hour of 2M Logs.** File times grow about linearly with Logs, so a 13M-Log hourly file would take roughly 6.5× the DuckDB file column.
- **Raw SQL.** It doesn't include SolidTrace's maps-and-coalesce, the UNION over hot, late and files, or Go's work.
- **Warm page cache, one ingest run each, a shared box.**
