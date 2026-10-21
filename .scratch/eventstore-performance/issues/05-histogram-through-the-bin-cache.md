# 05: The explorer's histogram through the bin cache

**Status:** resolved

**What to do:** Idea 9 of [performance-ideas.md](../research/performance-ideas.md), taken in [ticket 01](01-review-performance-findings.md). Run the Logs explorer's histogram bin by bin, as Charts do, so that bins whose Logs are all in Log files come from the bin cache.

## Where

- `handler/logs_histogram.go` calls `ScanLogTable` with no `Bins`.
- Charts pass `storage.TableBins` (`handler/logs.go`, `scanTable`) when the Query's table is made bin by bin (`query.Bins()`).
- The histogram's table is `summarize count() by bin(time, <bucket>), level` (`logquery/histogram.go`). Its bins are the bucket, and its key is the bin and the level.

## Expected gain

- Reopening a 24 h or 7 d explorer reads only the bins it lacks: the hot table's and the late Logs' hours.
- No gain for the default 1 h load, which reads only the hot table and late Logs. [Ticket 01](01-review-performance-findings.md)'s idea 3 (counts kept at ingest) is the one for that.

## Acceptance

- [x] A histogram over hours in files answers the same with the bin cache as without it, across an export, late Logs, Retention and Project deletion (as `TestTheBinCacheAnswersAsARunWithoutItAcrossExportsLateLogsRetentionAndDeletion` does for Charts)
- [x] A probe's before and after for a histogram opened twice (under a minute)

## Comments

### Built (2026-10-21, `feature/export-outside-write-lock`)

**What changed.**
- `handler/logs_histogram.go` passes `TableBins` whenever the histogram's Query is made bin by bin (`counts.Bins()`), as Charts do.
- Its key covers the Query's filters, so histograms of different Queries never share bins.

**A fix for Charts too.**
- `ScanLogTable` now skips the bin cache when the range has no Log file.
- With the cache on, every run first queried which bins hold hot or late Logs. Over a range with no file, no bin can come from the cache, since a bin it keeps holds no hot or late Log, so that query was pure cost.
- In the probe, it turned the hot table's histogram from 13 ms into 20 ms. On production's 12M-Log hot table it would be a scan of `time` on every default explorer load.
- Charts over ranges with no file save it too.

**Probe** (`bench/logreads`, 300,000 Logs, 2 pinned CPUs, best of 3). The histogram of the hour, with no Query, as the route runs it:

| Source | Without the bin cache (before) | Filling it | Reopened |
|---|---|---|---|
| Hot table | 13 ms | 13 ms | 13 ms |
| Log file | 26 ms | 29 ms | 3 ms |

- On production, every hour of a 24 h or 7 d explorer but the hot ones and those with late Logs comes from the cache once it has been opened.
- The default 1 h load is unchanged, as expected; idea 3 is the one for that.

**Tests.**
- The root bin cache tests now run three histograms (1 h buckets, 20 min with a `where`, and the auto size with `search`) through every scenario: after the export, after late Logs, after the next export, after a day's exports, after Retention, after the Project's deletion, and with the files in a bucket.
- Each must answer the same with the cache, without it, and from it, and miss no bin when run again.
- After the export, the hourly histogram takes 27 hours from the cache. That check fails without the handler change (checked).
- The bin cache, Chart, Dashboard and histogram root tests, and the `storage` and `handler` packages, pass.
