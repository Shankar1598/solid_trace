# 01: extract, substring and round in Queries

**What to build:** agents use `run_sql` to take a value out of a message or cut it short, because the Query language has no function for either. Add KQL's `extract`, `substring` and `round` to the language. A Query that applies a function to `message` reads the text of every Log in its range, so it counts as a text match and the budget sizes it. See the [spec](../spec.md).

**Blocked by:** None (can start immediately)

**Status:** ready-for-agent

- [ ] **`extract(regex, captureGroup, source)`**, as KQL has it: the text that the capture group matched, as a string. Group 0 is the whole match.
  - The regex and the group must be written in the Query: a string and a whole number of 0 or more. The regex is checked as `matches regex` checks its pattern (RE2 in both).
  - No match, or a group the regex doesn't have, gives no value, so `isempty()` is true. Check KQL's docs for whether that is an empty string or null, and follow them.
  - KQL's optional fourth argument (`typeof(...)`) isn't supported: `toint(extract(...))` does the same.
  - Example: `summarize count() by store = extract("\"store_id\":([0-9]+)", 1, message)`.
- [ ] **`substring(source, startingIndex [, length])`**, as KQL has it: characters, not bytes, from a zero-based index. Without a length, to the end. Follow KQL's docs for a negative index and a range past the end. ClickHouse's `substringUTF8` counts from 1.
- [ ] **`round(number [, precision])`**, as KQL has it: precision 0 when left out. Ties go as KQL's docs say. ClickHouse's `round` on a `Float64` rounds ties to even, so check before mapping it straight.
- [ ] **Optional arguments.** `scalarFunctions` takes a fixed number of arguments. Let `substring` and `round` take one fewer, with an error that says what they take, as the others do.
- [ ] **A function of `message` is a text match.**
  - `Query.MatchesText` (`event_store/logquery/reads.go`) is true for a Query that calls any scalar function on `message`, anywhere in the Query. That includes the existing ones, such as `strlen(message)`.
  - So the explorer and MCP size and refuse it as they do `message contains`, and over a quarter of the budget it's a heavy read.
  - `message` on its own in `project`, and plain Log lists, aren't text matches.
- [ ] **The editor** lists the three in `SCALAR_FUNCTIONS` (`console/app/frontend/lib/logs/language.ts`), with their arguments and one line each.
- [ ] **Docs:**
  - "Scalar functions" in `docs/logs/query-language.md` and its copy `console/app/mcp/docs/query-language.md`, which a test keeps identical;
  - "Reading less" says that a function of `message` counts as matching text;
  - `query_logs`' description gains a short mention of `extract`, `substring` and `round`.
- [ ] **Tests:**
  - root tests against ClickHouse (`event_store/logs_query_test.go`):
    - `extract` with group 0, group 1 and no match;
    - `substring` with and without a length, on text that isn't ASCII;
    - `round` with and without a precision, ties included;
  - the errors for a regex or group not written in the Query, and for the wrong number of arguments;
  - `event_store/logs_read_budget_test.go`: a `summarize ... by extract(..., message)` over a range past the budget is refused, the same Query on one file that fits runs, and a `project message` Log list over the same range isn't refused.

## Comments
