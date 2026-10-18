# 07: SQL queries read every Organization's Logs

**Status:** resolved
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

### 2026-10-18 resolved

The read-only check now also allows only what is known to be safe (`LogStore.checkScope`, `event_store/storage/logs_sql_scope.go`), on the same `json_serialize_sql` parse. `checkReadOnly` returns the parsed SELECT; `checkScope` walks it.

**How the references parse** (DuckDB 1.5.6, probed on the parse):
- Every table reference has a `type`, an `alias` and a `sample`, and no `class` (expressions have one) and no `modifiers` (query nodes have them). The kinds: `BASE_TABLE`, `TABLE_FUNCTION`, `SUBQUERY`, `JOIN`, `EXPRESSION_LIST`, `EMPTY`, `PIVOT` (`UNPIVOT` too) and `SHOW_REF`. Any other kind is refused.
- A file path or URL in `FROM` is a `BASE_TABLE` with the path as its name, so it is refused with every name but `logs`, `events` and `WITH` names in scope.
- `DESCRIBE x`, `SHOW x` and `SUMMARIZE x` are a `SHOW_REF` holding `SELECT * FROM x`, so the same walk checks them. `SHOW TABLES` and `SHOW ALL TABLES` are a `SHOW_REF` with a `table_name` (`"tables"`, `__show_tables_expanded`) and no query: refused.
- The `PIVOT … ON` statement isn't a SELECT and was already refused. `FROM x PIVOT (…)` is a `PIVOT` reference, walked.
- `WITH` names live in each query node's `cte_map`, in order. DuckDB binds each part with only the parts before it in scope: a part that names itself or a later one reads the real table, view or file of that name. So the walk does the same (`walkWith`), and only a recursive part (`RECURSIVE_CTE_NODE`, with a `cte_name`) sees its own name. Scope passes down only, so a name used outside its node is refused. A `WITH` name must be a plain identifier, which DuckDB can't read as a file, and mustn't be a table or view in the database, the global `events` view included. The review found the first version put every name in scope for every part, which let `WITH events AS (SELECT * FROM events)` read the global view and a path-named part read files. Both are now tests.

**Scalar functions:** `current_setting` and `getvariable` read settings and variables. The `pg_catalog` macros `pg_get_viewdef` and `pg_get_constraintdef` run `duckdb_views()` and `duckdb_constraints()` when they run, which the parse doesn't show: `pg_get_viewdef` would show the views' file paths. These four are refused. The others I checked read nothing of the catalogue that matters (`current_database`, `current_schema`, `format_type`, `get_block_size`). Table macros such as `histogram(table, column)` are table functions, so they are refused.

**The events view:** a query that names `events` gets a temporary view of `<database>.main.events` for its Project and range (`createEventsView`). It is named with its database because the temporary view hides the global one: `SELECT DISTINCT project_id FROM events` answers only the Project. A query that doesn't read `events` gets no view, so it runs while the global view is missing. A query that does, while the view is missing, gets the "Events can't be read now" message, as before.

**Message:** "SQL queries can read only the logs and events views: logs.logs_hot_456 isn't allowed". It names the reference as written, or `read_parquet()`, or `SHOW TABLES`.

**Tests:**
- `event_store/logs_sql_scope_test.go`, `TestSQLThatReadsBeyondItsProjectsViewsIsRefused` has 44 cases. Hot tables, raw and qualified. `main.logs`, `temp.logs` and `main.events`. A Log file's path, a wildcard and an `s3://` URL. Each file reader, `parquet_metadata`, `duckdb_tables()`, `pragma_table_info` and a table macro. `information_schema`, `pg_class`, the `duckdb_tables` view, `SHOW TABLES` and `SHOW ALL TABLES`. `SHOW`, `DESCRIBE` and `SUMMARIZE` of another table, and the four scalar functions. A `WITH` name that is a table, one used outside its scope and one used before its `WITH`; a part that names itself (`events`) or a later part (also called `events`); and a `WITH` name that is a file path or a wildcard. Then each of those nested in a subquery, scalar subquery, join, `PIVOT`, `UNPIVOT`, `WITH` part, table function argument, lambda and `UNION`.
- `TestSQLReadsItsOwnProjectsLogsAndEvents` checks that the `events` view holds only the Project's Events of the range. The docs' and explorer's examples still run, and so do `WITH` (also named `logs`, also recursive), `logs` joined with `events`, `range`, `generate_series`, `unnest`, `VALUES`, `PIVOT`, `UNPIVOT`, `DESCRIBE logs`/`events` and `SUMMARIZE` of a query.
- The readable-SQL test still runs every summarize a chart draws.
- Changed tests: the read-only test read `logs.log_key_counts` through SQL, and now reads it through `AttributeCounts`. The autoload check moved to `storage.TestDuckDBLoadsNoExtensionByItself`, as SQL can't call `current_setting` any more.
- The Playwright flow `logs-v1-sql.spec.ts` now shows the refusal reaching the user: it runs `SELECT count(*) FROM logs.log_key_counts`, which it used to expect to work. Not run: the dev stack's EventStore runs an older binary, and the peer session asked that it not be restarted.

**Docs:** ADR 0003 has an amendment and new Consequences. Also updated: `CONTEXT.md`'s SQL query entry, `InstanceSetting::SQL_QUERIES_DESCRIPTION` (with its test), `run_sql`'s description, `docs/logs/sql.md` (what's refused, and a section on the `events` view), `docs/mcp.md`, `docs/logs/object-store.md`, and a comment on MCP 12.

