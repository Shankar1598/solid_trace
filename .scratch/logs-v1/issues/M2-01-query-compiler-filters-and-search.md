# M2-01: Query compiler with filters and search

**What to build:** the first slice of the Query language end to end. EventStore compiles a Query of `where` equality filters and `search` to DuckDB SQL and runs it for a Project and time range, newest first with paging; validates a Query; and fetches one Log. This replaces M1-01's latest-N operation as the read path. Spec: [Query compiler and query endpoints](../spec.md#query-compiler-and-query-endpoints-eventstore); [ADR 0002](../../../docs/adr/0002-logs-scoped-by-project-flat-attributes.md); user stories 40, 46, 47, 54, 77, 78.

**Blocked by:** M1-08.

**Status:** ready-for-agent

- [ ] A hand-written Go compiler in EventStore, borrowing `runreveal/pql`'s structure, is the single path from a Query to SQL. This ticket's surface: an optional leading `logs |`; `where` with `==`, `!=`, `and`, `or`, `not`; `search`. Unknown stages and functions are rejected with a position.
- [ ] Semantics: `==` never coerces (`status == 200` matches numbers, `status == "200"` strings); `!=` includes Logs missing the Field; `search` is a case-insensitive `contains` over `message` and every string Attribute. The Project and time range come from the request.
- [ ] Columns come only from the cached promoted-set table, one branch per day: the column for promoted keys, `coalesce(column, map[key])` for keys added during that day (that day only), maps only for `logs_late`. No Parquet footer reads.
- [ ] "Latest N" scans newest first, hour by hour, stopping once it has N rows; older pages continue from a (time, id) cursor.
- [ ] Query-app operations: **run a Query** (Log rows plus a next-page cursor); **validate a Query** (errors with line and column; and when the Query is only `where field ==|!= literal` stages and one `search`, its chips); **fetch one Log** by id. Routing, cancel and result caps are the implementer's call ([ticket 15](../../logs/issues/15-how-log-queries-run.md)); no per-query timeouts.
- [ ] Tests act as a user: rows returned for each operator across hot table, Parquet and `logs_late`; same rows before and after an export and a promotion; paging with the cursor; a rare-key search returns matches newest first; validate positions and chips (and no chips for a non-Builder Query); fetch by id.
