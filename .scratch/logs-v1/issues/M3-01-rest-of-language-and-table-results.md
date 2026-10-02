# M3-01: The rest of the Query language and table results

**What to build:** the compiler covers the whole v1 language, and Queries that end in a tabular stage return a table instead of Log rows. Spec: [Query compiler and query endpoints](../spec.md#query-compiler-and-query-endpoints-eventstore) (The language, Run a Query); user stories 53, 58–60, 63, 66.

**Blocked by:** M2-01.

**Status:** ready-for-agent

- [ ] `where` gains comparisons (`<`, `<=`, `>`, `>=`), `contains` (case-insensitive), `contains_cs`, `startswith`, `matches regex`, `in`/`!in` (`!in` includes Logs missing the Field), `isempty`/`isnotempty`.
- [ ] Stages: `project`, `extend`, `summarize` with `count`, `dcount`, `sum`, `avg`, `min`, `max`, `percentile`, by Fields and `bin(time, …)`; `sort`, `take`, `top`, `count`, `distinct`, `search`. Unknown functions rejected with a position. KQL's semantics win where KQL and APL differ; common pasted Axiom queries work.
- [ ] A `where` on `time` may narrow the request's range, never widen it; nothing leaves the Project.
- [ ] Run a Query returns a table (columns and rows) for a Query ending in `summarize`, `project`, `distinct`, `count` or `top`; Log rows with a cursor otherwise.
- [ ] `dcount` and `percentile` are documented as estimates where the language is documented for users.
- [ ] Tests: each operator and function by the rows or table it returns; narrowing but not widening the range; a few pasted Axiom-style queries.
