# 07: SQL queries read every Organization's Logs

**Status:** ready-for-agent
**Area:** EventStore, with Console's setting text and the docs
**Found:** 2026-10-18, while deciding [06](06-no-organization-dead-end.md)
**After:** [MCP 12](../../mcp/issues/12-sql-fetches-web-addresses-on-bucket-installs.md), which is changing the same ADR, setting text and docs (uncommitted on 2026-10-18)

## What happens

With SQL queries on, a member's SQL runs on EventStore's one Logs DuckDB connection (`LogStore.RunSQL`, `event_store/storage/logs_sql.go`). From there it can read:

- every Project's hot table, `logs_hot_<project id>`
- the `events` view, which holds every Project's Events (`DuckDBWriter.createEventsView`, `event_store/storage/duckdb.go`)
- every Log and Event Parquet file, by path or by wildcard, such as `read_parquet('…/*/*.parquet')`
- the bucket, through the secret every connection shares
- any file the server can read
- the catalogue (`duckdb_tables()`, `SHOW ALL TABLES`, `DESCRIBE logs_hot_42`), which lists other Projects' attribute keys

The `logs` view shows only the bar's Project, but nothing stops the SQL from naming anything else. The read-only check (`checkReadOnly`, `event_store/storage/logs_sql_readonly.go`) stops writes only.

Today the setting's warning is the only guard: "Turn them on only if you trust every member of every Organization." Once [06](06-no-organization-dead-end.md) makes sign-up work, a stranger who signs up could read every Organization's Logs on an install with SQL on.

**Refusing SQL that names another Organization doesn't work** (considered 2026-10-18):
- Tables are named by Project number, not by Organization.
- The `events` view mixes every Project.
- A wildcard path names nothing.

## Fix: SQL reads only its own Project

Turn the read-only check from refusing what's known to be bad into allowing only what's known to be safe. DuckDB already parses the query (`json_serialize_sql`). Walk the parse, and refuse the query unless every table reference is one of these:

- **`logs`, unqualified:** the temporary view RunSQL already makes for the bar's Project and time range.
- **`events`, unqualified:** a new temporary view of the same Project's Events in the time range, made the same way.
  - Check that it hides the global `events` view. If it doesn't, rename the global one.
- **A `WITH` part of the query itself,** used within its scope.
  - Refuse a `WITH` name that is also a table or view in the database, so a reference outside its scope can't reach the real one.
- **A subquery, join, `VALUES` list, `PIVOT` or `UNPIVOT`** over the above. Refuse any reference kind that isn't on this list.
- **A table function that reads nothing:** `range`, `generate_series`, `unnest`.

Refuse everything else, with a message that says what is allowed, such as "SQL queries can read only the logs and events views: logs_hot_42 isn't allowed". That includes:

- **Qualified names,** such as `main.logs`, `temp.logs` and `logs_db.main.logs_hot_42`.
- **File paths and URLs in FROM,** such as `FROM 'x.parquet'` and `FROM 's3://…'`.
- **Every other table function,** such as:
  - the file readers `read_parquet`, `read_csv`, `read_json`, `read_text`, `read_blob` and `glob`
  - the metadata functions `parquet_metadata`, `duckdb_tables` and `pragma_table_info`
- **`SHOW`, `DESCRIBE` and `SUMMARIZE`** of anything but `logs` or `events`, and `SHOW TABLES` and `SHOW ALL TABLES`. Find out how they parse.
- **Scalar functions that read settings, files or the catalogue,** if DuckDB 1.5.6 has any. Check `current_setting` and its kind.

The check sees only the user's SQL, not the views' own definitions, so the views' `read_parquet` keeps working.

Then update:

- **ADR 0003.** Add an amendment:
  - SQL reads only the query's Project: what is allowed and what is refused.
  - It's a check, not the sandbox the ADR calls for. That's acceptable now because `SET`, statements that run during parsing, and `query()` are already refused.
  - A DuckDB upgrade should look for new kinds of table reference, new table functions and new replacement scans.
  - Update the Consequences: the `logs` view is now a limit.
- **The SQL query entry in `CONTEXT.md`,** which says the view is "not a limit on what the SQL can read".
- **The setting's description** (`InstanceSetting::SQL_QUERIES_DESCRIPTION`). What's left: slow queries can starve ingest, and every member can read every Log in their own Organization's Projects. It stays off by default.
- **`docs/logs/sql.md` and `docs/mcp.md`:** what SQL can read.
- **MCP 12.** With file paths and URLs refused, SQL can't reach a web address at all. Say so in that ticket, or close it.

Saved SQL that reads raw tables, files or other Projects stops working, Dashboard charts included. That's expected.

## Tests

In `event_store/storage`, beside the read-only tests, each of these is refused:
- a raw hot table
- a qualified name
- the global `events` view under a qualified name
- a file path
- a wildcard
- an `s3://` URL
- each refused table function
- the catalogue, through `duckdb_tables()`, `information_schema` and `SHOW`
- `DESCRIBE` of another table
- a `WITH` name that matches a real table
- a `WITH` name used outside its scope
- each of these nested in a subquery, join and `PIVOT`

Each of these still works:
- the explorer's SQL examples
- `WITH` parts
- `logs` joined with `events`
- `range`
- `DESCRIBE logs`
- a summarize that a chart draws

One Console or MCP test should show that the refusal message reaches the user.

## Not in scope

- The structural sandbox in ADR 0003. It would be its own effort, if a check ever proves not enough.
- Slow queries starving ingest.

## Comments
