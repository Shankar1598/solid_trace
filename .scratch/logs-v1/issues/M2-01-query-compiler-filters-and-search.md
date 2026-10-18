# M2-01: Query compiler with filters and search

**What to build:** the first slice of the Query language end to end. EventStore compiles a Query of `where` equality filters and `search` to DuckDB SQL and runs it for a Project and time range, newest first with paging; validates a Query; and fetches one Log. This replaces M1-01's latest-N operation as the read path. Spec: [Query compiler and query endpoints](../spec.md#query-compiler-and-query-endpoints-eventstore); [ADR 0002](../../../docs/adr/0002-logs-scoped-by-project-flat-attributes.md); user stories 40, 46, 47, 54, 77, 78.

**Blocked by:** M1-08.

**Status:** resolved

- [x] A hand-written Go compiler in EventStore, borrowing `runreveal/pql`'s structure, is the single path from a Query to SQL. This ticket's surface: an optional leading `logs |`; `where` with `==`, `!=`, `and`, `or`, `not`; `search`. Unknown stages and functions are rejected with a position.
- [x] Semantics: `==` never coerces (`status == 200` matches numbers, `status == "200"` strings); `!=` includes Logs missing the Field; `search` is a case-insensitive `contains` over `message` and every string Attribute. The Project and time range come from the request.
- [x] Columns come only from the cached promoted-set table, one branch per day: the column for promoted keys, `coalesce(column, map[key])` for keys added during that day (that day only), maps only for `logs_late`. No Parquet footer reads.
- [x] "Latest N" scans newest first, hour by hour, stopping once it has N rows; older pages continue from a (time, id) cursor.
- [x] Query-app operations: **run a Query** (Log rows plus a next-page cursor); **validate a Query** (errors with line and column; and when the Query is only `where field ==|!= literal` stages and one `search`, its chips); **fetch one Log** by id. Routing, cancel and result caps are the implementer's call ([ticket 15](../../logs/issues/15-how-log-queries-run.md)); no per-query timeouts.
- [x] Tests act as a user: rows returned for each operator across hot table, Parquet and `logs_late`; same rows before and after an export and a promotion; paging with the cursor; a rare-key search returns matches newest first; validate positions and chips (and no chips for a non-Builder Query); fetch by id.

## Comments

### 2026-10-03 built (logs-v1/m2-01)

What was built:
- **`logquery/parser`**: the scanner (`lex.go`), the syntax tree (`ast.go`, every node knows its `Span`) and a recursive-descent parser with precedence climbing (`parser.go`), after `runreveal/pql`'s structure (not vendored). It knows the grammar only.
  - `Parse(source) (*Query, error)`. `Query{Source *Ident, Stages []Stage}`. Stages are `WhereStage` and `SearchStage`. Exprs are `Ident`, `BasicLit` (string, number, bool), `BinaryExpr`, `UnaryExpr`, `ParenExpr` and `CallExpr`.
  - The parser already reads every binary operator (`== != < <= > >= + - * / %`, `and`, `or`), calls with any arguments, and unary minus. Only the stage keywords are fixed in `stage()`. M3-01 adds its stages there.
  - A dotted name (`http.status`, with no spaces) is one `Ident`, since Attribute names are flat. `['any name']` or `["…"]` quotes a name. `true` and `false` are literals. `not` is KQL's function form, `not(…)`. `//` starts a comment.
  - It stops at the first mistake: `*parser.Error{Span, Message}`. `PositionOf(source, offset)` gives the line and column, both from 1, the column counted in characters.
- **`logquery`** (the compiler):
  - `Compile(source) (*Query, error)` checks the tree (`compile.go`) and gives typed stages: `whereStage{predicate}` and `searchStage{term}`. Predicates are `compare{Field, op, Literal}`, `and`, `or` and `not`. Unknown stages and functions, a comparison that isn't Field-vs-literal, and any other operator are errors at their span.
  - `(*Query).Where(storage.LogColumns) string` is the SQL emitter (`sql.go`). It writes the stages as one condition against one source of Logs.
  - `(*Query).Chips() *Chips` and `ProblemOf(source, err) Problem` are in `chips.go`.
- **Semantics:**
  - Conditions are two-valued. Negation is pushed down to the comparisons, so `==` is `col = lit` (NULL keeps the Log out) and `!=` is `col IS DISTINCT FROM lit`. `not(a == b)` is exactly `a != b`.
  - `==` never coerces. The literal's type picks the Attribute's type (`status == 200` reads the number Attribute, `"200"` the string one). A built-in field compared with a non-string is constant `false` (`!=`: `true`).
  - `time` can't be compared yet (error). The range comes from the request, and M3-01 adds narrowing.
  - `search "t"` is `ILIKE '%t%'` (wildcards escaped) over `message`, every value of `attrs_string`, and the day's promoted string columns. Several stages are ANDed.
- **`storage.LogColumns`** says where one source keeps each Field: `BuiltIn(name)`, `Map(type)`, `Attribute(type, key)` (the column for a promoted key, `coalesce(column, map_extract_value(map, key))` when `Partway`, else the map) and `Promoted(type)`. For `logs_late` it gives maps only. It is built from the cached promoted set of the hour's day, so there are no schema or footer reads.
- **`LogStore.ScanLogs(ctx, LogScan{ProjectID, From, To, After *LogPosition, Where func(LogColumns) string, Limit})`** is "latest N" (`storage/logs_query.go`):
  1. It lists the hours that can hold Logs. These are the hour folders in range and the hours between `min(time)` and `max(time)` of the hot table plus `logs_late`, all read under one `publishMu` read lock.
  2. It runs one query per hour, newest first, and stops at `Limit`. Each query unions the hot table, the Project's `logs_late` rows and that hour's files (`LogFiles.Hour`) by name, each with its own `Where`. It runs under its own `publishMu` read lock, so an hour is never seen twice or not at all.
  3. The lock is never held across hours, so a long search doesn't stall the export (which holds `writeMu` while it waits for `publishMu`).
  - Rows are `ORDER BY time DESC, id DESC`. `After` continues strictly after a (time, id).
  - A source can lack a column of its day's set: a demoted column dropped from the hot table, or a file written before a key joined the day's set. The source is then unioned by name with an empty row of typed NULL columns (`withColumns`). DuckDB still pushes filters into the scans (checked with `EXPLAIN`).
- **`LogStore.LogByID(ctx, project, id, at *time.Time)`** searches the hot table, `logs_late` and the files of `at`'s hour, or every file of the Project without `at`. `LatestLogs` is gone.
- **Query-app routes** (`handler/logs.go`), replacing `GET /logs/latest`:
  - `POST /api/:project_id/logs/query`: request `{"query", "from", "to", "limit"?, "cursor"?}` (RFC 3339; `[from, to)`); limit defaults to 200, capped at 1,000. It answers `{"logs": […], "next_cursor": string|null}`. `next_cursor` is null when a page comes back short. A Query with mistakes gets `400 {"errors": [Problem]}`. Missing or reversed range, a bad cursor or a negative limit gets `400 {"error"}`.
  - `POST /api/:project_id/logs/validate`: request `{"query"}`. It always answers `200 {"errors": [Problem], "chips": Chips|null}`. `Problem` is `{message, line, column, end_line, end_column}`. `Chips` is `{"filters": [{"field", "op": "=="|"!=", "value": string|number|bool}], "search": string|null}`.
  - `GET /api/:project_id/logs/:log_id?time=` answers the Log (same JSON as in a page), 404 if not found, or 400 for a bad id.
  - The cursor is opaque: base64url of `RFC3339Nano|uuid`.
- **Tests:** `logs_query_test.go`. The test helper `latestLogs` now runs the empty Query over 2000–2100, so every earlier Log test reads through the compiler. `handler/routes_test.go` checks that the new POST routes are on the query app only.

Choices the ticket left open:
- **Chips** require every stage to be `where <Field> ==|!= <literal>` (one comparison per stage; parentheses around it are fine) and at most one `search`. `and` inside one `where` gives no chips, following CONTEXT.md's Builder strictly.
- **Cancel:** the scan checks `ctx` between hours and runs DuckDB queries with it. But the handlers pass `c.UserContext()`, and fasthttp gives no signal when a client disconnects, so a cancelled run in the browser doesn't stop the server's scan. It ends at `limit` Logs or the end of the range. Stopping on disconnect would need a cancel endpoint or a different server. Left for M2-03 to decide if it matters.
- **Caps:** 1,000 Logs per page and no cap on the range. An empty hour costs nothing because only hours with files or unexported Logs are scanned.
- **Fetch by id** takes an optional `time` hint because a UUIDv7 says when a Log was received, not its `time`. Without the hint it opens every file of the Project.
- **Errors:** the parser stops at the first mistake, so validate lists at most one. The response is a list so later tickets can report more.

Deviations: none from the spec. "Columns come only from the promoted-set table" holds for every condition. Rows are read with `SELECT *` and promoted columns merged back by name, as latest-N did.

Follow-ups:
- **M2-02:** histogram and summary need whole-range aggregates, not the newest-N scan. Build them on `LogColumns` and `Query.Where`, one branch per day (or hour) over the same three sources, with `withColumns` for missing columns.
- **M3-01:**
  - Add stages in `parser.stage()` and `compileStage`, and operators and functions in `compilePredicate`. Comparisons other than `==`/`!=` already parse.
  - `contains`, `in`, `!in` and the other word operators need scanner and parser support: `!` alone is an error token today, and a word after an expression ends it.
  - Timespans (`1h`) are currently a scan error ("isn't a number").
  - Non-Log results need a different runner than `ScanLogs`.
- **M2-03:** send `from`/`to` as UTC RFC 3339. Use `next_cursor` for older pages, and the Log's own `time` as the `time` hint on fetch.
- **M1-10:** measure `ScanLogs`. It runs a query per hour with Logs (each with a catalog lookup and a glob), and the `min`/`max(time)` over the hot table per run.

### 2026-10-03 code review

- **Reads hold no lock while they run:** each read is a DuckDB snapshot plus pinned files (`logRead`).
- **`ScanLogs` returns a `LogScanPage`** and stops after its budget with a next position.
- **`contains` also reads an Attribute's JSON text, for arrays.**

See the spec's 2026-10-03 code review entry, items 1, 3 and 7.
