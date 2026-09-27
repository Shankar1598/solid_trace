# 08: Live tail

**What to build:** A Stream view shows new Logs for a Project as they arrive, with an optional KQL filter, pause/resume, level colouring and a bounded on-screen buffer. Polling uses a cursor on the time-ordered Log id against the hot table only.

**Blocked by:** 04

**Status:** wontfix

- [ ] `GET /api/:pid/logs/recent?kql&after&limit` returns rows newer than the cursor from `logs_hot` only and rejects aggregating Queries
- [ ] Console proxies it; the page polls every two seconds, pauses, resumes without gaps, and keeps at most a few thousand rows in memory
- [ ] Clicking a row opens the same detail sheet as the explorer

## Comments

2026-09-27: Closed as wontfix. This plan was superseded by the [logs map](../../logs/map.md). The file stays here as reference only.
