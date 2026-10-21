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
