# 05: KQL-Lite v1 language, validate endpoint and editor

**What to build:** The full v1 KQL-Lite surface documented in `architecture.md` (where, extend, project family, summarize with bin/bin_auto, count, take, sort, top, distinct, search, parse, let, render hint, the scalar and aggregate function set) compiles to DuckDB SQL and runs against a Project's Logs. The editor highlights syntax, completes field names from the catalog and shows positioned errors from EventStore's validate endpoint.

**Blocked by:** 04

**Status:** wontfix

- [ ] Lexer and Pratt parser produce an AST; the resolver maps canonical names and catalogued Fields to columns or typed map lookups and reports unknown fields with suggestions
- [ ] SQL generation compiles pipes to nested subqueries with schema tracking, binds all literals, injects Project and time predicates, adds a default limit, evaluates `now()` once, and honours the documented Kusto semantics (case sensitivity, term-based `has`, default result names, sort defaults)
- [ ] Unsupported constructs are rejected with a positioned diagnostic; regexes are validated at compile time
- [ ] `POST /api/:pid/logs/validate` returns diagnostics and result columns; the Console proxies it
- [ ] CodeMirror 6 editor with a KQL tokenizer, completion from `/logs/fields` and lint from validate
- [ ] Golden tests (KQL to SQL) and execution tests over fixture rows in an in-memory DuckDB cover every operator and function in the v1 list

## Comments

2026-09-27: Closed as wontfix. This plan was superseded by the [logs map](../../logs/map.md). The file stays here as reference only.
