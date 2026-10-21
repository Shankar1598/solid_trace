# 02: Newest page read in two steps

**What to build:** A page of Log rows, as the Logs explorer, MCP `query_logs` and the Query route list them, reads each hour in two steps inside its one read:

1. It reads only `time` from the hour's sources (late Logs, the hot table and the hour's Log files), with the scan's bounds, cursor and Query condition, and takes the time of the Log at the page's remaining limit.
2. It reads whole Logs only at or after that time.

An hour with fewer Logs than the remaining limit is read as today. The page's Logs, order and cursor don't change. The scan's interface doesn't change. See the [spec](../spec.md)'s Implementation Decisions.

**Blocked by:** 01.

**Status:** ready-for-agent

- [ ] Logs at the same time as a page's last Log, spread across the hot table, late Logs and a Log file in one hour, are split across pages by id, with none skipped or repeated (root test through the Query route)
- [ ] An hour with fewer Logs than the limit lists them all, with no cursor
- [ ] A cursor continues inside an hour and across hours
- [ ] A Query condition with a small page limit lists the same Logs as listing all and filtering
- [ ] A run that spends its scan budget still answers what it found and a cursor
- [ ] The existing Query root tests pass
- [ ] The probe's newest-page timing, before and after, is in this ticket's Comments
