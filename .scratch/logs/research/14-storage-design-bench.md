# 14: The logs storage design on DuckDB 1.5

Ticket: [logs 14](../issues/14-benchmark-storage-design-on-1-5-5.md). Run 2026-10-02.

- **Engine:** DuckDB **1.5.6** (duckdb-go `v2.10506.0`, bundled), the newest 1.5.x tag, not 1.5.5.
- **Machine:** dev box, 2 CPU, 12 GB, shared and at times busy. DuckDB `memory_limit=2GB`, `threads=2`.
- **Harness:** `event_store/bench/logslayouts` on `feature/logging`. Same generator as [ticket 09](09-storage-bench-reduced.md) plus an array key (`tags`, 5% of Logs), so the fourth map has data.
- **Raw results:** [14-bench/](14-bench/).
  - `report.md`, `json.*`, `map.*`: the first run (about 4 minutes per layout).
  - `cycles-*`: hot-table export cycles.
  - `*.log`: probes, each under a minute (`LOGSBENCH_PROBE=1` tests).
- **Scale:** reduced. Day-level numbers are projections, marked as such.

## 1. MAP vs JSON text on 1.5

Four typed MAPs (`attrs_string`, `attrs_number`, `attrs_bool`, `attrs_json`) against JSON text, from `report.md`:

| | json | map |
|---|---|---|
| Unthrottled write | 26.9k Logs/s | 17.3k Logs/s |
| Flush max, unthrottled | 3.0 s | 4.1 s |
| Target rate (1.9 MB/s) | kept up, flush p99 208 ms | one 3.7 s flush, backlog 9.4k Logs |
| 5× burst | 85% of the rate, flush max 2.6 s | 97% of the rate, flush max 2.9 s |
| Hot table B/Log | 221 | 180 |
| Parquet B/Log | 107 | 102 |
| Rare-key filter, hot, warm | 583 ms | 267 ms |
| Group-by on an Attribute, hot, warm | 803 ms | 278 ms |

- **Checkpoint stalls happen on 1.5 too:** 3–4 s flushes, including once at the plain target rate. ADR 0004 already absorbs them by writing to Pebble first.
- **No crash.** The heap corruption on the alpha came from VARIANT, which does not run on 1.5:
  - `variant_extract_string` does not exist;
  - VARIANT needs `storage_compatibility_version` v1.5.0, after which a checkpoint ran out of memory.
- **MAP still ranks above JSON:** 2–3× faster Attribute queries and smaller on disk.

## 2. Promoted columns

**Per-file promotion** (first run, Project 1, about 610k Logs, warm, 2 threads):

| | MAP only | 10 keys | 20 keys | 40 keys |
|---|---|---|---|---|
| Parquet B/Log | 102 | 90 | 87 | 85 |
| Export copy time | 7.2 s | 8.8 s | 10.3 s | 12.7 s |
| Common-key histogram, per-file read | 435 ms | 26 ms | 24 ms | 29 ms |
| Same, `coalesce(column, map[key])` | – | 156 ms | 98 ms | 62 ms |

- Reading through `coalesce` across files was 4–6× slower than reading the column where the file has it.
- Every form returned the same rows as the hot table.

**The 5 s day target** (`scale.log`):
- Full scans were timed at two sizes, to separate fixed cost from per-row cost.
- The projection is for a Project with the instance's whole volume, on 4 vCPU, using this box's measured 1→2-thread speedup (1.07–1.35).

| Query | Parquet, 22 h | Hot table, 2 h, MAP only | Day |
|---|---|---|---|
| Common-key histogram | 6.6 s | 9.3 s | ~16 s |
| Group-by on a common key | 4.1 s | 5.5 s | ~10 s |

- **Missed, mainly because of the hot table.** It has no promoted columns, and its 2 hours cost more than 22 hours of promoted Parquet.

**Promoted columns in the hot table** (`hot-promotion.log`, 800k rows):
- Common-key scans 12–15× faster: histogram 299 → 20 ms, group-by 277 → 22 ms.
- The rare-key filter 6× faster (268 → 43 ms), because the maps are smaller.
- `ALTER TABLE ADD COLUMN` takes about 3 ms on a filled table, but invalidates open appenders. They must be closed and reopened.
- With promoted columns, the hot table's 2 hours drop to well under 1 s. The day becomes about the Parquet part, 4–7 s, near the target. The first-milestone load test on 4 vCPU should confirm it.

**How many keys** (`wide-columns.log`, `wide-column-limits.log`):

| Promoted keys | Appender | SQL promotion | Export, one Project's hour | Scans |
|---|---|---|---|---|
| 0 (MAP only) | 31k Logs/s | – | 1× | 1× |
| 100 | 28k Logs/s | 35k Logs/s | 1.9× | – |
| 250 | 12.8k Logs/s | 21k Logs/s | 3.9× | – |
| 1000 | 2.6k Logs/s | 6.4k Logs/s | 15× | 6–15× faster |

- Every value costs ingest and export work, even NULL.
- 1000 columns cannot ingest the 3.2k Logs/s target. 100 costs almost nothing.
- **Footers** of 24 files with about 1000 columns: merged schema 0.3 s, per-column null counts 0.8 s.

**Latest N:** the hot table holds the newest 1–2 hours and answers "latest 100" in 20–85 ms. Parquet's latest-100 scans the whole file: the upper bound is about 3 s per full hour file on 4 vCPU. The 2 s target holds whenever the newest hours have enough matches.

**Decided** (ticket 14's comments): promote keys at ingest, up to 100 per Project. Once the limit is hit, re-rank nightly by popularity. The promoted set is a table in `logs.duckdb`. This replaces ADR 0004's per-file top N, so [How are logs stored and retained?](../issues/10-storage-architecture-and-retention.md) is reopened.

## 3. Hot-table plateau

`cycles-plateau.md`: 10 export cycles of 200k-Log "hours", ingest unthrottled, a `CHECKPOINT` after each delete.

- **The file levels off:**
  - 245–255k rows in the table;
  - file 36–58 MB, blocks 136–223, with no upward trend;
  - 1.5 truncates trailing free blocks at a checkpoint.
- **Checkpoint after the delete:** 2–3 s at this scale. Once, the delete itself took 2.6 s because an automatic checkpoint ran inside it.

## 4. Hourly export time and its effect on ingest

- **Export:**
  - 200k Logs: top keys 0.4–0.7 s, copy 4.1–4.6 s, under unthrottled ingest;
  - at the target rate (`cycles-target.md`): 1.4–1.9 s per 100k Logs.
  - A full hour of the whole instance (11.5M Logs) projects to about 3–4 minutes on 2 threads.
- **Effect on ingest at the target rate:** none visible. Flush p99 during the export was 170–290 ms, with a backlog of at most 4 Logs.
- **Risk for the load test:** the checkpoint after the delete took 0.4–3 s here. If it grows with the rows deleted, it could take minutes at full scale. The Log processor stalls during it, and Pebble buffers.

## DuckLake

`ducklake-probe.log`, SQLite catalog, DuckDB 1.5.6:
- **Bookkeeping:** the catalog records which file holds which promoted column, from per-file column statistics.
- **Reading:** the table is read as one, so keys still need `coalesce`.
- **Every file gets every column** added to the table.
- **Retention:** deleting a whole Project ends its files, which are removed after `ducklake_expire_snapshots` and `ducklake_cleanup_old_files`.
- **Moving files:**
  - Moving every file at once is easy: copy them, then change the one `data_path` row.
  - Mixing locations works only by editing internal tables.
- **Decided:** plain files. DuckLake's catalog pruning is worth a re-check with the v2 S3/GCS tier.

## Not measured

- 4 vCPU itself, and full-scale hours.
- The promoted set changing at midnight.
- One hot table per Project.
- Checkpoint time at full scale.

The load test is a first-milestone ticket, and covers these.
