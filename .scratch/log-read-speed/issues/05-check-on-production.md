# 05: Check on production after release

**What to build:** Once 02 to 04 are in a release deployed to the production VM, time the Logs explorer on the `tagalys` Project and compare it with the figures from 2026-10-21 in the [spec](../spec.md)'s Further Notes. Take the timings from Console's "Completed 200 OK in Xms" lines in the solidtrace Organization's own Logs, matched by request id to their `Started GET` lines. Time:

- default loads (no Query, the last hour), early and late in an hour;
- a load with a `search`.

**Blocked by:** 02, 03, 04, and a release deployed to production.

**Status:** ready-for-human

- [ ] The first page's and the histogram's times for default loads, early and late in an hour, are recorded beside the 2026-10-21 figures
- [ ] A load with a `search` is timed and recorded
- [ ] The user confirms whether loading the explorer still holds both CPUs at 100% (for example from `docker stats`)
