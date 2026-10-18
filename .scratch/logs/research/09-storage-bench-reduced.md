# 09: Storage-layout benchmark at reduced scale (dev box)

Ticket: [logs 09](../issues/09-benchmark-storage-layouts.md). Run 2026-10-02.

- **Harness:** `event_store/bench/logslayouts` on branch `feature/logging`.
- **Raw results:** `09-bench/`.
  - Quick run, all layouts: `report.md`, `*.json`, `*.log`.
  - First 5-minute run, JSON and MAP only: `09-bench/first-run-5min/`.

## Setup

- **Engine:** DuckDB `v2.0.0-alpha43385`, linked as EventStore does (`-tags=duckdb_use_lib`).
- **Machine:** dev box, 2 CPU, 12 GB RAM, shared with idle sessions.
- **Limits:**
  - Each layout ran in its own process under `systemd-run --user --scope -p MemoryMax=3G`.
  - DuckDB `memory_limit=2GB`, `threads=2`.
- **Data:** synthetic Logs, a pure function of (seed, id):
  - about 570 B each, messages 50–500 B;
  - 6 services, six levels weighted toward info, 2% with a trace id;
  - 40 common keys plus a Zipf tail of 2,000 keys, 5% of keys mixed-type.
  - Logs are stamped on a logical clock at the target rate (3,200 Logs/s, 1.9 MB/s), so 1M Logs is about 5 logical minutes.
- **Layouts:**
  - `json`: Attributes as JSON text in a VARCHAR column.
  - `variant`: `text::JSON::VARIANT` through a temp staging table, because the Go appender cannot write VARIANT.
  - `variant_noshred`: the same with `variant_minimum_shredding_size=-1`. Added to separate VARIANT from its shredding.
  - `map`: typed MAPs, one each for string, number and bool. Integers go in the DOUBLE map.
  - Each was then written to hourly Parquet (zstd) and read from local disk.
- **Run shape:** cut to about 2 minutes per layout, at the user's request (5–10 minutes for the whole benchmark):
  - 30 s unthrottled;
  - 30 s at target rate, with the query set looping concurrently in the second 15 s;
  - 15 s at 5×.

  Then the query set, cold (fresh DuckDB, files evicted with `fadvise`) and warm (median of 3). Each query set has a 30 s budget, and queries left when it runs out are skipped.
- **Earlier run:** a first 5-minute run (2 min, 2 min, 1 min) of JSON and MAP gave 3.5–4.8M rows. Its numbers agree with the quick run and are cited where scale matters.
- **Correctness:** every layout and its Parquet matched the JSON baseline row for row (top 100, plus a count and an id checksum over all matches) for Logs 0..50,000. The unit tests also check the baseline against answers recomputed in Go from the generator.

## Ranking

| | json | map | variant_noshred | variant (shredded) |
|---|---|---|---|---|
| Unthrottled write, Logs/s | **32.8k** (18.6 MB/s) | 20.3k (11.5 MB/s) | 25.9k (14.7 MB/s) | 985 (0.56 MB/s), one flush stalled 40.6 s |
| Kept up at 1.9 MB/s | yes | yes (one 1.1 s flush while queries ran) | yes | yes, for 30 s |
| 5× burst, average achieved | 99% | 89% | 83% | crashed (see below) |
| Hot table B/Log (raw ≈ 570) | 205 | **139** | 235 | – |
| Parquet B/Log | 107 | **101** | 111 | – |
| Attribute queries, hot, warm | baseline | **1.1–5× faster** | mixed: 1.5–2.6× slower on common-key equality and range, 1.5–1.8× faster on rare-key equality and group-by | about 100× slower (smoke test) |
| Peak RSS | 0.87 GB (1.6 GB in the 5-minute run) | **0.72 GB** (1.1 GB) | 1.43 GB | 1.8 GB |

**Ranking: MAP, then JSON, then VARIANT without shredding.** Shredded VARIANT is not usable on this alpha.
- **MAP:** fastest attribute queries, smallest on disk, least memory. Its write rate, the lowest of the three, is still more than 6× the target.
- **JSON:** writes fastest. Its rare-key filter, range and group-by are 2.5–5× slower than MAP.
- **VARIANT without shredding:** close to JSON on queries (faster on some, slower on others), but uses more memory and the most disk.

Key hot-table queries (warm ms, quick run, about 1M rows):

| Query | json | map | variant_noshred |
|---|---|---|---|
| common Attribute equality (latest 100) | 37 | 33 | 55 |
| rare Attribute equality (latest 100) | 533 | **105** | 359 |
| numeric range on a mixed-type key | 45 | **18** | 116 |
| group by an Attribute, top 10 (full scan) | 652 | **145** | 356 |
| count by service per minute | 36 | 29 | 32 |
| substring in `message` | 15 | 25 | 28 |
| fetch one Log by id | 4.9 | 6.0 | 4.2 |

All tables are in `09-bench/report.md`.

## VARIANT

- **Write cost of `text::JSON::VARIANT`:** small until a checkpoint. Unshredded, it wrote 25.9k Logs/s against 32.8k for JSON (-21%).
- **Shredding happens per row group at checkpoint, and only for row groups of 30k+ rows** (`variant_minimum_shredding_size`).
  - Small flushes don't matter. Separate 5k-row commits pile into the same row group, which is shredded once it reaches 30k at a checkpoint. A 5k-row group is shredded later, once it has grown.
  - Detectable per row group in `pragma_storage_info`.
- **With a 2,000-key long tail, shredding is ruinous on this alpha:**
  - About 1,500 column paths per row group.
  - Checkpoint: 31.6 s for 43k rows. A 1 GB memory limit ran out of memory at 48k rows.
  - Attribute filters: 4–6 s per 43k rows, against 15–25 ms for MAP.
  - Unthrottled ingest: 985 Logs/s, because automatic checkpoints stall commits (a 40.6 s flush).
  - In the quick run, the 5× burst phase **crashed the process with native heap corruption**: `corrupted double-linked list`, SIGABRT inside `duckdb_execute_pending` on the staging `COMMIT`. This happened once. It was not re-run.
- **Pruning on shredded fields:** not answerable, because shredded queries never got fast enough to measure.
- **Parquet written from VARIANT:**
  - **Default shredding** writes a column per key seen, about 2,960 for 30k rows. On this alpha that is slow (12.8 s for 30k rows) and runs out of memory at the default row-group size. A filter like `variant_extract(attrs, k)::VARCHAR = …` on it raises an **INTERNAL error that invalidates the database** (`Calling a VariantStats method on BaseStatistics … STRING_STATS`). `variant_extract_string`, or disabling `filter_pushdown`, avoids the crash. `TestDefaultVariantParquet` reproduces this; run it with `LOGSBENCH_PROBE=1`.
  - **Explicit `SHREDDING {attrs: 'STRUCT(<40 common keys>)'}`** is stable: 89 leaf columns. Even so, exporting is 4.5× slower than JSON (24 s against 5.3 s for about 1M rows), and attribute filters on it are 3–7× slower than JSON-text Parquet. **Shredding does not pay off here even when limited to the common keys.**

## Parquet on local disk

- **Parquet vs the hot table:** "latest 100 matches" filters on a common value run 25–30× slower on Parquet: about 1.0–1.3 s for JSON and 0.5–0.8 s for MAP per 1M rows, against 33–45 ms in the hot table. Rare-value filters and full scans are 1.3–8× slower: group-by is 0.87 s against 0.65 s for JSON. The most likely reason is that the native table's top-N dynamic filter on `time` skips older row groups and the Parquet scan doesn't. That is inferred from latency, not confirmed with EXPLAIN.
- **Async I/O on vs off (`read_ahead_depth = 0`):** no consistent difference on local disk at this size, in either the quick run or the 5-minute run. Async on uses about 50–70 MB more peak RSS. Its benefit is expected on S3, which is parked.
- **Row-group size, from the 5-minute run (40 vs 157 row groups):**
  - 30,720 rows per group made `fetch_by_id` 2–3× faster cold (80 vs 255 ms), with less RSS while querying (249 vs 493 MB for JSON).
  - Scans were otherwise within noise. Size was the same.
- **Row groups per file and parallelism across files:** not measured. At this scale every run fits in one hourly file.
- **Are Parquet writes asynchronous?** Not observable from the harness. The engine exposes `async_threads=8` and `read_ahead_depth=-1` (automatic), and those were left at their defaults.
- **Are native `.duckdb` reads asynchronous?** `storage_block_prefetch` defaults to `REMOTE_ONLY`, so there is no local block prefetch. Forcing `ALWAYS_PREFETCH` made cold hot-table queries no faster and often slower: JSON group-by 1,088 vs 878 ms; VARIANT range 980 vs 168 ms.

## Ingest and queries in one process

- **At target rate, with the query set looping on the hot table:**
  - Ingest kept up: max backlog under 0.2 s of Logs. MAP had a single 1.1 s flush.
  - Queries took 25–32 ms at p50 and 0.2–0.6 s at most.
  - CPU reached 176% of 2 cores, and RSS stayed under 0.85 GB.
  - No starvation with `threads=2`. No settings sweep was done.
- **At 5×:** none of the layouts stayed within one second. Automatic checkpoints cause 2–3 s flush stalls, and in the 5-minute run the JSON backlog peaked at 79k Logs (about 5 s). On average JSON still achieved 96–99% of 5×.
- **At target rate without queries:** in the 5-minute run each layout had one 2.2–2.7 s stall, most likely an automatic checkpoint. So "kept up" in the 5-minute run shows `no` for that reason alone.
- **Peak RSS:** never above 1.8 GB, against the 3 GB cap.

## Projected full day (276M Logs), not measured

Warm latency scaled linearly by rows, from the quick run. Treat these as order-of-magnitude only.

| | json hot | map hot | json Parquet | map Parquet |
|---|---|---|---|---|
| count by service per minute | 7 s | 8 s | 13 s | 13 s |
| group by an Attribute, top 10 | 134 s | 41 s | 177 s | 143 s |
| Attribute filter (full scan in Parquet) | – | – | 210–245 s | 140–230 s |

- **Hot-table "latest 100" filters:** these don't scale with total rows. They stop once they have 100 matches, so they stay in the tens of ms for common values. Rare values scan further back: rare-key equality projects to 29 s on MAP and 109 s on JSON for a full day.
- **A full-day search on 2 cores:** an Attribute search or group-by over a whole day of Parquet would take 2–4 minutes. The design needs time-bounded defaults, or a pruning aid (per-file key/bloom metadata), for day-long searches.
- **Disk at target rate:**
  - Hot table: about 38 GB/day for MAP, 57 GB/day for JSON.
  - Parquet: about 28–30 GB/day, roughly 0.85–0.9 TB/month at 1.9 MB/s.

## Inputs for ticket 10 (storage architecture)

1. **Hot table: typed MAPs.** JSON text is the fallback if the simpler write path matters more than up to 5× faster Attribute queries and about 30% less disk.
2. **Don't use VARIANT on the 2.0 alpha,** neither in the hot table nor in Parquet. Re-run `logsbench` and `TestDefaultVariantParquet` on 2.0.0 (due 2026-10-21) before ruling it out for good.
3. **Cold tier: Parquet written from the MAP layout.**
   - 30k-row row groups look better than 122,880 for lookups and memory.
   - Async read-ahead buys nothing on local disk.
   - Day-long Attribute searches need pruning beyond row-group stats.
4. **Ingest:** plan for automatic-checkpoint stalls of 2–3 s at 5× bursts, such as by buffering ahead of DuckDB. Target rate itself has plenty of headroom on 2 cores.

## Not measured, or partial

- Row groups per file, and parallelism across files (one file per run at this scale).
- Memory used by read-ahead and `async_threads` while ingest runs. Queries during ingest only read the hot table, at default settings. There was no settings sweep for starvation.
- Whether Parquet writes are asynchronous.
- Shredding as a function of batch size and time, which was captured only after the final checkpoint.
- Pruning, which is inferred from latency only (no EXPLAIN ANALYZE).
- The row-for-row check covers Logs 0..50,000, all from the unthrottled phase.
- "Errors in the last 15 minutes" covers about half of each run's logical time, not a small recent slice.
- At 5× the generator runs inline with flushes on the same 2 cores, costing about 8% of a core.
- Shredded VARIANT has no full report: it crashed once, and its quick-run numbers come from `variant.log` plus the smoke tests.
