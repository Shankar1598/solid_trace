# 07: SQL queries on ClickHouse, isolated per Project

**What to build:** An SQL query is ClickHouse SQL, run as its Project's read-only ClickHouse user. Row policies keep it to its Project and the explorer's range, and grants and limits keep it off everything else. See the [spec](../spec.md)'s "SQL queries".

**Blocked by:** 04, 06

**Status:** ready-for-agent

- [ ] **Each Project has an SQL user**, made when first needed:
  - row policies on `logs` and `events` limit it to its Project, and to the explorer's range through settings the policies read;
  - a settings profile with constraints caps execution time, memory, threads and result size;
  - it gets no grants for table functions (`s3`, `file`, `url`, `remote`, …) or system tables.

  EventStore runs each SQL query as that user. Record the mechanism chosen in the ADR (10).
- [ ] **The schema SQL sees:** `logs` (built-in fields, `id`, `attributes` as JSON, the JSON-text map) and `events` (nine columns). Old Log files aren't visible.
- [ ] **What goes:** the read-only and scope checks on DuckDB's AST and the slow-text-match notice. `handler`'s DuckDB type-name mapping becomes ClickHouse's.
- [ ] **Errors** pass through with ClickHouse's text and position. Console's `errorParts.ts` parses ClickHouse's position, and `Logs/Index.tsx` still marks the spot. vitest covers the parser.
- [ ] **Docs and descriptions:** `docs/logs/sql.md`, `run_sql.rb`'s description, `instance_setting.rb`'s text and `docs/mcp.md` describe ClickHouse SQL and the tables. Amend ADR 0003 (the isolation is structural now) in this ticket.
- [ ] **Root tests:**
  - `logs_sql_*_test.go` and `logs_sql_scope_test.go` are rewritten around what a query can see, not which text is refused;
  - an SQL query can't read another Project, a file, a URL, the bucket or a system table;
  - it stops at its time limit;
  - it can't raise its limits with `SETTINGS`;
  - `logs` and `events` show the explorer's range.

  `logs_language_test.go`'s Readable SQL run through SQL mode is turned back on.
- [ ] **Console:** `logs_controller_test.rb` and `mcp/logs_tools_test.rb` pass. The `logs-v1-sql` flow passes on the dev stack (touch the edited `.tsx` files first: Vite can serve stale ones).
- [ ] Targeted tests only.
