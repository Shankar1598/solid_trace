# EventStore performance ideas beyond the Log read speed effort

2026-10-21, for [ticket 01](../issues/01-review-performance-findings.md). Asked for by the user during the [Log read speed](../../log-read-speed/spec.md) effort: are there other performance wins, a quicker JSON library in Go, other DuckDB settings? A research agent read the code at `feature/log-read-speed` (71e690b), production's own Logs over MCP and the web. It built, tested and benchmarked nothing.

**Read with these corrections (2026-10-21):**
- **Keep the export's sort (idea 1a).** Log read speed 02 reads the newest page in two steps, and its second step skips a Log file's older row groups because files are sorted by time. Dropping `ORDER BY time, id` would undo that. Take idea 1b (the COPY outside `writeMu`) instead.
- **Idea 4's "step 1 per source"** builds on Log read speed 02 as merged; read `scanHour` in `storage/logs_query.go` first.
- The Go 1.27 JSON finding (idea 11, "Libraries") is filed on its own as [ticket 02](../issues/02-go-1-27-without-slower-json.md).


Read-only research at `feature/log-read-speed` (71e690b). Nothing was built, tested or benchmarked. Paths are under `event_store/`.
Markers: **[M]** measured (the source is named), **[C]** cited from the web (URL in Sources), **[E]** my estimate.
The spec's tickets (two-step newest page, `contains`/`starts_with`, text matches last) are not re-proposed here.

## Production facts found today

- **[M] The hourly export stalls Log processing for 2 to 4 minutes every hour.** Source: EventStore's own Logs in the `solidtrace` org, read over MCP. Every hour from 01:03 to 15:01 UTC on 2026-10-21 has a `Log processing is behind` warning for Projects 2 and 3, at hh:01 to hh:04, with `behind` from 1m53s to 4m13s. The longest stalls fall between 02:00 and 08:00 UTC. `ExportLogs` holds `writeMu` for the whole export (`storage/logs_export.go:31`), and `AppendLogBatches` needs the same lock (`logs_duckdb.go:303`).
- **[M] Production has a bucket.** The same Logs show `Uploaded a Log file to the bucket` each hour. So `UseBucket` ran, and it sets **`threads = 4 × NumCPU`, which is 8 on the 2 vCPU VM** (`storage/duckdb_bucket.go:71`; ADR 0008 notes that local reads get these threads too). The probe figures in the spec were taken at 2 threads.
- **[M] (code)** The hot table holds the current and the previous hour (`HotWindow`, `logs_duckdb.go:212`). So the default explorer load, which reads the last hour, reads only the hot table and `logs_late`, never a Log file.
- **[M] (code)** The histogram never uses the bin cache. `handler/logs_histogram.go:98-106` passes no `Bins`, while Charts do (`handler/logs.go:320-321`).
- **[M] (bench 14, dev box, amd64, 2 threads)** Exporting a full hour took 3 to 4 minutes, and checkpoints stalled for 3 to 4 s (`.scratch/logs/issues/14-benchmark-storage-design-on-1-5-5.md:24-27`).

## Ranked ideas

| # | Idea | Where | Expected gain | Effort | Risk | Spec scope |
|---|---|---|---|---|---|---|
| 1 | Make the hourly export cheaper and stop it blocking Log processing | `logs_export.go:30-56`, `:256-261` | Removes a [M] 2–4 min/h stall of processing and a 2–4 min/h burst on both CPUs. [E] Dropping the sort cuts the export's CPU by 30–60% | S (drop the sort) / M (unlock) | Low/Med | Out |
| 2 | DuckDB threads 8 → 2–3 on the 2 vCPU VM | `duckdb_bucket.go:71` | [E] 10–30% less CPU per read, and Go ingest no longer starved while reads run. Cold remote reads get slower ([M] 1.9 s → 5.2 s for a cold 7-day chart, 16 vs 4 threads, in ADR 0008's probe) | S | Med | Out |
| 3 | Histogram with no condition from per-bucket counts kept at ingest | `logs_duckdb.go:334-383`, `handler/logs_histogram.go:98` | [M] today 0.3–2.5 s per default load → [E] a few ms, and it frees a CPU for the first page | M | Med | Out |
| 4 | Newest-first order: files written `time DESC`, step 1 run per source | `logs_export.go:259`, ticket 02's step 1 | [E] A filtered newest page whose matches are common stops after a few row groups instead of reading the whole hour | S–M | Low | Next to ticket 02 |
| 5 | `lz4_raw` for Log files | `logs_export.go:260` | [C] lz4 decompresses 2.5× faster than zstd. [E] A lone `message` search over files gets 30–40% faster; files grow 35–40% (563 → ~770 MB/h) | S | Low | Out (spec forbids) |
| 6 | Fewer hot-table checkpoints (raise `checkpoint_threshold`, or commit more Logs at a time) | `duckdb.go:99-120`, `logprocessing/processor.go:22` | [C] Each checkpoint re-compresses the whole tail row group. [E] 3–10% of a vCPU, and fewer 3–4 s stalls | S | Low–Med | Out |
| 7 | Trigram skip index per row group for `message` | export, manifest, `logquery/sql.go` | [E] Rare-term searches over files 10–100× faster; nothing for common terms | L | Med | Out |
| 8 | DuckDB 2.0 once duckdb-go releases it and 2.0.x settles | `go.mod`, Dockerfile | [C] async Parquet I/O (would remove the need for idea 2's 8 threads), appends during checkpoint, DICT_FSST, UUID bloom filters | M | High now | Out |
| 9 | Histogram through the bin cache | `handler/logs_histogram.go:98` | [E] Reopening a 24 h or 7 d explorer reads only bins it lacks. No gain for the default 1 h load | S | Low | Out |
| 10 | A CPU profile from production | new, gated by an env var | Turns the [E] figures here into [M] figures. SSH is blocked, so log the top functions | S | Low | Enabler |
| 11 | Ingest JSON: one pass, not ~6 | `ingest/logs.go:129-158`, `ingest/normalise/` | [E] Ingest JSON costs 3–10% of one vCPU; this halves it. No wait time changes | S–M | Low | Out |
| 12 | Log batches via msgp code generation | `storage/pebble_logs.go:237-257` | [C] ~6× faster than vmihailenco on small structs. [E] saves 1–3% of a vCPU | M | Low | Out |
| 13 | GOGC/GOMEMLIMIT tuned for ingest's allocation rate | docker-compose env | [E] Unknown until profiled; GC is often 5–15% of an allocation-heavy service | S | Low | Out |

Not worth doing now (reasons at the end): a faster JSON library, Arrow scanning, prepared statements, pool tuning, `parquet_metadata_cache` (production already has it on), trace_id bloom filters, `preserve_insertion_order=false`.

## 1. The hourly export
- The COPY is `ORDER BY time, id` with zstd and `ROW_GROUP_SIZE 30720` (`logs_export.go:256-261`). It covers about 12M wide Logs for `tagalys`. It runs under `writeMu` across every Project (`:31-55`), then checkpoints (`:52`).
- [E] Most of the cost is likely the sort of ~3.5 GB of rows: payload and all, and probably spilling at `memory_limit` half of 8 GB. zstd at level 3 should take only ~10 s of CPU.
- Check this first with `EXPLAIN ANALYZE` of the COPY in a probe of one 1.5M-Log hour.
- **(a) Drop the ORDER BY.** The hot table is in arrival order, which is close to time order, and `preserve_insertion_order` (on by default) keeps that order in the file. Row-group min/max on `time` still prunes. The spec says sorted files are "never needed for correctness" (spec:68). Late rows of the hour are few, and widen only some row groups' ranges.
- **(b) Run the COPY outside `writeMu`.** Log processing never writes the exported hour to the hot table, because it lies outside the hot window. It can write that hour to `logs_late`, so the DELETE must take only rows the COPY's snapshot saw (by `batch` ≤ the snapshot's cursor, for example). Then take `writeMu` only for the DELETE and the manifest row.
- Either change also removes the catch-up burst of 0.4–0.8M Logs after each stall [E], and the gap in fresh Logs the explorer shows between hh:00 and hh:04.

## 2. DuckDB threads on 2 vCPUs
- The ADR 0008 trade-off: httpfs holds a thread per request it waits on. But the default load and the last 24 h of files (`local_cache_hours`) are local and CPU-bound.
- With 8 DuckDB threads runnable against Go's 2 Ps, Linux shares CPU per thread. So ingest, Log processing and HTTP get much less than half the CPU while a read runs [E].
- Each extra thread also keeps its own aggregate hash table and Top-N heap, for example in the histogram's `GROUP BY bin, level` [E].
- Test: run the spec's probe pinned to 2 CPUs (`taskset -c 0,1`) at threads 2, 3 and 8, together with an ingest load.
- If remote reads need the threads, DuckDB 2.0's async Parquet I/O (#23290) is the structural fix [C].

## 3. Histogram from counts kept at ingest
- The default load's histogram is `summarize count() by bin(time, auto), level` over about 12M hot Logs (`logquery/histogram.go:14`). [M] It takes 325–2,514 ms, and is slowest beside the first page.
- Log processing already keeps per-day key counts in the same transaction (`countKeys`, `logs_duckdb.go:367`). It could keep `(project, 10 s bucket, level) → count` the same way.
- A histogram with no condition then sums those rows. A histogram with a condition, or a bucket that isn't a multiple of 10 s, reads as today.
- The counts must follow the Log transform's drops, deletion past Retention, and Project deletion.
- With the bin cache (idea 9) and this, the explorer's strip costs almost nothing for any range without a Query.

## 4. Newest-first order for filtered pages
- [C] Top-N dynamic filters pass through UNION ALL into each scan (#15099; `join_filter_pushdown_optimizer.cpp` at v1.5.6), and prune a row group by its min/max.
- But a Log file is scanned oldest row group first. So the heap's boundary only tightens at the end of the hour.
- Writing files `ORDER BY time DESC, id DESC` (same cost as ASC) puts the newest row groups first. A page whose condition matches often (`level == "error"`, a common `contains`) fills 200 Logs in the first row groups and skips the rest [E, the pruning itself is cited, the effect is not measured].
- [C] For the hot table, 1.5 reorders row groups by max(time) for a Top-N (#19537, #19655), but only on a lone native-table scan, not under a UNION. Running ticket 02's step 1 as one small query per source (hot, late, files) and taking the cutoff in Go gets that reordering too.
- Rare terms gain nothing. Late materialisation needs LIMIT ≤ 50 and no UNION [C], so it doesn't apply.

## 5. Codec for Log files
- [M, spec] Reading `message` from a zstd file took 323 ms, against 124 ms from the hot table, for the same 580 MB. So about 200 ms of the 323 is decompression.
- [C] lzbench: lz4 decompresses at 3,850 MB/s against zstd's 1,550 MB/s, with a ratio of 2.10 against 2.90. zstd decompresses at the same speed at every level, so lowering `COMPRESSION_LEVEL` doesn't help reads.
- [E] With `lz4_raw`, the probe's 323 ms becomes ~200 ms, and production's estimated 1.5–2 s of unpacking per hour becomes ~1 s. It costs more disk, more bucket storage and more egress. DuckDB's COPY has one codec for all columns.

## 6. Hot-table checkpoints
- [C] An append never goes into a compressed segment. At each checkpoint, a column with new data is re-analysed and re-compressed across all of its tail row group (`column_data_checkpointer.cpp`; duckdb PR #24692 describes it, closed without merging).
- [E] ~3.3 MB/s of WAL against the 16 MiB default threshold means a checkpoint every ~5 s, each re-compressing up to 122,880 rows of `message`.
- Raising `checkpoint_threshold` to 128–256 MB means fewer, larger checkpoints. It costs more WAL to replay at restart.
- [C] 2.0 lets appends continue during a checkpoint (#25988).
- [C] DICT_FSST is off for files at storage version v1.0.0, which is how `logs.duckdb` is attached (`logs_duckdb.go:93`, no `STORAGE_VERSION`). Turning it on needs a new file.

## 7. Search index (the ADRs' "until a search index exists")
- At export, build a bloom filter of lowercased trigrams for each row group, and keep it beside the manifest.
- A `contains`/`search` term of 3 or more characters then lists candidate row groups. Turn them into `time` ranges (files are time-ordered) so that DuckDB prunes by min/max. No DuckDB extension is needed.
- [C] duckdb-ngram exists, but it is third-party and not in the community repository, and ADR 0003 bars loading extensions automatically.

## 8. DuckDB 2.0.0 LTS (released today)
- [C] Async I/O for Parquet (#23290) and native tables (#24654).
- [C] The ILIKE fast path, 2.6× (#23406). Ticket 03 makes this moot.
- [C] Pruning on prefix and `lower` stats, and `contains` when a row group's min equals its max.
- [C] UUID bloom filters, appends during checkpoint, DICT_FSST on by default.
- [C] Files written by 2.0 can't be read by 1.5, so there is no rollback. duckdb-go has only a preview build (v2.20000.0-6.preview).
- Bench 14 saw a heap-corruption crash on the 2.0 alpha [M].
- Wait for 2.0.x and a duckdb-go release, then rerun the probes.

## 10. A profile on production
- There is no pprof anywhere (grep), and SSH is blocked.
- Gate it on an environment variable: take a 30 s CPU profile each hour (one during the export) and log its top 20 functions. Vector already ships EventStore's Logs to the `solidtrace` org.
- That would rank ideas 2, 6, 11, 12 and 13 on measured figures.

## 11–13. JSON, msgpack, GC (question 1)
Libraries today, all std `encoding/json` v1 unless noted. Fiber v2 defaults to `json.Marshal`/`Unmarshal`. goccy is only an indirect dependency.

| Path | Library | Runs | Notes |
|---|---|---|---|
| `ingest/logs.go:129` `json.Valid`, `:135` into `[]RawMessage`, `:154-156` into `map[string]RawMessage` | std | per request / per Log | About 4 scans of each byte before normalising starts |
| `ingest/normalise/attributes.go:23-45` `flatten` | std | per nested object | Re-parses at each level of nesting |
| `normalise/fields.go:12` `parseString` | std | per string field and Attribute | |
| `attributes.go:113-115` `json.Compact` | std | per JSON Attribute | |
| `normalise/sentry.go:69-74` `unwrap` | std | per Sentry Attribute | One map decode per Attribute |
| `normalise/normalise.go:136-148` | std | per Log, with a VRL transform | Encodes to JSON, then parses it again |
| `storage/pebble_logs.go:237-257` | vmihailenco/msgpack v5.4.1 | per batch, in and out | Reflection; a new Encoder each time |
| `vrl/vrl.go:114-163` | msgpack | per Log, with a transform | Logs reach Rust as msgpack, not JSON |
| `models/log.go:40` `Attributes.MarshalJSON` and Fiber `c.JSON` | std | per Log in an answer (≤1,000) | Small |
| `logs_duckdb.go:510-588` `scanHotRow`/`goMap` | duckdb-go `OrderedMap` | per Log row returned | `Set` is O(n²) (`duckdb-go types.go:553`); no JSON is decoded here |

- **Where JSON costs CPU [E].** About 1 KB per Log at 3,300 Logs/s, ~6 scans per byte, and maps allocated per Log, come to 3–10% of one vCPU for ingest JSON. msgpack in and out adds 1–3%.
- **Against reads.** A single default load is [M] 1.3–4.8 s plus 0.3–2.5 s on both CPUs today, and the export is [M] 2–4 min/h. JSON is a steady few percent that no member waits on. **A library change is likely not worth it.**
- If a profile shows ingest JSON above ~15% of a vCPU, do the single-pass cleanup (idea 11): drop `json.Valid` before `Unmarshal`, stream the array with `json.Decoder.Token`, and unquote strings without a second Unmarshal.
- **Libraries [C]:**
  - **encoding/json/v2** graduated in Go 1.27 (August 2026; `go.mod` here is 1.25.5). The v1 API now runs on the v2 engine, and for `map[string]any`/`any` it was measured 15–48% *slower* (reqfleet on an M2; huizhou92 on an M5). The v2 API itself was 1.76× faster than v1 on `any`.
    - So on upgrading to Go 1.27, either call the v2 API on the ingest paths (it rejects duplicate names and invalid UTF-8, where v1 accepted them), or build with `GOEXPERIMENT=nojsonv2`, and time ingest before and after.
    - Neither study measured `map[string]RawMessage`, which ingest uses.
  - **sonic:** no JIT on arm64. Its arm64 encode was slower than the stdlib (#786), it has an open process abort under CPU profiling on linux/arm64 (#970), and it falls back to the stdlib on each new Go release until it ships an update. No.
  - **goccy/go-json** came back with v0.11 (2026-09) after jsonbench called v0.10 "not safe for production". Not yet.
  - **segmentio** is slower than v2 on interface types. **json-iterator** is archived (2025-12). **simdjson-go** needs AVX2, so it doesn't run on Axion. **fastjson** only parses; it would mean rewriting `flatten`.
  - **msgpack:** vmihailenco is dormant (last release 2023). tinylib/msgp code generation is ~6× faster on small structs (alecthomas benchmarks), but its `interface{}` support is limited, which matters because `models.Log` holds typed maps.

## Checked and not worth it now
- **Arrow instead of `rows.Scan`.** It needs a build tag, has no pooling, and pages are ≤1,000 Logs. [C] duckdb-go #141 already halved scan cost in v2.10503.1, and we are on v2.10506.0.
- **Prepared statements.** The SQL carries its literals, and planning is milliseconds [E].
- **Pool tuning.** DuckDB connections are cheap [E].
- **`parquet_metadata_cache`.** [C] It defaults to off, but `UseBucket` turns it on, so production has it. A local-only install could turn it on too, at no cost.
- **Bloom filters for trace_id.** [C] The writer keeps a dictionary up to rows/5 (6,144 values at 30,720 rows), and a bloom filter needs the dictionary. [E] A trace_id row group very likely stays under that, so it already has a filter; check with `parquet_metadata`. `id` is UUIDv7, whose min/max already prune.
- **`preserve_insertion_order=false`.** It changes the order of SQL-mode rows. The export's `ROW_GROUP_SIZE_BYTES` needs it, but idea 1(a) needs it on.
- **[C] The 1.5 adaptive filter** reorders conditions only within a scan's filters. It doesn't change ticket 04.

## Sources
- Go: https://go.dev/doc/go1.27 · https://go.dev/blog/jsonv2-exp · https://go.dev/doc/jsonv2-migration · https://reqfleet.com/blog/benchmarking-go-1-27-encoding-json-the-any-trap · https://huizhou92.com/encoding-json-v2-waited-five-and-a-half-years-to-become-gos-default/ · https://github.com/go-json-experiment/jsonbench
- Go libraries: https://github.com/bytedance/sonic/issues/786 · https://github.com/bytedance/sonic/issues/970 · https://github.com/goccy/go-json/releases/tag/v0.11.0 · https://github.com/json-iterator/go · https://github.com/minio/simdjson-go · https://github.com/valyala/fastjson · https://raw.githubusercontent.com/alecthomas/go_serialization_benchmarks/master/report/data.js
- DuckDB Top-N: https://github.com/duckdb/duckdb/pull/15099 · /pull/19537 · /pull/19655 · /pull/17325
- DuckDB Parquet and settings: https://duckdb.org/2025/03/07/parquet-bloom-filters-in-duckdb · https://duckdb.org/docs/1.5/sql/statements/copy.html · https://duckdb.org/docs/1.5/configuration/overview.html · https://duckdb.org/docs/1.5/guides/performance/file_formats.html
- DuckDB storage: https://duckdb.org/docs/2.0/internals/storage.html · https://github.com/duckdb/duckdb/pull/24692
- DuckDB 2.0: https://duckdb.org/2026/10/21/announcing-duckdb-200.html · https://github.com/duckdb/duckdb/releases/tag/v2.0.0
- Codecs and drivers: https://github.com/facebook/zstd (lzbench table) · https://github.com/duckdb/duckdb-go · https://github.com/danthegoodman1/duckdb-ngram
