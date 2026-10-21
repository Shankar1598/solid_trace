# 01: Review the performance findings and apply the ones that pay

**Status:** ready-for-agent

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

- [ ] Each idea is marked taken, left, or later, with a line saying why, in this ticket's Comments
- [ ] Before building a change that trades something away, settle it with the user: threads against cold bucket reads (idea 2), file size against read speed (`lz4_raw`, idea 5), checkpoint WAL against restart time (idea 6), or a new store of counts (idea 3)
- [ ] Each change taken is its own ticket in this directory, with a probe's before and after figures (under a minute each, like `event_store/bench/logreads/`)
- [ ] An ADR for any change that alters a recorded decision (threads in ADR 0008, the hot table and files in ADRs 0004 and 0005)

**Blocked by:** None (Log read speed was merged into `main` on 2026-10-21).

## Comments
