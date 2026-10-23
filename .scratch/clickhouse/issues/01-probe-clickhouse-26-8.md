# 01: Probe: ClickHouse 26.8 with JSON Attributes, on a production quarter-hour

**What to build:** A probe, not product code. It settles the spec's open technical choices on real data before anything is built on them, and it gates the effort. It runs ClickHouse 26.8 LTS against the same production quarter-hour of `tagalys` as the [research](../../eventstore-performance/research/clickhouse-vs-duckdb.md), with the `logs` table the [spec](../spec.md) describes: JSON Attributes, a `file` sort column, `PARTITION BY (project_id, toDate(time))`, `ORDER BY (project_id, service, file, time)`. The results are compared with the research's figures for the same reads.

**Blocked by:** None (can start immediately)

**Status:** resolved

- [x] **Data.** Copy one `tagalys` quarter-hour off production again (`~/Tagalys/apps/api/ssh 10.164.0.2`, as on 2026-10-23), and delete it afterwards. Check the user's OK under Comments before copying. The dev box has about 9.5 GB free, so keep only this file and ClickHouse's copy.
- [x] **Setup.** Pin `clickhouse/clickhouse-server` to an exact 26.8 LTS patch. Run it on CPUs 2,3 with 3 GB, as the research did. Scripts and results go in `~/.cache/solid-trace/clickhouse-probe/`, and no run takes more than a minute.
- [x] **JSON against promoted columns.** Load the file into the spec's table. Attributes go into `JSON` from the file's promoted columns and maps; array/JSON values go into the separate map. Run the research's 14 slow reads and Dashboard 1's 16 Charts, hand-written as in `~/.cache/solid-trace/prod-quarter/hand_sql.py`, with Attributes read through JSON paths. Record CPU and best-of-3 times next to the research's MergeTree-with-108-columns figures.
- [x] **Typed values.** For the language's non-coercing `==`, check how a path holding both strings and numbers compares, how `tostring`/`tonumber` read it, and how a missing path reads.
- [x] **Sort column.** Check whether a typed JSON hint for `file` can be in `ORDER BY`, and how a plain `file` column compares. Pick one.
- [x] **`search`.** Time ways to match `message` and every string Attribute's *value* (never key names) on JSON. Pick one.
- [x] **Newest page.** Time the two-step read (times and ids, then rows by id) against one step with lazy materialization up to 200 rows, with no filter and with a common filter.
- [x] **Histogram MV.** Count by Project, minute and level from a summing MV against from the table.
- [x] **Inserts while reading.** Insert about 3,600 Logs a second for under a minute, in one-second inserts as Log processing will, while a read loop runs. Record:
  - insert CPU per Log, against the research's 15 µs;
  - parts and merges;
  - read times during ingest.

  Then try the merge pool and `max_threads` for 2 CPUs.
- [x] **Dedup.** Re-send an insert with the same token under `non_replicated_deduplication_window`, including one that spans two day partitions, and check it's dropped.
- [x] **Old Log files.** The production quarter-hour is a DuckDB-written file. Read it with `file()` (and, if MinIO is up, `s3()`) as one source with a second file that has fewer promoted columns. Time a promoted-key filter with its map fallback (`coalesce(col, map[key])`) against DuckDB's time on the same file. Check that the MAP, UUID and timestamp columns read correctly.
- [x] **Quantiles.** Note which ClickHouse quantile function matches DuckDB's `approx_quantile` closely enough for the root tests' expected values.
- [x] **Breaking changes.** Read the 26.4–26.8 breaking changes for anything that touches the spec: JSON, S3 disks, row policies, settings constraints, lazy materialization, `clickhouse-go`.
- [x] **Gate.** Pass if JSON paths cost no more than 1.5× the promoted columns' CPU on the common-key reads and the Charts, and inserts keep up at 3,600 Logs a second on 2 CPUs. If it passes, record the choices above under Answer for the later tickets. If it fails, stop and hand the table layout back to the user with the numbers (a table per Project with promoted columns, or typed `Map`s). Build nothing further.

## Comments

- 2026-10-23, planning: copying a production quarter-hour needs the user's OK, as for the research. Asked in the planning session.
- 2026-10-23: the user OK'd copying the quarter-hour ("ok to copy the quarter-hour, start 01").

## Answer

Run 2026-10-23. Scripts, raw outputs and a README are in `~/.cache/solid-trace/clickhouse-probe/`. The same production file as the research was used (`day=2026-10-22/hour=21/data-15.parquet`: 3.16 M Logs, 108 columns). The file and ClickHouse's data were deleted afterwards, and so were the containers.

**Gate: passes.**
- **Reads:** JSON paths used 0.95× the promoted columns' CPU on the 14 slow reads and 0.9–1.0× on the Charts. On the reads that only touch Attributes, they used 1.08–1.17×.
- **Inserts:** they kept up at 3,579 Logs/s using 8% of 2 CPUs.
- **Answers:** every read gave the same answer on both tables (`check_same.txt`).

### Setup

- **Image:** `clickhouse/clickhouse-server:26.8.34.13`, the newest 26.8 LTS patch. It has the critical row-policy fix #125495.
- **CPUs and memory:** CPUs 2,3, with 3 GB set through `max_server_memory_usage`.
  - **Not `docker --memory`.** With a cgroup memory limit, 26.8 starts with jemalloc `dirty_decay_ms=0`, so it returns freed pages to the kernel at once. Every scan then page-faults its buffers back in (220k soft faults and 0.48 s of system time for one `message` scan), and scans run about 1.9× slower.
  - The memory-worker settings, `MALLOC_CONF` and dropping the cpuset don't change this. Removing the cgroup limit does (`memory-limit.txt`).
  - 25.8 doesn't do it.
- **Comparison table:** a `logs_cols` table (the research's 108-column MergeTree) was loaded on 26.8 next to the spec's `logs`. 25.8.33.6 was re-run on today's box as a control; it matched the research to within noise.

### Reads (ms, best of 3; condition cache off)

| Read | Research (25.8) | 25.8 today | 26.8, 108 columns | 26.8, JSON |
|---|---|---|---|---|
| S0 newest page (200) | 250 | 269 | 80 | **69** |
| S1 region + file, `message` startswith | 4 | 3 | 6 | 5 |
| S2 `message` contains "lounge" | 374 | 364 | 323 | 333 |
| S3 file in 6 files | 5 | 4 | 4 | 3 |
| S4 `search` "burton-na-staging" | 996 | 948 | 828 | 763; **390** with `search_text` |
| S5 `search` "lounge" | 1,025 | 958 | 861 | 817; **422** |
| S6 file in 4 files, min/max | 4 | 4 | 4 | 3 |
| S7 `tostring(store_id) in (12)` | 33 | 35 | 31 | 27 |
| S8 region, service, level, not `message` contains | 79 | 104 | 610 | 627 (see PREWHERE) |
| S9 file contains "webhook" | 20 | 22 | 21 | 19 |
| S10 file + region + `message` contains | 5 | 4 | 6 | 5 |
| S11 `tags.host` contains "lounge" | 9 | 8 | 9 | 8 |
| S12 `search` "timeout", page | 1,087 | 1,088 | 1,084 | 976; **576** |
| S13 count by minute (table) | 43 | 42 | 33 | 35 |
| S13 count by minute (MV) | 2 | 2 | 2 | 3 |
| Each of Dashboard 1's 16 Charts | 4–9 | 5–8 | 4–9 | 4–9 |
| **CPU, 14 slow reads, one pass** | 7.48 s | | 7.58 / 7.84 s | **7.31 / 7.38 s** |
| **CPU, Dashboard 1 refresh** | 0.20 s | | 0.18 / 0.20 s | **0.18 / 0.18 s** |
| CPU, Attribute-only slow reads ×10 | | | 1.49 / 1.61 s | 1.73 / 1.75 s |

The JSON slow-read CPU above uses `search` as per-path ORs; with `search_text` it is lower. Raw figures are in `times_*.json`, `cpu.txt` and `cpu_keys.txt`.

**Storage.** `attributes` takes 24.5 MiB against 22.8 MiB for the promoted columns and maps. `search_text` adds 9.6 MiB. The whole table is 172 MiB against 161 MiB.

**PREWHERE in 26.8 (S8).** 26.8's automatic PREWHERE ordering puts the `message` matches ahead of `region_code IN`, so it reads all of `message`: 766 MB, against 47 MB on 25.8.
- `allow_reorder_prewhere_conditions=0` fixes S8 (93 ms) but slows S9 (19→46 ms) and S11 (8→26 ms).
- An explicit `PREWHERE` of the conditions that don't touch `message`, with the `message` matches in `WHERE`, gives 97 ms.
- This affects both layouts, not JSON. Ticket 04 should consider emitting that split.

### Inserts (`ingest_*/summary.txt`)

There were 45 one-second Native inserts of 151,006 Logs, about 3,580 a second. Each had a dedup token and `async_insert=0`, and was sent from a client container on CPUs 0,1. The CPU figures are the server's, including merges. The idle server uses 0.04 CPU-s each second, about 1.8 s of each run.

| | Server CPU | µs a Log, gross / net of idle | Share of 2 CPUs | Insert p50 / max | Merges | Active parts, max |
|---|---|---|---|---|---|---|
| JSON | 6.71 s | 44 / ~33 | 7.9% | 89 / 108 ms | 18 | 8 |
| JSON while reads run | 10.4 s (includes reads) | | 12.3% | 90 / 128 ms | 18 | 7 |
| JSON without `search_text` | 6.75 s | 45 / ~33 | 8.0% | 89 / 149 ms | 19 | 8 |
| 108 columns | 5.71 s | 38 / ~26 | 6.8% | 89 / 124 ms | 19 | 7 |

- **Against the research's 15 µs.** That figure was for a synthetic 13-column table, with the cost of reading the files subtracted. Production's shape costs about 26 µs a Log with promoted columns and about 33 µs with JSON (1.25×).
- **Merges** finished within 0.1–0.4 s of the last insert.
- **Reads during ingest,** on the partition being filled (medians, max in brackets): newest page 11 ms (63), a Chart 9 ms (39), count by minute 5 ms (33), `search` 27 ms (48).
- **Merge pool and `max_threads`:** left at their defaults and not measured, because reasoning settles it. 26.8 sizes `background_pool_size` to 3 on 2 CPUs (25.8 used 16), and `max_threads` is auto(2). Merges never queued, and ingest took 8% of the CPUs, so neither setting can change the outcome at this rate.

### Choices for the later tickets

- **Sort column: a plain `file LowCardinality(String)` column.** A typed hint in `ORDER BY` is accepted, but it coerces a numeric `file` to a string (`123` reads back as "123"), which breaks typed meaning (`sort_hint.txt`).
- **`attributes`: plain `JSON`,** with no hints and the default `max_dynamic_paths` (1024). The quarter-hour had 75 paths, none in shared data; two hold both numbers and strings (`metric`, `tags.store_id`).
- **`attrs_json`:** `Map(LowCardinality(String), String)`.
- **How values are inserted.** Log processing sends typed values through the native JSON column: Float64 for every number, plus String and Bool.
  - JSON text would infer Int64 for `200`. It would also infer Date or DateTime for date-like strings unless `input_format_try_infer_dates` and `input_format_try_infer_datetimes` are 0.
  - The probe's load wrote numbers with a decimal point and set both of those settings to 0.
- **Typed values (`typed.txt`).**
  - **Comparisons:** compare typed subcolumns, never the bare Dynamic path.
    - `== 200` becomes `attributes.k.:Float64 = 200`, `== "200"` becomes `.:String = '200'`, and `== true` becomes `.:Bool = true`.
    - Comparing the Dynamic path itself with a literal throws `NO_COMMON_TYPE` on a path that holds mixed types, for `=`, `!=` and `>` alike.
  - **Missing paths** read as NULL, so `==`, `!=` and `NOT` all fail to match, as now.
  - **`tostring`:** `coalesce(.:String, toString(.:Float64), toString(.:Bool))`. `toString(dynamic)` returns '' rather than NULL for a missing path, and 200.0 prints as "200".
  - **`tonumber`:** `coalesce(.:Float64, toFloat64OrNull(.:String))`, plus `.:Bool` if today's `tonumber` converts bools.
  - **Present:** `attributes.k IS NOT NULL OR mapContains(attrs_json, 'k')`.
  - **Aggregates:** aggregate typed subcolumns only. Since 26.6, `min` and `max` reject a Dynamic value.
- **`search`: a `search_text String MATERIALIZED '' CODEC(ZSTD(1))` column.**
  - It holds every string Attribute value joined by `\0`. Log processing writes it with `insert_allow_materialized_columns=1`, and being MATERIALIZED keeps it out of `SELECT *`.
  - Times: 390–576 ms, against 730–968 ms for per-path ORs and 1.9–2.2 s for `JSONAllValues`, which also matches numbers and bools.
  - All three give the same answers. It costs 5.6% of disk and no measurable insert CPU.
- **Newest page: one step with lazy materialization** (`newest.json`). The times below are for no filter, `level` error and `region_code` r4:
  - one step with lazy materialization: 70, 67 and 64 ms;
  - two steps: 84, 87 and 74 ms;
  - one step without lazy materialization: 87, 258 and 101 ms.

  26.8's default `query_plan_max_limit_for_lazy_materialization` is already 10000, so no setting is needed. Only one day was measured; ticket 03 should check a multi-day range (top-K dynamic filtering is on by default).
- **Histogram:** the summing MV, as in the spec. It answers in 4 ms against 60 ms from the table over the quarter-hour, with the same counts.
- **Insert and dedup settings (`dedup.txt`).**
  - On the writer: `async_insert=0` (the default has been 1 since 26.3), a dedup token on each insert, and `max_insert_threads=1`.
  - `non_replicated_deduplication_window` must be set on `logs` **and on every MV target** (and on the key-counts table). Without it on `logs_minute`, a re-send was dropped from `logs` but counted twice by the MV.
  - With it, both a one-day re-send and a re-send spanning two day partitions (two parts) were dropped from the table and the MV, under 26.6's `new_unified_hash`.
- **Memory:** cap ClickHouse with `max_server_memory_usage`, never a compose `mem_limit` or `docker --memory`. That includes `bin/clickhouse-dev`.
- **Quantile: `quantileTDigest`,** the same t-digest family as DuckDB's `approx_quantile` (`quantiles.txt`).
  - The root tests' only fixed value, `percentile(duration, 100)` = 900 in `logs_language_test.go`, holds. No expected values change.
  - On that test's six durations, p95 is 900 as on DuckDB; `quantile` gives 750. p50 differs (80.5 against DuckDB's 100.25), but no test asserts it.
  - On production durations, p95 is within 0.2% of DuckDB and p99 within 2.5%.
- **Old Log files (`old_reads.txt`, `s3_read.txt`).**
  - **Correctness:** `file()` reads DuckDB's MAP, UUID and TIMESTAMP columns correctly. Counts, distinct ids, id and batch strings, times to the microsecond and map sizes all equal DuckDB's.
  - **One `file()` call over both files:** `schema_inference_mode='union'` makes a missing promoted column NULL.
  - **Separate sources (`file()` with `s3()`):** the compiler must write `CAST(NULL AS Nullable(T))` for the missing column.
  - **`s3()`** needs explicit keys or a named collection, since 26.7. It read MinIO correctly, and `file()` with `s3()` as one read took 93 ms.
  - **Times,** with promoted keys read as `coalesce(col, map[key])` (ClickHouse against DuckDB, ms):

    | Read | ClickHouse | DuckDB |
    |---|---|---|
    | `tags.host` contains | 76 | 107 |
    | metrics Chart | 9 | 13 |
    | `region_code == r4` | 108 | 71 |

- **Breaking changes in 26.4–26.8** (about 58; `changelog-26.4-26.8.md`). These are the ones that touch the spec:
  - `async_insert` now defaults to 1, from 26.3.
  - The dedup hash is now `new_unified_hash` (26.6). Re-tested: it works.
  - `max_insert_threads` is now auto (26.8).
  - `s3()` from SQL no longer uses the server's credentials (26.7).
  - The Arrow Parquet reader is gone (26.5). The v3 reader reads old files correctly.
  - `min`/`max` and minmax indexes reject Dynamic values (26.6/26.7). The `batch` minmax index is on a UUID column, so it's fine.
  - Nothing breaking touches row policies, settings constraints, custom settings, `select_from_*_requires_grant`, lazy materialization, quantile/uniq or `clickhouse-go`. `clickhouse-go` speaks protocol 54460, so 26.8's new String wire layout doesn't reach it.
  - Default changes not in the breaking list:
    - `use_variant_as_common_type=1`: mixed `if`/`multiIf` branches now give a Variant instead of an error;
    - `date_time_input_format=best_effort`;
    - `deduplicate_blocks_in_dependent_materialized_views=1`;
    - `allow_statistics_optimize=1`.
  - Known bugs in 26.8.34.13:
    - DateTime64 key pruning skips the boundary second when `time` is compared with a DateTime or integer constant (#123999). Compare only with `toDateTime64(…)`, and re-pin to 26.8.35.x once it has been out a few days.
    - Lazy materialization is lost for `SELECT * FROM (SELECT * …)` under a row policy (#127157). SQL mode shouldn't wrap the query that way.

### Where the spec should change

- **Newest page:** one step, not "keeps today's two steps".
- **Columns:** add `search_text` for `search`.
- **Typed values:** "the path's Dynamic value, compared within its own type" has to mean comparing typed subcolumns.
- **MV dedup:** MV targets need the dedup window.
- **Dev container:** cap it with `max_server_memory_usage`, not a container memory limit.
- **Ingest CPU:** about 33 µs a Log net (26 µs with promoted columns), not 15 µs. That is still 8% of 2 CPUs at 3,600 Logs/s.
- **Sort column:** keep the `file` column. A hint can sit in `ORDER BY`, but it coerces values.
