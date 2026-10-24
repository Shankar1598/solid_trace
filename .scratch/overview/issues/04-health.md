# 04: Health

Spec: [../spec.md](../spec.md)

**What to build:** A **Health** section on the Overview shows:

- Per Project, how long the oldest waiting Log batch has waited for Log processing.
- Per Project, the median and 95th percentile time of Log reads over the last 24 hours (whatever the range), and the slowest recent reads with their kind, Project and time.
- Monitors whose last run failed, and Firing Monitors, each linked.
- New Issues and Events in the range.

Each item wears a status colour with an icon and words, never colour alone.

End to end:

- EventStore tags its ClickHouse reads with a `log_comment` naming the Project and the read's kind. That covers the newest page, Log by id, tables, Charts, the histogram, Fields and SQL queries. It's set once where reads are issued, not at each call site.
- The route answers read times from `query_log`, grouped by those tags, and the oldest waiting batch from Log processing's state.
- Console adds Monitors, Issues and Events itself, Events through EventStore's existing counts, so these still show when the route fails.
- `get_overview` answers Health too.

**Blocked by:** 01

**Status:** resolved

- [x] After reads of two kinds on two Projects, `/overview` answers each Project's read times and the slowest reads with their kinds (Go route test)
- [x] A Project whose Log batches wait answers its oldest wait (Go route test)
- [x] The page's props include failing and Firing Monitors and new Issues and Events, still present when EventStore is unreachable (Rails controller test)
- [x] `get_overview` includes Health (MCP tool test)
- [x] 01's Playwright flow also sees the Health section

## Answer

Built on `overview/04-health`, merged into `feature/overview` as `4ad8b97` (2026-10-24). Details are in `~/.cache/solid-trace/overview-notes/notes.md` under "04 result".

Tests run on the merge:
- Go: `-run 'Overview|LogBatchesWait|IngestCounts|Overloaded' .` and `./handler` passed.
- Rails: the Overview health, controller and MCP tests, 21 runs, 0 failures.
- `tsc` is clean.
- The flow passed on the implementer's own stack.

Choices:
- **Events when EventStore is down.** The Overview route counts Events, per the spec's adjustment, so they're blank when EventStore is unreachable. Monitors and Issues still show.
- **Charts are tagged "table".** EventStore can't tell a Chart from an explorer table.
- **Status levels** (the spec gave none):
  - a Log batch waiting 60 s is a warning, and 10 min an error;
  - a read taking 1 s is a warning, and 10 s an error.
- **The read tag replaces the context's settings.** No tagged read sets settings of its own today. A future one will need a merge.

Open:
- The `query_log` read is untimed on production's data.
- On a phone the Health table scrolls sideways; noted for 07.
