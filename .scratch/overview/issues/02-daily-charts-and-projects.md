# 02: Daily Charts and Projects

Spec: [../spec.md](../spec.md)

**What to build:** Below the rates, the Overview shows three daily Charts for the range, each with its table below:

- Logs per day, stacked by Project.
- Received and stored bytes per day, as grouped bars on one axis.
- Average and peak Logs per second per day, as two lines.

Each Project takes a Chart colour in creation order, never cycled, and keeps it whatever the range. The eighth Project and on fold into Other. Every bar and point has a tooltip. The Charts follow the Console's dataviz rules (4px rounded tops, 2px gaps, 2px lines, crosshair, legend, direct labels up to four series) and sit two across on wide screens, stacked on a phone.

A **Projects** table follows. Each row shows the Project's:

- Logs, received and stored bytes, ratio and share of the total
- Average and peak Logs per second
- Retention (the instance default when unset) and oldest day kept
- Total stored Logs and size across every day kept
- When its last Log arrived, marked when that's over an hour ago

A Project with no Logs says so and links to its setup. A row opens that Project's Logs. The route and `get_overview` answer the daily series and the Projects too.

**Blocked by:** 01

**Status:** resolved

- [x] `/overview` answers the daily series (Logs by Project, received and stored bytes, average and peak per second) and each Project's totals, oldest day and last arrival (Go route test)
- [x] Project colours follow creation order and fold into Other past seven (Rails or vitest test on the builder's output, whichever owns the order)
- [x] `get_overview` includes the daily series and Projects (MCP tool test)
- [x] 01's Playwright flow also sees both Projects in the table, with the stale mark on one whose last Log is over an hour old

## Answer

Built on `overview/02-daily-charts-and-projects`, merged into `feature/overview` as `5ff7c86` (2026-10-24). The route and builder shapes are in `~/.cache/solid-trace/overview-notes/notes.md` under "02 result".

Tests run on the merge:
- Go: the Overview tests and `./handler` passed.
- Rails: the Overview controller and MCP tool tests, 17 runs, 0 failures.
- `tsc` is clean.
- The flow passed on the implementer's own stack.

Choices:
- Logs per day count Logs as received, by arrival day.
- Share is each Project's share of bytes received.
- The ratio is received over stored for now; 03 changes it to kept over stored.
- The flow back-dates a third Project's arrival in ClickHouse to get a stale row, so it needs `E2E_CLICKHOUSE_DATABASE`.
- A Project's totals across every day kept cover ClickHouse only, not old Parquet files.

Look issues are listed for 07 in notes.md.
