# M3-01: The rest of the Query language and table results

**What to build:** the compiler covers the whole v1 language, and Queries that end in a tabular stage return a table instead of Log rows. Spec: [Query compiler and query endpoints](../spec.md#query-compiler-and-query-endpoints-eventstore) (The language, Run a Query); user stories 53, 58–60, 63, 66.

**Blocked by:** M2-01.

**Status:** resolved

- [x] `where` gains comparisons (`<`, `<=`, `>`, `>=`), `contains` (case-insensitive), `contains_cs`, `startswith`, `matches regex`, `in`/`!in` (`!in` includes Logs missing the Field), `isempty`/`isnotempty`.
- [x] Stages: `project`, `extend`, `summarize` with `count`, `dcount`, `sum`, `avg`, `min`, `max`, `percentile`, by Fields and `bin(time, …)`; `sort`, `take`, `top`, `count`, `distinct`, `search`. Unknown functions rejected with a position. KQL's semantics win where KQL and APL differ; common pasted Axiom queries work.
- [x] A `where` on `time` may narrow the request's range, never widen it; nothing leaves the Project.
- [x] Run a Query returns a table (columns and rows) for a Query ending in `summarize`, `project`, `distinct`, `count` or `top`; Log rows with a cursor otherwise.
- [x] `dcount` and `percentile` are documented as estimates where the language is documented for users.
- [x] Tests: each operator and function by the rows or table it returns; narrowing but not widening the range; a few pasted Axiom-style queries.

## Comments

### 2026-10-03 built (logs-v1/m3-01)

What was built:
- **Parser** (`logquery/parser`):
  - The scanner now reads `=~`, `!~` and `!`, timespans (`30s`, `5m`, `1h`, `2d`, `100ms`, KQL's unit spellings, via `ParseTimespan`) and `datetime(…)`, quoted or not, as one token.
  - Word operators (`contains`, `contains_cs`, `startswith`, `startswith_cs`, `in`, each with a `!` form, and `matches regex`) bind like comparisons. The right side of `in` is a `ListExpr`.
  - New stage nodes: `ProjectStage`, `ExtendStage`, `SummarizeStage`, `SortStage` (`sort`/`order by`, `asc`/`desc`, `nulls first`/`last`), `TakeStage` (`take`/`limit`), `TopStage`, `CountStage` and `DistinctStage`. Columns are `NamedExpr` (`name = expr` or `expr`).
  - A leading `['dataset'] |` is accepted as the source, like `logs |`.
- **Compiler** (`logquery`). The files are `compile.go` (Query, Bounds, scope), `expr.go` (typed expressions), `stages.go` (stages, narrowing, result kind), `sql.go` (conditions and values) and `table.go` (the tail as SQL, and result rows).
  - `Compile(source, Bounds{From, To, Now})`. The signature changed: `Now` feeds `ago()`/`now()`, and the range feeds `bin_auto` and narrowing. Validate passes only `Now`. The handler takes a `clock.Clock`, so `NewLogsHandler(store, clock)`.
  - Expressions are typed: string, number, bool, datetime, timespan, and **dynamic**. A dynamic value is an Attribute read with no type from context. In SQL it is a `STRUCT(s, n, b, j)` with one part set, or NULL when the Log lacks the Attribute. A comparison with a value reads the part of that value's type, so `==` never coerces.
  - **Head and tail.** The leading `where`/`search` stages are the head. `(*Query).Where(LogColumns)` writes only the head, pushed into every source as before. Every later stage is the tail.
- **Result kind.** `(*Query).Table()`. A tail of only `take` stages and `sort by time desc` keeps Log rows (`Take() (n, ok)`). Any other stage gives a table.
- **Table execution:**
  - `(*Query).Select(LogColumns)` is the select list each source is read with: `id`, the built-ins, and each Attribute the tail reads as `"@<type>:<key>"`.
  - `(*Query).TableSQL(logs, limit)` writes the tail as one CTE per stage over the Logs that pass the head. Sorted relations carry a `"#order"` row number, because SQL keeps no order between CTEs.
  - `Columns()` and `Row(raw)` turn DuckDB values into JSON. Times become RFC 3339 UTC, and a dynamic value becomes the value it holds.
- **`LogStore.ScanLogTable(ctx, LogTableScan{ProjectID, From, To, Where, Select, Query})`** (`storage/logs_query.go`) unions `logs_late`, then for each day the hot table and that day's hour files with that day's `LogColumns`, filters each by the head, and runs `Query(logs)` once. It holds `publishMu`'s read lock for the whole query, because an aggregate must see each Log exactly once. An export therefore waits for a running table query.
- **Run endpoint:**
  - A table answers `{"table": {"columns": [{"name", "type"}], "rows": [[…]], "truncated"}}`, at most 10,000 rows. `type` is `string`, `number`, `bool`, `datetime`, `timespan` or `dynamic`. Log rows answer as before.
  - With `take`, the cursor also carries how many Logs are left (`time|id|left`), so paging stops at take's number. Old cursors still decode.
- **Docs:** `docs/logs/query-language.md` is the user-facing language reference. It documents `dcount` (HyperLogLog, `approx_count_distinct`) and `percentile` (T-Digest, `approx_quantile`) as estimates. M3-03's `logs` view doc can sit beside it in `docs/logs/`.
- **Tests:** `logs_language_test.go`, through the query app as before. It covers each operator before and after the export and with a late Log, narrowing but not widening, each stage's table across hot, files and late Logs, take paging, scalar functions and sorts of mixed types, pasted Axiom-style queries, and validate errors with positions. One M2-01 validate case that used `summarize` as an unknown stage now uses `mv-expand`.

Choices the ticket left open:
- **Which Queries return Log rows.** A tail of only `take` and `sort by time desc` keeps Log rows. The ticket's five stages always give a table. So do `extend` and a sort by anything else, because Log rows are newest first with a cursor. A table whose rows are still Logs (after `top`, `extend` or another sort) has the built-in fields, then the Attributes the Query names, then the added columns. Without a sort, it is newest first.
- **Unsorted tables** come back `ORDER BY ALL`, so results are deterministic. KQL leaves them unordered.
- **Narrowing.** Top-level `and`ed comparisons of `time` with a constant (`datetime`, `ago`, `now() ± span`) in the head narrow the scanned range. Every time condition also stays in the SQL, so the range can never widen.
- **KQL vs APL:** `startswith` ignores case, regexes are case-sensitive RE2, `sort` defaults to descending with nulls last (first for `asc`), and bin times are aligned to the epoch.
- **For pasted queries**, beyond the ticket: `=~`/`!~`, the `!` forms of the word operators, `countif`, `isnull`/`isnotnull` (same as `isempty`/`isnotempty`), `bin_auto` (at most about 100 bins over the request range), `_time` as an alias of `time`, `['dataset']` as the source, and `tostring`, `toint`/`tolong`, `todouble`/`toreal`, `tolower`, `toupper`, `strlen` and `floor`. Any other function is rejected at its span.
- **Typing rules:**
  - A bare Attribute in `where` isn't a condition, so write `== true`.
  - `sum`, `avg`, `percentile`, `min` and `max` read an Attribute as a number. `dcount`, `by`, `project` and `distinct` read it as dynamic.
  - Comparing two Attributes with no type from context is an error that suggests `tostring`.
  - Order comparisons of mismatched types are errors. `==` of mismatched types is constant false, as in M2-01.
- **`search`** is only allowed among the leading filters. After other stages it is an error that suggests `where … contains`.
- **Column names** follow KQL (`count_`, `avg_duration`, `percentile_duration_95`, `Count`, and `time` for `bin(time, …)`). Duplicate names are an error at the second column. `project _time` names its column `time`.

Deviations: none from the spec. The Log-rows rule above is the reading of "Log rows with a cursor otherwise" that keeps the cursor meaningful.

Follow-ups:
- **M2-02:** "the Logs that pass the filters before a tabular stage" is `(*Query).Where(cols)`, which writes the head only. `ScanLogTable` is a ready runner for whole-range aggregates. Give it a `Select` of what the histogram needs and a `Query` that groups by `time_bucket`. Note that it holds the publish read lock for the query's duration.
- **M3-03:** emission is separate from parsing:
  - The head is `Where(LogColumns)` and the tail is `TableSQL(logs, limit)` over a relation with `Select(LogColumns)`'s columns.
  - For readable SQL against the `logs` view, feed `Where`/`Select` a `LogColumns` that reads the maps only (today's `LogColumns{late: true}`; add an exported constructor). Wrap the select as the first CTE.
  - A Log-rows Query has no tail SQL yet. It needs `ORDER BY time DESC, id DESC LIMIT n`.
  - `ago()` and `now()` are folded to literal timestamps at compile time.
- **M3-02:** a table result's `type: "dynamic"` columns hold mixed JSON values (number, string, bool, or raw JSON for arrays and objects). Pivots should add their `where` before the first tail stage, which is the end of the head.
- **M2-03/M3-02:** a table run ignores `limit` and `cursor`. `truncated` says rows were cut at 10,000.
- **M1-10:** measure a day-long `summarize` through `ScanLogTable`. It holds the publish read lock throughout, and dynamic reads add four columns per Attribute.
