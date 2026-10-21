# Call timings

Planned with the user on 2026-10-21, to see later which MCP tool calls and which Log reads are slow, and who runs them.

## What

Console writes one JSON line for every MCP tool call and every Log read, with how long it took. In production the line goes to stdout, and Vector ships it, as it ships every container's output, to the Meta Organization's Project, where the line's fields become Attributes ([ADR 0011](../../docs/adr/0011-meta-organization-and-vector-reads-docker-logs.md)). Development appends the lines to `console/log/call_timings.log`; tests write nothing unless a test asks.

A Log read is a Query run (`EventStore.query_logs`), a histogram, a SQL query, or a Match monitor's run (`logs_ingested_after`), from wherever it starts: the explorer, a Dashboard's charts, the chart editor, a Monitor, or an MCP tool.

## Decisions (the user, 2026-10-21)

- **stdout, not a file of its own.** Vector already reads stdout and parses a JSON line into Attributes; a file would need rotation, ownership and a new Vector source, which ADR 0011 turned down.
- **Every Log read**, each tagged with its `origin`, not only Dashboards and MCP.
- **The full Query or SQL text**, and an MCP call's arguments. Only the Meta Organization's members can read them.

## The lines

Both kinds: `time`, `level` (`info`, or `warn` when the call didn't succeed), `message`, `kind`, `duration_ms`, `outcome`, `error` (when not ok), `user_id`, `user_email`, `request_id` (the web request's id, which Console's own log lines carry, or the job's id).

- `kind: "mcp_tool"`: `tool`, `arguments` (flattened into dotted Attributes, ADR 0002). `outcome` is `ok`, `error` (the tool answered an error) or `exception`.
- `kind: "log_read"`: `read` (`query`, `histogram`, `sql`, `match`), `origin` (`logs#query`, `dashboard_charts#data`, `mcp:query_logs`, `MonitorEvaluationJob`, …), `project_id`, `query`, `from`, `to`, `rows` (Logs, table rows or buckets answered). `outcome` is `ok`, `rejected`, `timed_out`, `unavailable` or `sql_off`.

Timing is Console's: it includes the request to EventStore and parsing its answer.

## Not now

- EventStore's own timings per stage.
- A Dashboard in the Meta Organization over these lines.
