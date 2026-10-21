# 01: Review the performance findings and apply the ones that pay

**Status:** claimed

**What to do:** Go through the ideas in [performance-ideas.md](../research/performance-ideas.md) (its corrections at the top first), measure the ones that matter on a 2 vCPU VM, and apply those that pay off. Asked for by the user on 2026-10-21, outside the [Log read speed](../../log-read-speed/spec.md) spec. Build on `main` once Log read speed is merged, since ideas 1 and 4 touch what its ticket 02 changed.

## What the research found on production (2026-10-21)

- The hourly export holds Log processing up for 2 to 4 minutes every hour: `Log processing is behind` at hh:01 to hh:04, up to 4m13s behind. `ExportLogs` holds `writeMu`, which `AppendLogBatches` needs.
- DuckDB runs 8 threads on the 2 vCPU VM: a bucket install sets `threads = 4 × NumCPU` (`storage/duckdb_bucket.go:71`), and local reads get them too.
- The default explorer load reads only the hot table and late Logs, never a Log file.
- The histogram never uses the bin cache.

## Likely order

The report ranks 13 ideas. The first three look like the biggest wins on production:

1. The export's COPY outside `writeMu`, keeping its sort (idea 1b).
2. DuckDB threads for local reads on 2 vCPUs (idea 2), weighed against cold bucket reads (ADR 0008).
3. A histogram with no Query from counts kept by Log processing (idea 3), and the histogram through the bin cache (idea 9).

A CPU profile on production (idea 10) would turn the report's estimates into figures before ideas 6, 11, 12 and 13 are weighed.

- [x] Each idea is marked taken, left, or later, with a line saying why, in this ticket's Comments
- [ ] Before building a change that trades something away, settle it with the user: threads against cold bucket reads (idea 2), file size against read speed (`lz4_raw`, idea 5), checkpoint WAL against restart time (idea 6), or a new store of counts (idea 3)
- [ ] Each change taken is its own ticket in this directory, with a probe's before and after figures (under a minute each, like `event_store/bench/logreads/`)
- [ ] An ADR for any change that alters a recorded decision (threads in ADR 0008, the hot table and files in ADRs 0004 and 0005)

**Blocked by:** None (Log read speed was merged into `main` on 2026-10-21).

## Comments

### Triage (2026-10-21)

Production is now 2 vCPU with 15 GiB (was 7.7 GiB), and SSH works, so ideas 10 and 13 can be measured there.

| # | Idea | Call | Why |
|---|---|---|---|
| 1a | Drop the export's sort | **Left** | Log read speed 02's newest page skips a file's older row groups because files are sorted by time. |
| 1b | Export's COPY outside `writeMu` | **Taken**: [03](03-export-outside-the-write-lock.md) | Production stalls Log processing 2 to 4 min every hour. No trade-off: the files and the commit marker stay as they are. |
| 2 | DuckDB threads 8 → 2–3 | **Ask** | Trades local reads, ingest and the export against cold bucket reads (ADR 0008). The export probe already ran 30% slower at 8 threads than at 2 on 2 CPUs. |
| 3 | Histogram from counts kept at ingest | **Ask** | A new store of counts, which must follow the Log transform's drops, Retention and Project deletion, against 0.3–2.5 s per default load. |
| 4 | Files written newest first, step 1 per source | **Later** | Low risk, but its gain (filtered newest pages over files) needs a probe, and it touches Log read speed 02's two steps. After 03 and a production profile. |
| 5 | `lz4_raw` for Log files | **Ask** | Files grow 35–40% (about 600 → 800 MB an hour for tagalys) for 30–40% faster `message` searches over files. |
| 6 | Fewer hot-table checkpoints | **Ask** | Raising `checkpoint_threshold` means more WAL to replay on restart. A profile (idea 10) would show what checkpoints cost first. |
| 7 | Trigram skip index for `message` | **Later** | Large; worth it only once rare-term searches over files are the slow path. |
| 8 | DuckDB 2.0 | **Later** | duckdb-go has only a preview build, and 2.0's files can't be read by 1.5, so there's no rollback. |
| 9 | Histogram through the bin cache | **Taken** (next ticket) | Small, no trade-off: a reopened 24 h or 7 d explorer reads only the bins it lacks. |
| 10 | CPU profile from production | **Taken** (next ticket) | The enabler for 2, 6, 11, 12 and 13. With SSH, profiles can be written to the data volume and fetched. |
| 11 | Ingest JSON in one pass | **Later** | Go 1.27 (ticket 02) already made ingest JSON 18–29% faster. Wait for the profile. |
| 12 | msgpack code generation | **Later** | 1–3% of a vCPU at most; wait for the profile. |
| 13 | GOGC/GOMEMLIMIT | **Later** | Wait for the profile; with 15 GiB there is room for a higher GOMEMLIMIT. |
| — | DuckDB `memory_limit` (6000MB on production) | **Ask** | The VM doubled its memory. 50% (8GB) leaves the page cache for the last 24 h of Log files. A deploy config change, not code. |

Filed: [03](03-export-outside-the-write-lock.md) (built and resolved on `feature/export-outside-write-lock`; no ADR changes), [04](04-cpu-profiles-on-production.md) (profiles) and [05](05-histogram-through-the-bin-cache.md) (histogram through the bin cache), both `ready-for-agent`. The "Ask" rows wait for the user.

### The user's calls on the "Ask" rows (2026-10-21)

- **Idea 2, threads: left at 8.**
  - The user first chose 4 (2 per CPU). Measured first, on 2 pinned CPUs, 4 threads did no better than 8:
    - the export took 5.5–5.6 s at 4 threads against 5.6–5.8 s at 8;
    - the median wait of appends running beside it was 51–53 ms against 52–53 ms;
    - the read probe (`bench/logreads`, now with `LOGREADS_PROBE_THREADS`) was within noise at 8 and 4.
  - Only 2 threads helped: 4.8–5.2 s and 36–39 ms. But that costs cold bucket reads about four times, from about 2.6 s to 10 s for a cold 7-day chart, estimated from ADR 0008's probe.
  - With those figures the user kept 8. Revisit with ticket 04's profiles, or with DuckDB 2.0's async I/O.
- **Idea 3, counts at ingest: later**, after 05 and 04.
- **Idea 5, `lz4_raw`: probe it first**, as [06](06-lz4-raw-log-files-probe.md).
- **Idea 6, checkpoint threshold: after 04's profiles.**
- **DuckDB `memory_limit`:** the user leans to 50% of the VM, 8GB, so the page cache keeps the rest. It's a deploy config change for the user.
