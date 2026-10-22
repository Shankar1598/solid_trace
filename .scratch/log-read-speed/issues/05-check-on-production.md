# 05: Check on production after release

**What to build:** Once 02 to 04 are in a release deployed to the production VM, time the Logs explorer on the `tagalys` Project and compare it with the figures from 2026-10-21 in the [spec](../spec.md)'s Further Notes. Take the timings from Console's "Completed 200 OK in Xms" lines in the solidtrace Organization's own Logs, matched by request id to their `Started GET` lines. Time:

- default loads (no Query, the last hour), early and late in an hour;
- a load with a `search`.

**Blocked by:** 02, 03, 04, and a release deployed to production.

**Status:** ready-for-human

- [ ] The first page's and the histogram's times for default loads, early and late in an hour, are recorded beside the 2026-10-21 figures
- [ ] A load with a `search` is timed and recorded
- [ ] The user confirms whether loading the explorer still holds both CPUs at 100% (for example from `docker stats`)

## Comments

**2026-10-21, v0.8.0 on production (EventStore up from 19:44:58 UTC).** First timings, through MCP `query_logs` on `tagalys` (last hour, a page of 200, no totals, unlike the explorer's `totals=1`), from Console's `log_read` call timings and EventStore's `log_read_sql` lines:

| Read, at 20:15 UTC (a quarter past) | Before (2026-10-21) | v0.8.0 | EventStore's SQL statements |
|---|---|---|---|
| Default load, first page | 1,304–1,332 ms at a quarter past; 2,292–4,798 ms after 10:15 | 203 ms, 313 ms | 113 + 64 + 15 ms; 61 + 106 + 100 ms |
| `search "timeout"` (40 Logs) | not timed | 2,931 ms | 29 + 2,752 + 104 ms |

Still to do: the histogram and the explorer's own first page (its `Completed 200 OK` lines; MCP reaches neither the histogram route nor `totals`), a late-in-the-hour load, and `docker stats` during a load.

Found on the way: `log_read_sql`'s `duration_ms` and `statement` still arrive as strings. The VM's checkout has v0.8.0's `vector.yaml`, but Vector reads its config only at start, and `deploy/install`'s `compose up` leaves it running (up 9 hours), so the new config isn't loaded.

**2026-10-21, 22:00 UTC, the Sidekiq Dashboards.** Dashboard 1 ("Sidekiq — jobs & queues", 18 Charts, 6 h) loaded at a median of 15.5 s per Chart cold (slowest 25 s), 0.1 s warm from the bin cache. Every Chart picked its Logs by an Attribute only `sidekiq_metrics.log`'s snapshots have (`isnotnull(latency_seconds)` and the like), and such rare keys sit in the maps, not promoted. On one cold hour of `tagalys` (12.8 M Logs, 10:00–11:00):

| Query, one hour | ms |
|---|---|
| `summarize count()` | 1,247 |
| `where file == "sidekiq_metrics.log" \| summarize count()` (15,040 Logs) | 107 |
| `where isnotnull(latency_seconds) \| summarize count()` | 1,461 |
| `where message contains "INFO: done:" \| summarize count()` | 4,251 |
| `where file == "sidekiq.log" and message contains "INFO: done:" \| summarize count()` (Dashboard 2's filter) | 2,029 |
| Chart 8 as it was (12:00–13:00) | 881 |
| Chart 8 with `file == "sidekiq_metrics.log" and` first (11:00–12:00) | 161 |

With the user's go-ahead, all 18 of Dashboard 1's Charts now start `where file == "sidekiq_metrics.log" and …`, and its description says why. All 18 over a cold 13:00–19:00, run 5–8 at a time through `get_chart_data` (the MCP server turns more away): median 2,135 ms, slowest 2,937 ms. Dashboard 2 already filters on `file`; what's left is its `message contains`, for the Log transform (log-transform 08) to turn into an Attribute.
