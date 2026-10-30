# 06: Planning stalls: find the cause

**What to build:** some tiny Log reads spend 10–80 s in ClickHouse's planning, before reading any data. They make 400–2,900 GCS GETs one at a time across nearly every part of the `logs` table. Six of the 18 timeouts on 2026-10-27 to 30 were these. The cause isn't pinned yet: the [spec](../spec.md)'s Further Notes list what was ruled out (the filesystem cache, the deserialization-prefixes cache, ALTERs) and what the stalls' ProfileEvents show. The remaining suspect is statistics-based part pruning: every column carries `basic, uniq_v2` auto statistics, and `use_statistics_for_part_pruning=1`. Try it, and if it isn't the cause, catch a stall with trace logs.

**Blocked by:** None (can start immediately)

**Status:** claimed

- [x] Write the watch query: tagged Log reads (`log_comment` JSON) whose `QueryPlanOptimizeMicroseconds` is over 3 s, with their `S3GetObject` count. Record it here with today's baseline. The baseline is 19 reads over 3 s in 3 days: 14 of them over 9 s, and 7 timed out during or just after planning. See Comments.
- [x] ~~EventStore's Log reads set `use_statistics_for_part_pruning=0`, and `allow_statistics_optimize=0` if the first isn't enough.~~ Statistics aren't the cause (see Comments). Instead, EventStore's connection and the SQL users' profile set `allow_calculating_subcolumns_sizes_for_merge_tree_reading=0` (`event_store/storage/clickhouse.go`, `clickhouse_sql.go`), committed on `query-timeouts/06-planning-stalls` at `5f09e985`.
- [x] Check first, on the dev stack and in production `query_log`, that this doesn't slow normal reads, comparing median read times before and after. Dev: no change, except Attribute aggregations, 13–21% slower. Production (2026-10-30, with the user's go-ahead): 5 recent Log reads, each as fast or faster with the setting off. See Comments.
- [ ] After a release on production, run the watch query daily for a week. If stalls go on, capture one with trace logs (`send_logs_level='trace'` on a Log read tagged for the purpose, or `text_log` turned on briefly), and record where the planning time goes.
- [x] Record the outcome in the spec: the cause, or what was ruled out.

## Comments

### 2026-10-30: the cause

**Cause.** ClickHouse 26.8 works out the size of each JSON subcolumn a read names, such as `attributes.hostname.:String`, in every active part of `logs`. It does this before any pruning. The prewhere optimizer calls `MergeTreeData::getColumnSizes(queried_columns, allow_calculating_subcolumns_sizes_for_merge_tree_reading)`, which calls `part->getSubcolumnSize()` for each part. For a JSON subcolumn that opens a reader on the part, which reads the JSON column's serialization prefixes with `getReadSettingsForMetadata()`, so "without prefetches/filesystem cache". For a part in the bucket that's a few small GETs per subcolumn, one after another. The sizes are kept per part (an LRU of 1,024 names). So a stall comes when:
- a Query names Attributes, or type variants of them, not asked about before;
- or merges and moves have made new part objects.

Log reads ask for `.:String`, `.:Float64` and `.:Bool` of each Attribute they show, so one new Attribute is three new subcolumns.

**How it fits the evidence:**
- **The worst stall.** 07:44:38, 79.8 s, `ExceptionBeforeStart` code 159. It made 2,891 GETs of about 1 KB each, with `CachedReadBufferReadFromCacheMisses` 0, because the cache is bypassed. It read no marks or primary index, and took `PartsLocks` 1, the parts snapshot.
- **The statistics load was quick.** `LoadedStatisticsMicroseconds` was 29.6 ms, which matches `SharedPartsLockHoldMicroseconds` (29.5 ms), the statistics loop's per-part locks.
- **Re-runs were fast.** The sizes stay in each part's memory, which is why re-runs, cold-day runs and cache-off probes never stalled.
- **Clusters at the start of a session.** That's when new questions bring new Attributes.

**Ruled out: statistics.**
- `use_statistics_for_part_pruning` (`filterPartsByStatistics`) runs after partition pruning, on the parts left, and its estimates are cached on each part.
- `allow_statistics_optimize`/`use_statistics` gates the prewhere estimator. That loads statistics for all parts whenever the parts changed since the last 300 s refresh, but through the filesystem cache, and it took 20–40 ms on production.
- On dev, turning both off left the stall as it was (below).

**Reproduced on dev.** Table `probe06.logsw`, a copy of `logs` with Wide parts: 80 parts, 76 of them on the MinIO bucket disk. Each run reattached the table first, then ran a tiny read naming three Attributes (two types each):

| Settings | Planning | GETs |
|---|---|---|
| defaults | 2.35 s | 1,400 |
| `use_statistics=0, use_statistics_for_part_pruning=0` | 2.32 s | 1,368 |
| `allow_calculating_subcolumns_sizes_for_merge_tree_reading=0` | 0.026 s | 0 |

MinIO answers a GET in about 1.7 ms; at GCS's 38 ms, 1,400 GETs is about 53 s. Naming two more Attributes afterwards cost another 926 GETs, so it is per subcolumn, not per part. Compact parts (one data file) make 1 GET per part, which is why the first dev probes showed little. A read with `ORDER BY … LIMIT` on a plain table takes the top-K path and skips the prewhere optimizer, which is why the reattach probe showed nothing. EventStore's Queries go through the CTE form, which does take that path.

**The fix and what it costs.** With the setting off, ClickHouse sizes each Attribute as the whole `attributes` column, so planning reads nothing. It uses the same approximation to size the selected parts' read tasks, and as the first guess of the block-size predictor. Blocks start smaller (5.6× more function calls), so reads that aggregate over Attributes do more per-block work. Dev, warm, local disk, the median of 5 after one warm-up (`~/.cache/solid-trace/query-timeouts/ab-dev.sh`):

| Read | setting off | default |
|---|---|---|
| 90 s window, stall-shaped table read | 8 ms | 16 ms |
| newest page over a day | 68 ms | 73 ms |
| `search`-like over an hour, with an Attribute filter | 43 ms | 48 ms |
| avg of an Attribute by minute, an hour | 11 ms | 13 ms |
| count by an Attribute over a day (1M of 3M rows) | 51 ms | 42 ms |
| count and avg by two Attributes, 3M rows | 259 ms | 230 ms |
| count by `file`, 3M rows (no Attributes) | 58 ms | 60 ms |

So scans that read Attributes take about 10–20% more CPU, and the rest is unchanged or faster. A stall, by comparison, costs 10–80 s and usually a timeout, and ClickHouse goes on planning after EventStore has given up.

**Production A/B, 2026-10-30 09:54 UTC, with the user's go-ahead.** The SQL of four recent Log reads, plus `q90s.sql`, is in `~/.cache/solid-trace/query-timeouts/ab/`. Each was replayed with the setting at 0 and 1, alternating, for 3 rounds, in one `clickhouse-client --readonly=2 --database solidtrace` session: 30 statements in about 7 s, no errors, 0 GETs. The table gives median (planning) in ms. The setting off was as fast or faster every time, and planned 25–40 ms quicker even warm:

| Read | off | default |
|---|---|---|
| `q90s.sql` (Project 3, 90 s window, 7 Attributes) | 68 (29) | 76 (71) |
| Project 2, 3 days, summarize by `origin`, `read`, `project_id` | 68 (25) | 100 (53) |
| Project 2 table (`9765…`) | 68 (27) | 70 (51) |
| Project 3 table (`3402…`) | 11 (1.6) | 12 (1.6) |
| Project 3 count (`5734…`) | 14 (1.3) | 14 (1.2) |

These are small reads, since production's recent Log reads were. No big Attribute aggregation was replayed, so the dev figure (10–20% slower) still stands for those.

**The watch query** (`~/.cache/solid-trace/query-timeouts/watch-stalls.sql`). It must include `ExceptionBeforeStart`: a read that times out while planning is logged as that, with its planning time recorded.

```sql
SELECT
    event_time,
    JSONExtractString(log_comment, 'read') AS kind,
    JSONExtractUInt(log_comment, 'project_id') AS project,
    round(query_duration_ms / 1000, 1) AS secs,
    round(ProfileEvents['QueryPlanOptimizeMicroseconds'] / 1e6, 1) AS plan_s,
    ProfileEvents['S3GetObject'] AS gets,
    read_rows,
    formatReadableSize(read_bytes) AS bytes,
    exception_code AS err
FROM system.query_log
WHERE type IN ('QueryFinish', 'ExceptionWhileProcessing', 'ExceptionBeforeStart')
  AND event_date >= today() - 3
  AND log_comment LIKE '{%'
  AND ProfileEvents['QueryPlanOptimizeMicroseconds'] > 3000000
ORDER BY event_time
```

**Baseline, 2026-10-27 to 30.** 19 reads planned for over 3 s, 14 of them over 9 s. 7 timed out with 17–80 s of planning: 13:32:32 and 13:33:52 on 10-28; 07:44:38 (two reads), 07:45:25, 07:50:22 and 08:35:04 on 10-30. Codes 159, 210 and 236. Two more were big scans that also planned for 4.6 s and 11.8 s, then timed out reading (13:22:59 on 10-28, 05:21:37 on 10-30). All 19 were `table` reads of Projects 2 and 3.
