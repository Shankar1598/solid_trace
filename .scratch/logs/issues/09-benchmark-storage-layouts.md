# 09: Benchmark the shortlisted storage layouts at reduced scale on the dev box

Type: task
Status: resolved
Blocked by: 03, 07
Map: [SolidTrace Logs](../map.md)

## Question

AFK task. Build a throwaway Go benchmark of the hot-table layouts shortlisted by the [DuckDB storage research](03-duckdb-schemaless-storage-options.md) and run it **on the dev box, about 5 minutes per layout**. It produces relative rankings and settings for [How are logs stored and retained?](10-storage-architecture-and-retention.md), not proof that the target scale holds; it reports numbers and a ranking, with no pass/fail bar. Record the numbers, the engine version and the machine.

**Re-scoped 2026-10-02.** A full day of target data (100–170 GB) does not fit this box (2 CPU, 11 GB RAM, ~21 GB free disk, shared with other sessions), and no target-sized VM or S3 bucket exists. S3, DuckLake, Pebble-first ingest and the full-scale run are parked (see "Parked" below).

### Engine

The DuckDB 2.0 alpha that EventStore already links (`event_store/scripts/fetch-duckdb`, `mise.toml` env, `-tags=duckdb_use_lib`); confirm and record `version()` as `TestDuckDBEngineVersion` does. Keep the harness re-runnable unchanged on 2.0.0 (due 2026-10-21).

### Layouts

Seven fixed columns (`time`, `level`, `message`, `service`, `environment`, `trace_id`, `span_id`) plus an id and `project_id`, with Attributes stored as:
1. JSON text
2. VARIANT, written as `text::JSON::VARIANT` (the 1.5-era Go bindings cannot append or bind VARIANT natively)
3. typed MAPs (one per value type)

Then hourly Parquet written from each layout, read from local disk.

### Run shape, per layout (about 5 minutes)

1. 2 min unthrottled: maximum write rate; this also builds the query dataset.
2. 2 min at target rate (1.9 MB/s uncompressed), then 1 min at 5×: does it keep up; memory (RSS) and CPU.
3. The query set, timed, cold and warm.
4. Size on disk. Delete the layout's files before the next layout runs.

### Synthetic data (defaults; within the limits of [Which ingest sources ship in v1?](08-v1-ingest-sources-auth-limits.md))

- Logs average about 600 B; messages 50–500 B.
- 3–8 services; the six levels, weighted toward `info`.
- About 40 common Attribute keys plus a Zipf long tail of about 2,000 distinct keys; about 5% of keys are mixed-type (a number in some Logs, a string in others); nesting already flattened to dotted names.
- About 2% of Logs carry a trace id.
- Generated in-process straight into the layout; the raw input is never stored on disk.

### Query set

- errors in the last 15 minutes
- count by `service` per minute
- equality on a common Attribute
- equality on a rare Attribute
- numeric range on a mixed-type Attribute (matches only numeric values)
- substring match in `message`
- group by an Attribute, top 10
- fetch one Log by id

Every layout's results must match the JSON baseline row for row.

### Questions to answer

- Ranking of the three layouts on write rate, query latency, disk size and memory.
- **VARIANT:** what the `text::JSON::VARIANT` write path costs in write rate; whether small flush batches (1–6k logs per flush) are shredded or only large row groups (30k+ rows on 1.5), and if not, how large or late batches must be before Attribute filters run fast; whether filters on shredded fields prune data in the hot table and in Parquet written by DuckDB.
- **Parquet on local disk:** time-range and Attribute queries with async I/O on and off (`read_ahead_depth = 0`); which rows per row group and row groups per file work best for shredding and async parallelism.
- **Sharing one process:** memory used by read-ahead and `async_threads` while ingest runs at target rate, and which settings keep a query from starving ingest.
- Are Parquet writes and native `.duckdb` reads asynchronous on the build tested?
- Projected query latency over a full day, labelled as a projection from the measured data size.

### Running on this box

- Run every build, test and benchmark process one at a time, under `systemd-run --user --scope -p MemoryMax=3G`. Set DuckDB `memory_limit = '2GB'` and `threads = 2`, and use `go build -p 1` / `go test -p 1`. Start no background processes.
- Keep disk use under 10 GB.
- A fresh worktree needs `mise trust` first.
- Ask that other sessions start no builds or tests during the runs (2 cores; they would skew the numbers).
- Findings go to `.scratch/logs/research/09-storage-bench-reduced.md` on a throwaway `research/logs-storage-bench-small` branch, with the harness code; this ticket gets a pointer.

### Parked (not in this ticket; on the map as fog)

- A full-scale run on a VM sized like the target (3–5 TB/month), with a day of data.
- The S3/GCS cold tier, and the published async gains on S3 (~3.7×) and many small files (~3×).
- DuckLake as the cold tier. If it is measured later, use a **SQLite catalog in its own file** (not the Rails database), so a separate sandboxed SQL-mode process can attach alongside EventStore; it needs DuckDB's `sqlite` extension bundled for air-gapped installs, and multi-client lock retries must be checked.
- Pebble-first ingest.
- Native VARIANT append and bind from Go, which waits on `duckdb-go v2.20000.0`.

## Comments

**2026-09-27, from [upgrade-pebble-duckdb 01](../../upgrade-pebble-duckdb/issues/01-upgrade-pebble-and-duckdb.md):** the "does duckdb-go have a 2.0 build?" check is done. It does not: the `v2.20000.0-N.preview` tags report `version()` = v1.5.4 (with 2.0 backports), not 2.0. To benchmark the real 2.0 alpha from Go, build with `-tags=duckdb_use_lib` against the alpha's `libduckdb.so` from https://duckdb.org/install/preview.html. In a scratch test, EventStore's storage and handler tests passed that way against v2.0.0-alpha43546. Once 2.0.0 ships, `duckdb-go/v2@v2.20000.0` should be the plain Go option. **Update:** EventStore itself now links the alpha this way. The benchmark can reuse `event_store/scripts/fetch-duckdb` and the `mise.toml` env, and `TestDuckDBEngineVersion` shows how to confirm the engine.

## Answer

Run 2026-10-02 on the dev box with DuckDB `v2.0.0-alpha43385`.
- **Findings:** [.scratch/logs/research/09-storage-bench-reduced.md](../research/09-storage-bench-reduced.md).
- **Harness:** `event_store/bench/logslayouts`.
- **Raw results:** `research/09-bench/`.
- **Where they live:** branch `feature/logging`, in the logs worktree, not the throwaway branch named above (the user's call).
- **Run length:** cut to about 2 minutes per layout (the whole benchmark in about 10 minutes), with slow query sets cut short. A first 5-minute run of JSON and MAP gave 3.5–4.8M rows, and its numbers agree.

**Ranking: typed MAPs, then JSON text, then VARIANT without shredding. Shredded VARIANT is unusable on this alpha.**
- **MAP:**
  - Attribute queries up to 5× faster than JSON (rare-key equality 105 vs 533 ms, group-by 145 vs 652 ms per about 1M rows).
  - The smallest on disk: 139 B/Log in the hot table, 101 in Parquet, against about 570 raw.
  - The least memory.
  - It writes 20k Logs/s unthrottled, more than 6× the 1.9 MB/s target.
- **JSON:** writes fastest (33k Logs/s) at 205 B/Log.
- **VARIANT without shredding:** close to JSON on queries, but uses the most disk and more memory.
- **Every layout and its Parquet matched the JSON baseline row for row.**

Answers to the ticket's questions:
- **VARIANT:**
  - Shredding happens per row group at checkpoint, for row groups of 30k+ rows. Small flushes pile into one row group and are shredded once it is big enough.
  - With a 2,000-key tail, shredding is ruinous: checkpoints of 31 s per 43k rows, Attribute filters of 4–6 s per 43k rows, and 985 Logs/s ingest with a 40 s flush stall.
  - In the quick run it hit one native heap-corruption crash in the 5× burst.
  - Default-shredded Parquet (about 2,960 columns) is slow, runs out of memory, and a `variant_extract(...)::VARCHAR` filter on it raises an INTERNAL error that invalidates the database.
  - Explicit shredding of the common keys is stable, but its Parquet is still 3–7× slower to query than JSON-text Parquet.
  - Pruning on shredded fields could not be measured.
- **Parquet on local disk:**
  - Async read-ahead on vs off makes no consistent difference, and on costs about 50–70 MB more RSS.
  - 30k-row row groups make lookups 2–3× faster cold than 122,880, with less memory.
  - Common-value "latest 100" filters are 25–30× slower than in the hot table, probably because the native top-N time pruning doesn't apply to the Parquet scan.
  - Row groups per file were not measured (one file per run at this scale).
- **One process:**
  - At target rate with queries looping, ingest kept up and queries took 25–32 ms at p50.
  - At 5×, automatic checkpoints cause 2–3 s flush stalls, but JSON still averaged 96–99% of 5×.
  - Peak RSS was 1.8 GB or less.
  - No settings sweep was done.
- **Async:**
  - `storage_block_prefetch` defaults to `REMOTE_ONLY`. Forcing `ALWAYS_PREFETCH` did not speed up cold native reads.
  - Whether Parquet writes are async could not be observed.
- **Projected full day (276M Logs), labelled a projection:**
  - A full-scan Attribute search or group-by over a day of Parquet would take 2–4 minutes on 2 cores, so day-long searches need time-bounded defaults or extra pruning.
  - Disk is about 28–30 GB/day of Parquet and 38 GB/day of MAP hot table.

The findings doc lists what was not measured or only partly measured. Re-run `logsbench` and `TestDefaultVariantParquet` on DuckDB 2.0.0 before ruling VARIANT out for good.
