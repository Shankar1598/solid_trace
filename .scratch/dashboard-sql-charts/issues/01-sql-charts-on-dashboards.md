# 01: SQL Charts on Dashboards, with filters

**Status:** needs-triage

Filed on 2026-10-21 while planning [Dashboard filters](../../dashboard-filters/spec.md). The user wants filters in SQL Charts too, but Dashboards can't hold SQL Charts yet, so that is its own effort.

**Today:** a Dashboard Chart is Query text only (`dashboard_charts.query`). The Chart editor has a Builder and Query switch with no SQL tab, and `ChartRuns` runs Queries only. SQL queries run only in the Logs explorer (`EventStore.run_sql`, `storage/logs_sql.go`), through the read-only check, and stop after 10 seconds.

**Idea so far:**

- A Chart stores SQL or a Query. The Chart editor gains the SQL tab, on installs where SQL queries are on. SQL Charts get no bin cache.
- In SQL, `$service` is a bound DuckDB parameter: a string for a select, a list for a multiple select, NULL for All. The SQL writes the All case itself, as in `($service IS NULL OR service = $service)`, since SQL can't drop a comparison.
- Check that the read-only check accepts the parameters, and how go-duckdb binds a list.

To settle with the user: whether SQL Charts need the instance setting that SQL queries do, and the Chart tools' `sql` argument on the MCP server.

## Comments
