# 01: A function of message counts as matching text

**What to build:** a Query that applies a function to `message` reads the text of every Log it scans, just as `message contains` does. Today only `search`, and `contains`, `startswith`, `=~` and `matches regex` on `message`, count as text matches. So `where strlen(message) > 1000` or `summarize count() by tolower(message)` over a week runs without being sized. Make any scalar function that takes `message` as an argument, anywhere in the Query, count as a text match. Then the Logs explorer and the MCP server size it and refuse it over the text-match budget, and over a quarter of the budget it's a heavy read. Every function that 02 and 03 add is then covered as soon as it lands. See the [spec](../spec.md)'s Implementation Decisions.

**Blocked by:** None (can start immediately)

**Status:** resolved

- [x] A Query counts as matching text when a scalar function has `message` among its arguments: in any stage (`where`, `extend`, `project`, `summarize`'s by columns or inside an aggregate, `sort`, `top`, `distinct`) and at any depth, such as `toint(strlen(message))`.
- [x] Over a busy week, `where strlen(message) > 1000` and `summarize count() by tolower(message)` are refused from the Logs explorer and from `query_logs`. The refusal is the same as a search's: the estimate, the budget, the largest range that fits, the busiest files and a narrower Query. The narrower Query runs.
- [x] The same Queries on one file that fits the budget run.
- [x] `estimate` sizes them as text matches, and over a quarter of the budget they're heavy reads.
- [x] These don't change:
  - `project message` and plain Log lists;
  - Queries with no function of `message`;
  - functions of other Fields, such as `tolower(service)`;
  - Charts and Monitors, which log their estimate and are never refused.
- [x] "Reading less" in both copies of the language reference says that a function of `message` counts as matching text.
- [x] Tests run the Queries through EventStore's Query API against ClickHouse, beside the existing budget tests, over a busy week seeded through the Source counts:
  - refused over the week, with a narrower Query that runs;
  - run on one file;
  - a function of another Field over the week runs;
  - a `project message` Log list over the week runs.

## Comments

**2026-11-04, built on `query-functions/01-function-of-message-is-a-text-match`, from `feature/query-functions` at `5daaafa7`.**
- **EventStore:**
  - `MatchesText` now counts a `call` (a scalar function: the `scalarFunctions` table and `bin`) with an argument that reads `message`, beside a `textMatch` on it (`readsMessageText` in `logquery/reads.go`). `anyOf` already walks every stage's expressions, aggregates and nested calls, so any stage and depth is covered, and every function 02 and 03 add is too once it compiles to a `call`.
  - Not counted, as the ticket says only scalar functions: `message` alone (`project message`, `summarize ... by message`), the aggregates on it (`dcount(message)`), and `isempty(message)`, which isn't a `call`.
  - The refusal's message says "(search, contains, startswith or =~ on message, or a function of message)". Its shape and the rest of its text are unchanged.
- **Docs and descriptions:** "Reading less" in both copies of the reference names a function of `message`, with `where strlen(message) > 1000` and `summarize count() by tolower(message)`, and says `message` on its own, as in `project message`, doesn't match text. The same parenthetical changed in `query_logs`' description, `update_instance_settings`' argument and Settings → Instance's help, and in the copies of EventStore's refusal message that `refusal.test.ts`, `api.test.ts` and the `logs-refusal` flow use as fixtures.
- **Left open, not built:** a column that copies `message` and is then used by a function, such as `extend m = message | where strlen(m) > 1000`, isn't counted. `extend m = message | where m contains "x"` isn't counted today either, so 01 keeps that rule. Closing it means following columns through `extend`, `project` and `summarize`.
- **Tests** (`event_store/logs_read_budget_test.go`, over `tagalysSources`' busy week):
  - `TestAFunctionOfMessageOverMoreThanTheBudgetIsRefusedAsASearchIs`: `where strlen(message) > 1000` and `summarize count() by tolower(message)` are refused with the same estimate, largest range and busiest files as `search "timeout"`. Their narrower Query (on `cron.log`) runs and finds the Logs sent. On `cron.log` they run, with a search's estimate there, within the budget and heavy.
  - `TestAFunctionOfMessageMatchesTextInAnyStageAndOtherQueriesRunAsBefore`: `estimate` gives a text match over the budget for a function of `message` in `where` (nested and under `not`), `extend`, `project`, an aggregate, `countif`, `sort`, `top` and `distinct`. `project message`, `project time, service, message`, a plain Log list, `where level == "error"`, `tolower(service)` and `strlen(service)` aren't text matches and run over the week. An unchecked run (a Chart's) of `summarize count() by tolower(message)` runs and logs `text_match=true`, `over_budget=true`, `refused=false`.
- **Run:**
  - Go: `-run 'Budget|Refused|Heavy|Estimate|Histogram|Force|FunctionOfMessage|Operator|Validate|Search|Queries'` in the root package passed (108 s). `./logquery/...` and `./handler/` passed.
  - Console: `bun run typecheck` passed. `refusal.test.ts` and `api.test.ts` passed (9). `test/mcp/logs_tools_test.rb` and `test/mcp/instance_settings_tools_test.rb` passed (37), the reference copies matching. Rubocop is clean on the two tools. The `logs-refusal` flow isn't run here; only its fixture's text changed.
