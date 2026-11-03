# 01: A function of message counts as matching text

**What to build:** a Query that applies a function to `message` reads the text of every Log it scans, just as `message contains` does. Today only `search`, and `contains`, `startswith`, `=~` and `matches regex` on `message`, count as text matches. So `where strlen(message) > 1000` or `summarize count() by tolower(message)` over a week runs without being sized. Make any scalar function that takes `message` as an argument, anywhere in the Query, count as a text match. Then the Logs explorer and the MCP server size it and refuse it over the text-match budget, and over a quarter of the budget it's a heavy read. Every function that 02 and 03 add is then covered as soon as it lands. See the [spec](../spec.md)'s Implementation Decisions.

**Blocked by:** None (can start immediately)

**Status:** ready-for-agent

- [ ] A Query counts as matching text when a scalar function has `message` among its arguments: in any stage (`where`, `extend`, `project`, `summarize`'s by columns or inside an aggregate, `sort`, `top`, `distinct`) and at any depth, such as `toint(strlen(message))`.
- [ ] Over a busy week, `where strlen(message) > 1000` and `summarize count() by tolower(message)` are refused from the Logs explorer and from `query_logs`. The refusal is the same as a search's: the estimate, the budget, the largest range that fits, the busiest files and a narrower Query. The narrower Query runs.
- [ ] The same Queries on one file that fits the budget run.
- [ ] `estimate` sizes them as text matches, and over a quarter of the budget they're heavy reads.
- [ ] These don't change:
  - `project message` and plain Log lists;
  - Queries with no function of `message`;
  - functions of other Fields, such as `tolower(service)`;
  - Charts and Monitors, which log their estimate and are never refused.
- [ ] "Reading less" in both copies of the language reference says that a function of `message` counts as matching text.
- [ ] Tests run the Queries through EventStore's Query API against ClickHouse, beside the existing budget tests, over a busy week seeded through the Source counts:
  - refused over the week, with a narrower Query that runs;
  - run on one file;
  - a function of another Field over the week runs;
  - a `project message` Log list over the week runs.
