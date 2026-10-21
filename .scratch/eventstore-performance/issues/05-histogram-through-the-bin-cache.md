# 05: The explorer's histogram through the bin cache

**Status:** ready-for-agent

**What to do:** Idea 9 of [performance-ideas.md](../research/performance-ideas.md), taken in [ticket 01](01-review-performance-findings.md). Run the Logs explorer's histogram bin by bin, as Charts do, so that bins whose Logs are all in Log files come from the bin cache.

## Where

- `handler/logs_histogram.go` calls `ScanLogTable` with no `Bins`.
- Charts pass `storage.TableBins` (`handler/logs.go`, `scanTable`) when the Query's table is made bin by bin (`query.Bins()`).
- The histogram's table is `summarize count() by bin(time, <bucket>), level` (`logquery/histogram.go`). Its bins are the bucket, and its key is the bin and the level.

## Expected gain

- Reopening a 24 h or 7 d explorer reads only the bins it lacks: the hot table's and the late Logs' hours.
- No gain for the default 1 h load, which reads only the hot table and late Logs. [Ticket 01](01-review-performance-findings.md)'s idea 3 (counts kept at ingest) is the one for that.

## Acceptance

- [ ] A histogram over hours in files answers the same with the bin cache as without it, across an export, late Logs, Retention and Project deletion (as `TestTheBinCacheAnswersAsARunWithoutItAcrossExportsLateLogsRetentionAndDeletion` does for Charts)
- [ ] A probe's before and after for a 24 h histogram opened twice (under a minute)

## Comments
