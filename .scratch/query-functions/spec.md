# Query functions for what agents use SQL for

Asked for by the user on 2026-11-03, after a check of production's MCP calls. Agents reach for `run_sql` mostly to do what the Query language can't: take a value out of a message. `run_sql` skips the text-match budget and the heavy-read slots ([query timeouts](../query-timeouts/spec.md) 02 and 04), so each of those calls is a read that nothing sizes. The decision: keep both languages ([logs 11](../logs/issues/11-query-language.md), [ADR 0003](../../docs/adr/0003-unrestricted-sql-queries-behind-an-instance-setting.md)), and add the missing functions to the Query language, so that these reads go through `query_logs` and its budget.

## Findings (2026-10-27 to 11-03, production's call timings)

- **105 `run_sql` calls.** One call can count in several of these rows:
  - 40 take values out of `message`: `JSONExtractString(message, 'resource_type')`, `JSONExtractInt(message, 'result', 'cache_keys_expired')`, or `extract(message, '"store_id":([0-9]+)')`;
  - 39 match text with `LIKE` or `ILIKE`;
  - 22 cut a message short with `substring(message, 1, 160)`;
  - 11 use arrays, such as `JSONLength(message, 'ids')`;
  - none joins tables or reads `events` or `spans`.
- **`query_logs` failed on the same gaps:** `unknown function "substring"`, `"extract"` and `"round"`.
- **The language's scalar functions** are only `tostring`, `toint`, `tolong`, `todouble`, `toreal`, `tolower`, `toupper`, `strlen`, `bin`, `floor`, `bin_auto`, `ago` and `now` (`event_store/logquery/expr.go`, `scalarFunctions`).
- **The query is in Organization `solidtrace`, Project `solidtrace`:** `where kind == "mcp_tool" and tool == "run_sql"`, with the SQL in `arguments.sql`.

## What we're building

1. **`extract`, `substring` and `round`** ([01](issues/01-extract-substring-round.md)), and a function of `message` counts as matching text, so the budget sizes it.
2. **`parse_json(message)`** ([02](issues/02-parse-json.md)), with `.name`, `["name"]` and `[0]` to reach into it, and `array_length`.

## Decisions

1. **KQL's names, arguments and semantics**, as the language follows KQL where KQL and APL differ ([logs 11](../logs/issues/11-query-language.md)). Agents write KQL first.
2. **`parse_json` with member access, not `extract_json`.** `parse_json(message).result.cache_keys_expired` is what KQL users write. Its value is dynamic, as an Attribute read without a type already is, so `== 494`, `toint()`, `tostring()` and `summarize ... by` work on it as they do on Attributes. `extract_json` would need `typeof()`, which is new syntax too, and agents write it less.
3. **A function of `message` is a text match.** `summarize count() by extract("…", 1, message)` reads the text of every Log in the range, as `message contains` does. So it's sized and refused the same way, from the explorer and MCP. This covers the functions that already exist too (`strlen(message)`, `tolower(message)`).
4. **`run_sql` stays as it is.** Bringing SQL under the budget would need EventStore to size SQL it can't parse. It still stops at 10 s.

## Out of scope

- `extract_all`, the `parse` stage, `has`, `mv-expand` and `join`.
- `parse_json` of an Attribute: Attributes that hold JSON are already read as dynamic values.
- A budget or heavy-read slot for `run_sql`.

## To check after release

The same call-timings Query over the week after the release: the share of `run_sql` calls that use `JSONExtract`, `extract` or `substring` should fall, and `query_logs` should show no `unknown function` errors for these names.
