# 16: How does the explorer show non-Log results and keep its state?

Type: grilling
Status: resolved
Blocked by:
Map: [SolidTrace Logs](../map.md)

## Question

The explorer layout ([ticket 06](06-logs-explorer-layout.md)) and the query modes ([ticket 12](12-query-builder-ux.md)) cover a stream of Logs. How are Query results that are not Logs shown (a `summarize` table, a `by bin(time, …)` series, `distinct`, `top`), and are they charted? Does the explorer's state (Project, time range, mode, Query text) live in the URL? Which library choices for the virtualised stream, the histogram and the inspector drawer belong in the spec, and which are left to implementation?

## Answer

Resolved 2026-10-02 (grilling). Each decision is a comment below.

- **URL:** the time range (as given), the mode, the Query text and the SQL text; the Project is in the path. Each run pushes a history entry. SQL never runs on page load, only on an explicit Run.
- **Non-Log results:** a table only, in place of the stream. Charts for `summarize … by bin(time, …)` wait for dashboards (v2).
- **Histogram:** always shown, counting the Logs that passed the filters.
- **Stream:** the newest 200 Logs, then 200 older per scroll by a (time, id) cursor, at most about 10,000.
- **Summary line:** Log count, per-level counts (click to filter), error rate = (error + fatal) ÷ Log count. No p95.
- **Time zone:** the browser's local time on screen; UTC ISO in the URL and the inspector.
- **Errors:** compile errors under the Query box with line and column, keeping the last result; run errors replace the result area; a Cancel button while a Query runs.
- **Libraries:** left to the implementer to research and choose, compatible with shadcn (base-ui) and the existing toolset (React 19, Inertia 2, Tailwind). The spec states only the requirements: smooth with 10,000 lines; the inspector overlays the stream; the histogram as in the prototype.

## Comments

### 2026-10-02 grilling

- **URL state:** the time range (as given; relative stays relative), the mode, the Query text and the SQL text live in the URL; the Project is already in the path. Each run pushes a history entry; typing does not. **SQL never runs on page load**, only on an explicit Run, so a crafted link cannot run SQL under another member's session.
- **Non-Log results** (`summarize`, `project`, `distinct`, `count`, `top`) show a **table only** in v1, in place of the stream. Charting a `summarize … by bin(time, …)` waits for dashboards (v2).
- **The histogram stays** above non-Log results, counting the Logs that passed the filters; drag-to-zoom still sets the time range.
- **Stream loading:** the newest 200 Logs first; reaching the bottom fetches the next 200 older by a (time, id) cursor; at most about 10,000 Logs kept, then "Narrow the time range to see older Logs".
- **Summary line:** Log count, per-level counts (click to filter), and error rate = (error + fatal) ÷ Log count. No p95 duration: a Log has no duration field ([ticket 07](07-log-domain-model.md)).
- **Libraries** for the virtualised stream, the histogram and the inspector drawer are not chosen in the spec: the implementer researches and decides. They must fit shadcn (base-ui) and the existing toolset (React 19, Inertia 2, Tailwind). Requirements only: smooth with 10,000 lines; the inspector overlays the stream.
- **Time zone:** the browser's local time everywhere (stream, histogram labels, range picker). Absolute ranges in the URL are UTC ISO timestamps; the inspector also shows the raw `time` in UTC ISO.
- **Errors:** compile errors appear under the Query box with line and column, and the last good result stays; run errors (from DuckDB) replace the result area. A running Query shows a Cancel button, the only way to stop one while there are no timeouts; whether the server stops it is [ticket 15](15-how-log-queries-run.md)'s.
