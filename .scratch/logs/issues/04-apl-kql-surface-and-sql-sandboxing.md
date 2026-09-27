# 04: What APL/KQL surface is worth supporting, and can DuckDB SQL mode be sandboxed?

Type: research
Status: resolved
Blocked by: none
Map: [SolidTrace Logs](../map.md)

## Question

Which APL/KQL operators and functions cover most real log queries, and which are expensive or impossible to map onto DuckDB SQL? What existing KQL/APL parsers or KQL-to-SQL transpilers exist (any language, with licences), and what are the trade-offs of parsing in Go, Ruby or the browser? How do query builders (Axiom, Grafana, Better Stack) map to a text language? Finally, what can DuckDB do to sandbox a user-written SQL query scoped to one Project (statement validation, `json_serialize_sql`, per-connection vs instance-wide settings, resource limits, timeouts)?

Starting points: `.scratch/logging-old/research/research-kql.md`, `research-duckdb.md` (unverified).

## Answer

Full findings: `.scratch/logs/research/04-apl-kql-and-sql-sandbox.md` on branch `research/apl-kql-and-sql-sandbox` (commit `539812e`); read with `git show research/apl-kql-and-sql-sandbox:.scratch/logs/research/04-apl-kql-and-sql-sandbox.md`. Findings on the sandbox were tested against the pinned DuckDB 1.4.3 (duckdb-go v2.5.4).

- **Core surface:** no usage statistics exist. Microsoft's, Axiom's, Log Analytics' and Grafana's builders and tutorials all converge on the same core: where with and/or and string operators, project/extend, summarize (count, dcount, sum/avg/min/max, percentiles) by fields plus a time bin, sort, take/top, count, distinct and search. All of it maps to DuckDB 1.4.3.
- **Hard or lossy mappings:**
  - `has` becomes a full regex scan (no term index), and RE2's `\b` treats `_` as a word character, giving wrong answers.
  - `dcount` and `percentile` are estimates in both engines, so numbers won't match.
  - Semantics differ for `/`, `%`, 0- vs 1-based `substring`, and `mv-expand` on nulls.
  - RE2 has no named groups or lookaround.
  - No SQL equivalent for the `evaluate` plugins, `scan`, `facet`, or APL's `spotlight`, `series_*`, `genai_*` and geo-IP functions.
- **APL ≠ KQL:** its `join` defaults to inner and is a capped preview; `toint` is 64-bit; `bin_auto` is APL-only.
- **Parsers:**
  - Go has no mature KQL parser; `runreveal/pql` (Apache-2.0) is a subset, stale since 2025-01, and passes unknown functions through to SQL.
  - The best KQL→DuckDB prior art is C#: `saoc90/kql-to-sql` (MIT), on Microsoft's Apache-2.0 parser, which also ships an ANTLR grammar.
  - The browser option is the 32 MB `@kusto/language-service-next`, which accepts full KQL.
  - There is no Ruby parser.
- **Builder vs text:**
  - Axiom and Grafana Loki go both ways over a subset and warn when a query can't be shown in the builder.
  - Grafana SQL is one-way.
  - Log Analytics wraps complex KQL as an opaque block.
  - Grafana ADX and Better Stack compile builder queries to text only.
- **SQL sandboxing:**
  - DuckDB's security policy calls untrusted SQL "unsafe by design" and recommends OS-level sandboxing.
  - All security and resource settings apply to the whole instance. There is no per-query memory, thread or timeout setting, and `allowed_configs` only arrives in 1.5.
- **Escape routes observed on 1.4.3:**
  - duckdb-go runs every statement before the last, even when only preparing.
  - `main.<table>` bypasses a Project-scoping CTE.
  - `query()`/`query_table()` hide SQL in strings; `FROM '/path'` parses as a table name; `PRAGMA` reports its type as SELECT.
  - A SELECT calling `enable_logging()` gets past `lock_configuration`; not reported upstream.
  - Result sets are materialised outside `memory_limit` (963 MiB against a 200 MB limit).
- **What works:**
  - `json_serialize_sql` rejects anything that isn't a SELECT and exposes a full AST.
  - Cancelling the Go context interrupts execution; v2.5.5 extends this to preparation.
  - Separate in-memory instances in one process keep independent settings.
  - `allowed_directories` blocks `..`, but would expose every Project's archive to user SQL.
- **Version date conflict:** this research says community support for DuckDB 1.4 LTS ended on 2026-09-16; the storage research reads the current release calendar as 2026-11-17. The storage decision should re-check it.

Open questions it hands to decisions: baseline dialect (KQL vs APL vs a PQL-sized subset); `has` semantics without a term index; parser language (Go from scratch, port or grammar, or the C#/TS prior art); builder↔text direction; whether SQL mode can be safe enough in v1 (separate process or instance, AST allow-list, result caps) or should be deferred.
