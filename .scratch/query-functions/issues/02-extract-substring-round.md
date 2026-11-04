# 02: extract, substring and round in Queries

**What to build:** agents use `run_sql` to take a value out of a message, cut it short or round a number, because the Query language can't do any of the three. Add KQL's `extract`, `substring` and `round`, with KQL's arguments and meaning. They then work wherever a Query runs: the Logs explorer, Charts, Monitors and `query_logs`. A Query that uses them on `message` over too wide a range is refused (01). See the [spec](../spec.md).

**Blocked by:** 01 (A function of message counts as matching text)

**Status:** resolved

- [x] **`extract(regex, captureGroup, source)`** gives the text that the capture group matched, as a string. Group 0 is the whole match.
  - The regex and the group must be written in the Query: a string, and a whole number of 0 or more.
  - The regex is checked as `matches regex` checks its pattern (RE2 in both), so a bad regex fails before the Query runs.
  - No match, or a group the regex doesn't have, gives no value, so `isempty()` is true. Read KQL's docs for whether that is an empty string or null, and follow them.
  - KQL's optional fourth argument (`typeof(...)`) isn't taken: `toint(extract(...))` does the same.
- [x] **`substring(source, startingIndex [, length])`** counts characters, not bytes, from a zero-based index. Without a length, it runs to the end. Follow KQL's docs for a negative index and a range past the end.
- [x] **`round(number [, precision])`** rounds to a whole number when the precision is left out. Ties go as KQL's docs say. ClickHouse rounds ties in a `Float64` to even, so check that before mapping straight to its `round`.
- [x] **Optional last argument:** `substring` and `round` take one fewer argument than their full form. Functions take a fixed number of arguments today, so this has to change. A wrong count gives an error that says what each function takes.
- [x] **Mistakes are placed:** a regex or group not written in the Query, a bad regex and a wrong count are errors with a line and column, which the Query box marks.
- [x] **A text match:** `summarize count() by extract("…", 1, message)` over a busy week is refused with a narrower Query, and runs on one file that fits.
- [x] **The Query box** offers the three as you type, with their arguments and one line each.
- [x] **Docs:**
  - the language reference's scalar functions, in both copies, with an example each;
  - `query_logs`' description names the three in a few words;
  - `run_sql`'s description says that `query_logs` takes values out of messages, naming `extract`. Add the sentence if 03 hasn't, or add the name to it.
- [x] **Tests** run Queries through EventStore's Query API, run and validate, against ClickHouse:
  - `extract` with group 0, group 1 and no match, and `toint(extract(...))` summed;
  - `substring` with and without a length, on text that isn't ASCII;
  - `round` with and without a precision, ties included, in a summarize;
  - the errors above, with their places;
  - the refusal over a busy week.

## Comments

**2026-11-04, built on `query-functions/02-extract-substring-round`, from `feature/query-functions` at `32548021`.**
- **EventStore** (`logquery/expr.go`, `logquery/sql.go`):
  - The function table's entries gain `optional` (how many last arguments a call may leave out) and `usage` (what the function takes). A wrong count says `<name> takes <usage>` where a function has one, and `takes N arguments` as before where it hasn't, so the existing functions' errors are unchanged. The three new entries sit after a blank line in `scalarFunctions`, so the existing lines keep their alignment.
  - `extract` maps to ClickHouse's `regexpExtract` with the group as its index. The regex goes through the same `checkRegex` as `matches regex` (moved out of `binary` into a helper, same message). A group past the regex's groups is worked out when the SQL is written and gives no value, since `regexpExtract` throws on it.
  - `substring` maps to `substringUTF8`, 0-based to 1-based. A start written in the Query, 0 or more, is written as a number; any other start goes through the end-relative form.
  - `round` doesn't use ClickHouse's `round` (halves to even, `round(2.5)` is 2 there, checked on the dev server). It scales by an exact power of ten, takes the whole part plus one away from zero when what's left is 0.5 or more, and scales back. A value scaled to 2^52 or more is returned as it is. The precision may be any number, not only one written in the Query, as in KQL.
- **Decisions the ticket left open:**
  - **extract with no match:** KQL's docs say `null`. Here it's NULL, so no-match Logs are one group in `summarize ... by` and `isempty()`/`isnull()` are true. An empty match is NULL too, since a KQL string's null is its empty string.
  - **substring:** KQL's docs give a negative start ("retrieved from the end") and their four examples, which all hold, but don't say what happens past either end. Chosen: a negative start past the beginning starts at the first character (`substring("123456", -20, 3)` is `123`); a range past the end stops there; a start past the end, a length of 0 and a negative length give an empty string, not no value.
  - **round's ties:** KQL's docs don't state a rule. None of their examples is an exact half in binary (2.15 is just under one). All five of them hold here. Halves go away from zero (`round(2.5)` 3, `round(-2.5)` -3, `round(0.125, 2)` 0.13, `round(25, -1)` 30), as SQL Server's `ROUND` does. This couldn't be checked against a real Kusto cluster from here. If Kusto turns out to round halves to even, change one line in `writeRound` and the tie rows in the test.
  - **Type errors** aren't in the ticket but come with the table: `extract`'s source, `substring`'s source, start and length, and `round`'s number and precision each say what they need at their argument, such as "round takes a number, not a string".
- **Console:** the Query box's `SCALAR_FUNCTIONS` lists `extract` (`regex, group, s`, picking it writes `extract("", 1, message)` with the cursor in the regex), `substring` (`s, start [, length]`) and `round` (`x [, precision]`), after `strlen`.
- **Docs:** the three rows sit after `tolower` … in the reference's Scalar functions, in both copies (still identical). The extract row notes that `\d` is written `\\d` in a Query's string, since the lexer refuses unknown escapes. `query_logs`' description names the three with an example each after the aggregates. `run_sql`'s ends "It takes values out of messages too, with extract.", as its own sentence so 03 can add `parse_json` to it.
- **Tests**, all in a new `event_store/logs_extract_substring_round_test.go` so they don't collide with 03's:
  - `TestExtractTakesWhatARegexsGroupMatched`: group 1, group 0, no match, a group the regex lacks, a missing Attribute, `isempty`, `summarize ... by` with a no-value group, `sum(toint(extract(...)))` with a `\\d` regex, and `toint(extract(...)) > 5` in a where.
  - `TestSubstringCutsCharactersFromAZeroBasedIndex`: KQL's four examples on digits and on `héllo wörld ✓`, past the end, before the start, a length of 0 and below 0, a start and length worked out per Log, and in a `summarize ... by`.
  - `TestRoundTakesHalvesAwayFromZero`: KQL's five examples with a precision read from each Log, every Log with no precision, 2 and -1, ties positive and negative, and `round` of a value and of aggregates (`round(sum(x))`, `round(avg(x), 2)`) in a summarize.
  - `TestExtractOfMessageOverABusyWeekIsRefusedWithANarrowerQueryThatFits`: refused with a search's estimate and largest range; its narrower Query and the same Query on `cron.log` run and count the store. `estimate` gives a text match over the budget for `extract` in a where, `substring(message, …)` in a project and `round(strlen(message), -2)` in an aggregate.
  - `TestValidatePlacesMistakesInExtractSubstringAndRound`: 14 mistakes with their line and column: a regex or group not written in the Query, a negative and a fractional group, a bad regex, wrong types, and wrong counts, one spanning two lines.
- **Run:**
  - Go: `./logquery/...` and `./handler/` passed. The root package with `-run 'Budget|Refused|Heavy|Estimate|Histogram|Force|FunctionOfMessage|Operator|Validate|Search|Queries|Scalar|Tabular|Extract|Substring|Round|Where|Contains|Pasted|Take|Language|Chart'` passed (173 s). `go vet` is clean.
  - Console: `bun run typecheck` passed. `completions.test.ts` passed (23). `test/mcp/logs_tools_test.rb` passed (27, the reference copies matching). Rubocop is clean on `query_logs.rb` and `run_sql.rb`.
