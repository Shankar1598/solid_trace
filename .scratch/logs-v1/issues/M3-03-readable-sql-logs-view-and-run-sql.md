# M3-03: Readable SQL, the logs view and Run SQL

**What to build:** on an instance with SQL queries on, EventStore runs a member's raw DuckDB SQL against a documented `logs` view for the Project and time range, and prints any Query as readable SQL against that view. Spec: [Query compiler and query endpoints](../spec.md#query-compiler-and-query-endpoints-eventstore) (Readable SQL, Run SQL); [ADR 0003](../../../docs/adr/0003-unrestricted-sql-queries-behind-an-instance-setting.md); user stories 67–69.

**Blocked by:** M1-02, M3-01.

**Status:** resolved

- [x] The compiler prints a Query as readable SQL against the `logs` view, one CTE per stage.
- [x] A query-app operation returns that readable SQL for a Query.
- [x] Run SQL, only when the instance setting is on (refused otherwise): EventStore builds `logs` as a temporary view on that query's connection, covering the Project's hot table, its days in Parquet and its `logs_late` rows, with promoted columns merged back into the four maps, and runs the SQL as written. The view's columns: `project_id`, the built-in fields, the Attributes maps.
- [x] The `logs` view is documented for users.
- [x] Cross-check test: for a set of Queries (filters, search, summarize, top, distinct), the readable SQL run through Run SQL returns the same rows as the Query.
- [x] Tests: Run SQL refused while the setting is off; the view covers exactly the requested Project and range across hot, Parquet and late rows.

## Comments

### 2026-10-03 built (logs-v1/m3-03)

What was built:
- **`logs` view** (`storage/logs_sql.go`). `LogStore.RunSQL(ctx, projectID, from, to, sql, limit) (SQLResult, error)`:
  - It builds `CREATE TEMPORARY VIEW logs` on a connection of its own, then runs the SQL as written on that connection.
  - The view has these columns: `project_id` (UINTEGER), `id`, the seven built-in fields, and `attrs_string`/`attrs_number`/`attrs_bool`/`attrs_json`. Each day's promoted columns are merged back into their maps with `map_concat(map, map_from_entries(list_filter([...], non-null)))`.
  - It covers the Project's `logs_late` rows, its hot table and its hour files in `[from, to)`, using the same sources as `ScanLogTable`. Those sources are now built by `logSourcesLocked(project, from, to, sel, where)`, which `ScanLogTable` also uses. Its queries are unchanged.
  - It holds `publishMu`'s read lock for the whole run, as table queries do, so the view sees each Log exactly once. An export waits for a running SQL query.
  - Afterwards, the connection is thrown away (`conn.Raw` returning `driver.ErrBadConn`). Whatever the SQL leaves behind, such as temporary objects, settings on the connection or an open transaction, never reaches the store's pool.
  - `SQLResult{Columns []SQLColumn{Name, Type (DuckDB type name)}, Rows (JSON-ready values), Truncated}`. Only the first `limit` rows are read. A failure of the SQL itself is a `*storage.SQLError`.
- **`storage.LogViewColumns()`** is the exported maps-only `LogColumns`. It writes `attrs_number['key']`, not `map_extract_value(...)`, so the SQL is easier to read. DuckDB 1.5's `map['key']` returns the value.
- **`(*logquery.Query).ReadableSQL()`** (`logquery/readable.go`) is the Query as SQL against `logs`, with one CTE per stage: `stage1`, `stage2` and so on, head stages included. `TableSQL`'s stage loop moved into `writeTail(chain, fields)`, which both `TableSQL` and `ReadableSQL` use. `TableSQL`'s output is unchanged.
  - **A Log-rows Query** ends in `SELECT * FROM stageN ORDER BY "time" DESC, "id" DESC [LIMIT take]`. Its result has the view's columns.
  - **A table Query** ends in the output columns. Its tail reads Attributes straight from the maps, because the rows keep every view column until `project`/`summarize`. A dynamic output column is written as JSON: `CAST(coalesce(to_json(s), to_json(n), to_json(b), CAST(j AS JSON)) AS JSON)`. Through Run SQL that gives the same JSON value a run gives, rather than the `{s,n,b,j}` struct. No row limit.
- **Query-app routes** (`handler/logs_sql.go`):
  - `POST /api/:project_id/logs/sql`. Request `{"sql", "from", "to"}`. It answers `403 {"error"}` while `SQLQueriesEnabled` is off. Otherwise it answers like a run's table, `{"table": {"columns": [{"name","type"}], "rows", "truncated"}}`, with at most 10,000 rows. Failing SQL answers `400 {"error": <DuckDB message>}`. Column types map DuckDB types onto the table's `string`/`number`/`bool`/`datetime`/`timespan`/`dynamic` (maps, lists, structs and JSON are `dynamic`).
  - `POST /api/:project_id/logs/readable-sql`. Request `{"query", "from", "to"}`. It answers `{"sql"}`, or `400 {"errors": [Problem]}` as a run does.
- **Wiring:** `NewLogsHandler(logs, settings handler.InstanceSettings, clock)`. `main.go` and the test env pass `*storage.ConsoleSettings`, and `routes_test.go` passes a stub with SQL on.
- **Docs:** `docs/logs/sql.md` covers the view's columns, reading Attributes, results, the readable SQL's shape, and the ADR 0003 warning.
- **Tests:** `logs_sql_test.go`. Run SQL is refused before the setting is set and while it's off, and a bad column gives 400. The view covers exactly the requested Project and range across files, hot rows and a late Log, with promoted values read back from the maps. Another Project's Logs are excluded. The cross-check covers 9 Log-row Queries (filters, search, take, sort) and 18 table Queries (summarize, top, distinct, project, extend, sort, count, `ago()`): readable SQL run through Run SQL returns the same rows. Unordered tables are compared as sets. I checked that it goes red when the head is dropped.

Choices the ticket left open:
- **`id` is in the view**, beside `project_id` and the built-ins. A newest-first order needs it, and so does linking a row back to a Log.
- **Readable SQL ignores the setting.** It runs nothing, and Console hides SQL mode while the setting is off (M3-04). Only Run SQL is refused.
- **Readable SQL takes `from`/`to`** like a run, because `bin_auto` sizes its bins from the range. `ago()`/`now()` are folded to literal timestamps.
- **Unordered tables:** both a run and readable SQL end in `ORDER BY ALL`. Readable SQL orders by the JSON form of dynamic columns, so rows with equal sort keys can come back in a different order than a run's. The rows are the same.
- **Result cap:** 10,000 rows, the same as table runs.

Deviations: none from the spec.

Follow-ups:
- **M3-04:** seed SQL mode from `readable-sql` and run SQL with `sql`. A table answer has the same shape as a run's, so the table component can be reused. Console must still refuse and hide SQL while `InstanceSetting.current.sql_queries_enabled?` is false. EventStore's 403 is the second check.
- **Cancel:** like other runs, a client going away doesn't stop the SQL on the server (fasthttp gives no signal), and the publish read lock stays held until the query ends. A runaway SQL query therefore delays the hourly export. That fits ADR 0003 ("starve ingest"). If it matters, add a cancel endpoint later.
- **M1-10:** the view unions every day's sources with merged maps. A `SELECT` over a long range touches every file. Not measured.
