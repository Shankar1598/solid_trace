# 11: What query language(s) do users write?

Type: grilling
Status: resolved
Blocked by: 04, 07
Map: [SolidTrace Logs](../map.md)

## Question

How much of APL/KQL does SolidTrace support and with what semantics, whether raw DuckDB SQL mode ships in v1 and under what safety rules, whether there is one query engine or two, and where parsing and translation happen?

Input (2026-10-02, from [What is a Log?](07-log-domain-model.md)): Fields are seven reserved built-ins plus Attributes with flat dotted names (`http.method`, no path syntax). An Attribute's values can have different types in different Logs, and a numeric comparison matches only numeric values; arrays are single values tested with "contains". `level` can be absent, so the language needs an "is empty" test.

## Answer

Resolved 2026-10-02 (grilling). Glossary: **Query** and **SQL query** added to `CONTEXT.md`. The unrestricted, opt-in SQL decision is [ADR 0003](../../../docs/adr/0003-unrestricted-sql-queries-behind-an-instance-setting.md).

**Two ways to query in v1**
- **The query language:** KQL-style and unbranded (never called APL or KQL). Where KQL and APL differ, KQL's published semantics win. Pasted Axiom queries often work, but that is not promised.
- **SQL queries:** raw DuckDB SQL run exactly as written, with no validation or sandbox. Off by default behind one instance-level setting; once on, every member of every organization can run them. The setting's description lists what that allows: reading every organization's Logs and Events and files on the server, writing files, dropping tables (including the Event index), and starving ingest.
- **What SQL sees:** a stable, documented `logs` view (`project_id`, the seven built-in fields, the Attributes) regardless of storage layout. Raw tables stay reachable but undocumented. The explorer's Project selector does not apply; users filter `project_id` themselves.

**Language surface (v1)**
- `where` with `and`/`or`/`not`, comparisons, `contains` (case-insensitive), `contains_cs`, `startswith`, `matches regex`, `in`/`!in`, `isempty`/`isnotempty`.
- `project`, `extend`; `summarize` with `count`, `dcount`, `sum`, `avg`, `min`, `max`, `percentile`, by fields and `bin(time, …)`; `sort`, `take`, `top`, `count`, `distinct`, `search`.
- `dcount` and `percentile` are documented as estimates.
- Not in v1: `has`, `join`, `mv-expand`, `parse`, `evaluate`, series and geo functions.

**Semantics**
- The explorer bar sets the Project and time range. Query text starts with operators; a leading `logs |` is optional. A Query can narrow the time range but never widen it or leave the Project.
- The search box is a case-insensitive `contains` over `message` and every string Attribute value. Whether that is fast enough is for the benchmark.
- `!=` and `!in` include Logs where the Field is missing, so "Hide this" never hides unrelated Logs. `==` never coerces types (`200` does not match `"200"`). A filter built from a tag uses that value's own type.

**Engine**
- One query path: filter chips, search, histogram, per-level counts and field top-values all become query text, compiled once into DuckDB SQL. The hidden query text is always the real query.
- A hand-written Go parser and compiler in EventStore, borrowing `runreveal/pql`'s structure, rejecting unknown functions. The browser only highlights, and gets errors from a validate endpoint.
