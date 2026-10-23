# 07: SQL queries on ClickHouse, isolated per Project

**What to build:** An SQL query is ClickHouse SQL, run as its Project's read-only ClickHouse user. Row policies keep it to its Project and the explorer's range, and grants and limits keep it off everything else. See the [spec](../spec.md)'s "SQL queries".

**Blocked by:** 04, 06

**Status:** resolved

- [x] **Each Project has an SQL user**, made when first needed:
  - row policies on `logs` and `events` limit it to its Project, and to the explorer's range through settings the policies read;
  - a settings profile with constraints caps execution time, memory, threads and result size;
  - it gets no grants for table functions (`s3`, `file`, `url`, `remote`, …) or system tables.

  EventStore runs each SQL query as that user. Record the mechanism chosen in the ADR (10).
- [x] **The schema SQL sees:** `logs` (built-in fields, `id`, `attributes` as JSON, the JSON-text map) and `events` (nine columns). Old Log files aren't visible.
- [x] **What goes:** the read-only and scope checks on DuckDB's AST and the slow-text-match notice. `handler`'s DuckDB type-name mapping becomes ClickHouse's.
- [x] **Errors** pass through with ClickHouse's text and position. Console's `errorParts.ts` parses ClickHouse's position, and `Logs/Index.tsx` still marks the spot. vitest covers the parser.
- [x] **Docs and descriptions:** `docs/logs/sql.md`, `run_sql.rb`'s description, `instance_setting.rb`'s text and `docs/mcp.md` describe ClickHouse SQL and the tables. Amend ADR 0003 (the isolation is structural now) in this ticket.
- [x] **Root tests:**
  - `logs_sql_*_test.go` and `logs_sql_scope_test.go` are rewritten around what a query can see, not which text is refused;
  - an SQL query can't read another Project, a file, a URL, the bucket or a system table;
  - it stops at its time limit;
  - it can't raise its limits with `SETTINGS`;
  - `logs` and `events` show the explorer's range.

  `logs_language_test.go`'s Readable SQL run through SQL mode is turned back on.
- [x] **Console:** `logs_controller_test.rb` and `mcp/logs_tools_test.rb` pass. The `logs-v1-sql` flow passes on the dev stack (touch the edited `.tsx` files first: Vite can serve stale ones).
- [x] Targeted tests only.

## Answer

Built on `clickhouse/07-sql-queries-per-project`, from `feature/clickhouse` at 996c3b7, with ticket 05's tip (52124d0) merged in before the last runs. Notes for 10 (the mechanism in detail, what a SQL user can still do, what's left) are in `~/.cache/solid-trace/clickhouse-notes/07-notes.md`.

**For the user:**
- **SQL queries are on by default today, not off.** ADR 0003's amendment of 2026-10-18 turned them on, and the spec's "still off by default" doesn't match that. This ticket left the default alone, and the docs say it's on.
- **The dev stack ran this branch for the flow and is back on `dashboard-filters`.** The dev ClickHouse's `solidtrace` database now holds the flows' dummy Logs and the user `solidtrace_sql_1`.

**How it works:**
- **Each Project gets its SQL user the first time a process runs that Project's SQL** (`storage/clickhouse_sql.go`). The user's password is random and lives only in that process's memory. EventStore connects as the user and runs the SQL as written.
  - This uses credentials rather than impersonation. With `EXECUTE AS`, the user's SQL would have to be written into a statement of EventStore's own.
  - On an authentication failure, EventStore makes the user again and retries once.
- **Row policies:** on `logs` (by `time`) and on `events` (by `timestamp`), they show only the Project's rows. The range comes from the custom settings `SQL_from` and `SQL_to`, which a query may widen. Partition and key pruning still use it.
- **Settings profile:** `readonly = 1` and fixed limits:
  - 10 s;
  - 1 GiB of memory, and 1 GiB of spill to disk;
  - half the CPUs;
  - 100,000 result rows (`break`).

  Read-only, a user can change no setting but the range.
- **Grants:** SELECT on `logs` and `events`, nothing else.

**Gotchas:**
- clickhouse-go sends `max_execution_time` itself whenever the context has a deadline. A read-only user can't set it, so `RunSQL` passes a context without a deadline that still cancels.
- clickhouse-go 2.49 can't decode a JSON column that 26.8 sends after a sort has merged several parts. The profile fixes `output_format_native_use_flattened_dynamic_and_json_serialization = 1`. **EventStore's own reads don't set this.** Its Log reads all have a LIMIT, and none failed, but **11** should keep an eye out for "unsupported column type".
- Readable SQL now writes a value of no fixed type as `::Dynamic`, so SQL mode returns the Query's values. A JSON value comes back as its JSON text. Tables in no order of their own are sorted by their values, since a Dynamic can't be a sort key.

**Tests:**
- Rewritten: `logs_sql_test.go`, `logs_sql_scope_test.go` and `logs_sql_readonly_test.go`. They check what SQL sees: its Project and range, and the range widened. They also check what is refused:
  - another Project;
  - EventStore's other tables;
  - system tables and `information_schema`;
  - `file()` (both forms), `url`, `s3`, and `remote` as `default`;
  - `EXECUTE AS`;
  - every write;
  - every limit raised with `SETTINGS` or `SET`.
- Also checked:
  - the time limit gives a 504;
  - the memory limit stops the query;
  - a syntax error carries ClickHouse's own message and position;
  - `attributes` and `attrs_json` read back across parts.
- `sqlOnClickHouse` is gone, so the language, text-match and run-logging tests run the Readable SQL through SQL mode again. `logs_reads_test.go`'s long read is a ClickHouse query.
- Console: vitest covers `errorParts`. The Rails tests (`logs_controller_test`, `mcp/logs_tools_test`, `mcp/base_test`, `instance_settings_task_test`) pass, and so do all 5 tests in the `logs-v1-sql` flow on the dev stack.

**Left for 10:**
- GLOSSARY's **SQL query** entry.
- ADR 0016 should cite this mechanism.
- `duckdb_bucket.go`'s comment names the deleted `checkReadOnly`.
- `LogColumns.view` is unused.
- A deleted Project's SQL user and its row policies stay behind. They're harmless, and Cleanup could drop them.
