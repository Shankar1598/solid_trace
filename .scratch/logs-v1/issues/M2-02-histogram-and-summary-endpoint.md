# M2-02: Histogram and summary endpoint

**What to build:** for any filters, EventStore returns Log counts per time bucket per Level, the total Log count and per-Level counts, compiled through the same Query compiler. Spec: [Query compiler and query endpoints](../spec.md#query-compiler-and-query-endpoints-eventstore) (Histogram and summary); user stories 48, 50.

**Blocked by:** M2-01.

**Status:** resolved

- [x] A query-app operation takes a Project, time range, Query and bucket size, and returns counts per bucket per Level (Logs with no Level counted as their own group), the Log count and per-Level counts.
- [x] It is built as a Query through the compiler, not hand-written SQL, so it applies exactly the filters the stream does. For a Query ending in a tabular stage (M3-01), the counts are of the Logs that pass the filters before that stage.
- [x] Tests: counts match the rows the run operation returns for the same filters, across hot table and Parquet; empty buckets; Logs without a Level.

## Comments

### 2026-10-03 built (logs-v1/m2-02)

What was built:
- **`(*logquery.Query).Histogram(bucket) *Query`** (`logquery/histogram.go`): a new Query made of the Query's head (its leading `where`/`search` stages) followed by `summarize count() by bin(time, <bucket>), level`, compiled from that text by `Compile` and given the head and the narrowed range. Its table is `time, level, count_`, `level` NULL for Logs with no Level. Any tail stage is left out, so for a tabular Query (or one with `take`) it counts the Logs that pass the filters before it.
- **`logquery.AutoBucket(span)`**: the default bucket, the same ladder as `bin_auto` (at most about 100 buckets).
- **`POST /api/:project_id/logs/histogram`** on the query app (`handler/logs_histogram.go`): request `{"query", "from", "to", "bucket_seconds"?}` (RFC 3339, `[from, to)`, as a run). It runs the histogram Query through `ScanLogTable` (`Where`, `Select`, `TableSQL` of the histogram Query) and answers
  `{"bucket_seconds", "buckets": [{"time", "count", "levels"}], "count", "levels"}`.
  - `levels` always has all seven keys: `trace`, `debug`, `info`, `warn`, `error`, `fatal` and `none` (Logs with no Level).
  - `buckets` has every bucket that overlaps the request range, empty ones with zeros, oldest first, aligned to the epoch as `bin` is.
  - `count` and `levels` are the summary: the sum of the buckets.
  - A Query with mistakes gets `400 {"errors": [Problem]}`, as a run. A missing or reversed range, a negative bucket, a bucket over a year, or more than 10,000 buckets gets `400 {"error"}`.
- **Tests:** `logs_histogram_test.go`, through the query app: exact buckets with an empty bucket and Logs without a Level, before and after the export and with a late Log; for a set of Queries (filters, search, narrowing, a `summarize`, `take` and `project` tail) the counts equal the Logs the run returns for the same filters, before and after the export; the default bucket and the 400s. `handler/routes_test.go` checks the route is on the query app only.

Choices the ticket left open:
- **Bucket size** is the client's: `bucket_seconds`, a whole number of seconds. Without it EventStore picks as `bin_auto` does. The answer echoes the size used.
- **Buckets span the request range**, not the range a `where time …` narrows it to, so the strip's axis is the bar's range; buckets outside the narrowed range are zero.
- **The summary is summed from the buckets**, so it is one query, and the Log count always equals the sum of the bars.
- **No Level** is the key `none` (the Level set is fixed, so it can't clash) rather than JSON `null`.
- Like a table run, the histogram holds the publish read lock for its query, so an export waits for it.

Deviations: none.

Follow-ups:
- **M2-05:** add the operation to Console's EventStore client; send the same `query`, `from` and `to` as the run, and `bucket_seconds` from the range. The error rate is `(levels.error + levels.fatal) / count`. Per-Level counts filter with a `level == "…"` chip; `none` needs `isempty(level)`, which isn't a chip.
- **M3-03:** the histogram adds no SQL emission of its own: it is an ordinary compiled Query, so the readable SQL of `Histogram(bucket)` works as for any other Query.
- **M1-10:** measure a day-long histogram; it is a whole-range `summarize` through `ScanLogTable`.
