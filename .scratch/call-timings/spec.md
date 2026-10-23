# Call timings

Planned with the user on 2026-10-21, to see later which MCP tool calls and which Log reads are slow, and who runs them.

## What

Console writes one JSON line for every MCP tool call and every Log read, with how long it took. In production the line goes to stdout, and Vector ships it, as it ships every container's output, to the Meta Organization's Project, where the line's fields become Attributes ([ADR 0011](../../docs/adr/0011-meta-organization-and-vector-reads-docker-logs.md)). Development appends the lines to `console/log/call_timings.log`; tests write nothing unless a test asks.

By default only the MCP server's calls and Log reads are timed. The operator times every Log read with `instance_settings:set LOG_ALL_READS=true` (`InstanceSetting#log_all_reads`, off by default): the explorer and Dashboards refresh every 15 seconds, and each read can be many SQL lines, written back into the same EventStore.

A Log read is a Query run (`EventStore.query_logs`), a histogram, a SQL query, or a Match monitor's run (`logs_ingested_after`), from wherever it starts: the explorer, a Dashboard's charts, the chart editor, a Monitor, or an MCP tool.

## Decisions (the user, 2026-10-21)

- **stdout, not a file of its own.** Vector already reads stdout and parses a JSON line into Attributes; a file would need rotation, ownership and a new Vector source, which ADR 0011 turned down.
- **Every Log read**, each tagged with its `origin`, not only Dashboards and MCP; but (2026-10-21, after the volume was reckoned) only the MCP server's by default, the rest behind `log_all_reads`.
- **The full Query or SQL text**, and an MCP call's arguments. Only the Meta Organization's members can read them.
- **The image's release on every line**, but not its commit (2026-10-21): the commit doesn't rebuild a Query's SQL, which depends on the Logs stored then (each hour's sources, the Project's promoted columns, one step or two). EventStore logs the SQL it ran instead.

## The lines

Both kinds: `time`, `level` (`info`, or `warn` when the call didn't succeed), `message`, `kind`, `duration_ms`, `outcome`, `error` (when not ok), `user_id`, `user_email`, `user_name`, `request_id` (the web request's id, which Console's own log lines carry, or the job's id). And `version`: the release tag the Console image was built from (`release.yml` passes it), or `dev`.

- `kind: "mcp_tool"`: `tool`, `arguments` (flattened into dotted Attributes, ADR 0002). `outcome` is `ok`, `error` (the tool answered an error) or `exception`.
- `kind: "log_read"`: `read` (`query`, `histogram`, `sql`, `match`), `origin` (`logs#query`, `dashboard_charts#data`, `mcp:query_logs`, `MonitorEvaluationJob`, …), `project_id`, `query`, `from`, `to`, `rows` (Logs, table rows or buckets answered). `outcome` is `ok`, `rejected`, `timed_out`, `unavailable` or `sql_off`.

Timing is Console's: it includes the request to EventStore and parsing its answer.

## The SQL EventStore ran

Console sends each read it times `X-Log-SQL: 1`, its `request_id` as `X-Request-Id`, and its user as `X-User-Email` and `X-User-Name` (percent-encoded, added 2026-10-23). Only then does EventStore record every statement a run (Query, Match monitor, histogram, SQL) makes DuckDB run (`storage.WithReadStatements`), and when the run ends logs one line per statement: `message="Log read SQL"`, `kind=log_read_sql`, `read`, `project_id`, `request_id`, `statement` (1, 2, …) of `statements`, `duration_ms` (DuckDB's run of the statement), `sql`, `user_email` and `user_name`, with `error` at `warn` when it failed. For SQL queries the statements are the `logs` view's definition, then the query. Vector makes the numbers numbers. A long statement is cut at 64 KB by Log ingest. `read` is `query`, `match` (a Match monitor's `after_batch` run), `histogram` or `sql`.

## Not now

- EventStore's own timings per stage.
- A Dashboard in the Meta Organization over these lines.
