# 01: Filters in the Query language

**Status:** claimed
**Spec:** [spec.md](../spec.md)

**What to build:** `$name` in a Query, filled in by EventStore from the request's `parameters` when it compiles, as the spec's "In a Query" says. Nothing in the Console UI changes yet, beyond the Query editor colouring `$name`.

## Where

- [ ] `event_store/logquery/parser`: `$` followed by name characters lexes as a parameter token. `$` stays allowed inside a name (`isIdentPart`) but no longer starts one, so a field named `$type` is written `['$type']`. A `Param` expression (name and span) parses wherever a primary expression does, and `search` takes a `Param` as well as a quoted string.
- [ ] `event_store/logquery`: `Compile` takes the values, as `map[string][]string`.
  - A comparison or text match with a `Param` on either side becomes, for one value, the comparison with that value as a literal. For several, a positive operator becomes an `or` of them and a negated one an `and`. For none, a true condition, written so that the head's SQL is the same as for the Query without it (`Where` leaves it out). `search $q` works the same way.
  - Types follow the spec: text against built-in fields, strings and text operators. Against an Attribute, `==` and `=~` become an `or` of the text and the number or bool the value reads as; `!=` and `!~` become an `and`. Against a number column, a number; a value that isn't one matches nothing.
  - A `Param` anywhere else is an error at its span, saying where filters go (`compiler.expr`'s default case, `in`, ordered comparisons, `matches regex`, `not(…)`).
  - `Query.Parameters()` gives the names used, in order of first use.
- [ ] `event_store/logquery/chips.go`: a where stage comparing a field with a `Param` is a chip whose value is `{"filter": "<name>"}`. `search $q` sets `search_filter` to `"q"` (with `search` null), so the Builder can show such Queries (04).
- [ ] `event_store/handler/logs.go`: `queryRequest` takes `parameters` and passes them to `compile`. More than 100 values for a name, or a value over 1,000 bytes, is a 400. Validate answers `parameters`, the names the Query uses. Histogram and readable SQL compile with none (All).
- [ ] Console: `EventStore.query_logs(…, parameters: nil)` sends them when present. `ChartRuns.run_chart` takes `parameters:` and passes them on (02 sends real values).
- [ ] Browser: `lib/logs/queryLexer.ts` gains a `parameter` token kind, coloured in the Query editor. A `parametersOf(text)` helper lists the names a Query uses, in order, each once. It is lexer-based, since the browser never parses Queries.
- [ ] Docs: a "Dashboard filters" section in `docs/logs/query-language.md` and its MCP copy, `console/app/mcp/docs/query-language.md`. ADR 0014 and GLOSSARY.md's **Dashboard filter** are written with the spec.

## Tests

- [ ] Go (`go test -p 1`, `./logquery/...` and the root package's logs tests by `-run`):
  - parse: `$name`, `['$type']`, and `search $q`;
  - one value; several with `==` (any) and `!=` (none); text operators;
  - All gives the same `Where` as the Query without the comparison;
  - an Attribute holding `500` in one Log and `"500"` in another matches both;
  - a built-in field compares as text;
  - a number column after `summarize`;
  - each error case;
  - `Parameters()`; chips with a filter value and `search_filter`;
  - a root-package run over real Logs with `parameters`, and a bin-cache run whose two values don't share bins.
- [ ] vitest for the lexer's `parameter` token and `parametersOf`.
- [ ] A Rails test that `query_logs` sends `parameters`.

## Comments
