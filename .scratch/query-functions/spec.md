# Query functions for what agents use SQL for

Status: ready-for-agent

Asked for by the user on 2026-11-03, after a check of production's MCP calls. Tickets: [01](issues/01-function-of-message-is-a-text-match.md) (a function of `message` counts as matching text), then, side by side, [02](issues/02-extract-substring-round.md) (`extract`, `substring`, `round`) and [03](issues/03-parse-json.md) (`parse_json` with `.name`, `["name"]` and `[0]`, and `array_length`).

## Problem Statement

People and agents who read Logs in SolidTrace often need a value that sits inside a Log's message: a store id, a count in a JSON message, the first part of a long line. The Query language can't take anything out of a message. It has casts, case changes, string length and bins, and nothing more. So an agent working over the MCP server turns to `run_sql` and writes ClickHouse SQL by hand (`JSONExtractString`, `extract`, `substring`). In the Logs explorer, a person must leave the Query for SQL mode, and lose the Builder, the histogram and the narrower Query that a refusal offers.

That has a cost beyond convenience. A SQL query isn't sized before it runs. The text-match budget and the heavy-read slots, built to stop the Log reads that hit the 30-second timeout, only apply to Queries. On a small VM with two CPUs, a week-long SQL scan of message text runs until its 10-second limit and takes CPU from ingest and from other members' Queries. Nothing offers the agent a narrower read.

## Solution

The Query language gains the functions that agents use SQL for, with KQL's names, arguments and meaning, since agents and people who know KQL write those first:

- `extract(regex, captureGroup, source)` takes the text a regex's capture group matched;
- `substring(source, startingIndex [, length])` cuts a string;
- `round(number [, precision])` rounds a number;
- `parse_json(message)` reads a JSON message as a value, and `.name`, `["name"]` and `[0]` reach into it;
- `array_length(x)` gives an array's length.

A Query that applies any function to `message` counts as matching text, as `message contains` does. So the Logs explorer and the MCP server size it and refuse it over the text-match budget, with the largest range that fits, the busiest files and a narrower Query. Over a quarter of the budget, it's a heavy read. Both languages stay: `run_sql` is still there for what only SQL can do.

## User Stories

1. As an agent on the MCP server, I want to take a store id out of a message with `extract`, so that I can count Logs by store without writing SQL.
2. As an agent, I want `extract` with group 0 to give the whole match, so that I can pull out a token without wrapping the regex in a group.
3. As an agent, I want `extract` to give no value when the regex doesn't match, so that `isempty()` finds those Logs and `summarize ... by` puts them in one group.
4. As an agent, I want `toint(extract(...))` to give a number, so that I can sum, average or compare what I extracted.
5. As an agent, I want an error that says the regex and the group must be written in the Query, so that I fix the Query instead of guessing.
6. As an agent, I want `extract`'s regex checked as `matches regex` checks its pattern, so that a bad regex fails before the Query runs, at its line and column.
7. As an agent, I want `substring(message, 0, 160)` to cut a long message, so that a table of messages stays short in my answer.
8. As an agent, I want `substring` to count characters, not bytes, so that text that isn't ASCII is never cut mid-character.
9. As an agent, I want `substring` without a length to run to the end of the string, as in KQL.
10. As an agent, I want `round(avg(duration), 2)` in a summarize, so that a table's numbers are readable.
11. As an agent, I want `round` with no precision to round to a whole number, as in KQL.
12. As an agent, I want `round` to break ties as KQL does, so that a Query copied from KQL gives the same numbers.
13. As an agent, I want `parse_json(message).resource_type` on a JSON message, so that I can group Logs by a field of the message.
14. As an agent, I want to reach a nested field such as `parse_json(message).result.cache_keys_expired`, so that I can sum a count inside the message.
15. As an agent, I want `["store-id"]` to reach a field whose name isn't a plain identifier.
16. As an agent, I want `[0]` to reach an array's first element.
17. As an agent, I want `array_length(parse_json(message).ids)`, so that I can sum how many ids each Log carried.
18. As an agent, I want a value from `parse_json` to match `== 494` when it is a number and `== "494"` when it is a string, as an Attribute does, so that the rule I already know applies.
19. As an agent, I want `toint()`, `tostring()`, `isempty()`, the aggregates and `summarize ... by` to work on a value from `parse_json`, as they do on an Attribute.
20. As an agent, I want a message that isn't JSON, a missing field, or an index past the end to give no value, not an error, so that one odd Log doesn't fail the whole Query.
21. As an agent, I want `tags.store_id` to still mean that Attribute, so that my existing Queries don't change meaning.
22. As an agent, I want a Query that uses these functions on `message` over too wide a range to be refused with a narrower Query, so that I get an answer instead of a 30-second timeout.
23. As an agent, I want `estimate` to size a Query that uses these functions, so that I can check it before I run it.
24. As an agent, I want `query_logs`' description to name these functions, so that I use them instead of `run_sql`.
25. As an agent, I want `run_sql`'s description to say that `query_logs` takes values out of messages, so that I choose SQL only when the Query language can't do the work.
26. As an agent, I want `query_language_reference` to explain each function with an example, so that I can write it right the first time.
27. As a developer in the Logs explorer, I want to use these functions in the Query box, so that I don't have to switch to SQL mode and lose the histogram and the Builder.
28. As a developer, I want the Query box to offer these functions as I type, with their arguments, as it offers the others.
29. As a developer, I want a mistake in one of these functions marked at its line and column in the Query box, as other mistakes are.
30. As a developer, I want a refused Query that uses these functions to show the same refusal, with its range and file buttons and Run anyway, as a search does.
31. As a Dashboard author, I want a Chart of `summarize count() by tostring(parse_json(message).resource_type)`, so that a Dashboard shows a field that lives only in the message.
32. As a Monitor author, I want a Threshold monitor on `avg(todouble(parse_json(message).completions_last_5m))`, so that I'm alerted on a number that only the message carries.
33. As an Instance admin, I want reads of message text through these functions to count against the text-match budget, so that agents can't get round the budget by computing over `message` instead of searching it.
34. As an Instance admin, I want such a read over a quarter of the budget to wait for a heavy-read slot, so that it doesn't starve ingest on a small VM.
35. As an Instance admin, I want `strlen(message)` and `tolower(message)` counted as text matches too, so that the rule holds for every function and not just the new ones.
36. As an Instance admin, I want fewer `run_sql` calls that scan message text, so that more of the install's heavy reads are sized before they run.
37. As the maintainer, I want a check after release of how many `run_sql` calls still take values out of messages, so that I know whether the functions did their job.

## Implementation Decisions

- **KQL's names, arguments and semantics.** The language follows KQL where KQL and APL differ (logs 11). Where KQL's docs settle an edge case (no match, a negative index, a range past the end, ties in rounding), follow them, and read them rather than guess.
- **Only the language grows.** The Query API, the validate endpoint, the refusal's shape and the MCP tools' arguments stay the same. A Query that compiled before compiles to the same thing.
- **EventStore's Query compiler** gains the five functions in its function table, and its SQL writer maps them to ClickHouse:
  - `extract`: the regex and the group must be literals. The regex is checked as for `matches regex` (RE2 in both). The group is a whole number of 0 or more. ClickHouse's regex extraction by group index fits. KQL's optional fourth argument (`typeof(...)`) isn't taken: `toint(extract(...))` does the same.
  - `substring`: KQL's index counts from 0, ClickHouse's UTF-8 substring from 1.
  - `round`: ClickHouse rounds ties in a `Float64` to even. KQL may not, so check before mapping it straight.
  - Today a function takes a fixed number of arguments. `substring` and `round` take an optional last one, and a wrong count says what each takes.
- **`parse_json` with member access, not `extract_json`.** `parse_json(message).field` is what KQL users write. Its value is dynamic, the type an Attribute read without a type already has, so comparison by the literal's type, the casts, `isempty()`, `summarize ... by` and the aggregates work on it with no new rules. `extract_json` would need `typeof()`, which is new syntax too.
  - **New syntax:** `.name`, `["name"]` and `[0]` after a dynamic value. A dotted name on its own, such as `tags.store_id`, stays one Attribute's name.
  - **Typed parts:** a dynamic value is made of typed parts (string, number, bool, JSON). For a path into a message, ClickHouse's JSON type check and its typed JSON extraction give them. The path's keys and indexes are passed as arguments, never joined into SQL text.
  - **`array_length`** gives no value for anything but an array.
- **A function of `message` is a text match.** The rule that decides whether a Query matches text (until now `search`, and `contains`, `startswith`, `=~` and `matches regex` on `message`) also holds for a Query that calls any scalar function with `message` among its arguments, anywhere in the Query. That includes the existing ones. `message` alone in `project`, and plain Log lists, are unchanged. So the explorer and MCP size, refuse and queue these Queries through the existing budget and heavy-read slots. Charts and Monitors, which are never refused, log their estimate as before.
- **Console's Query editor** lists the new functions among its scalar functions, with their arguments and one line each, so completions offer them.
- **Docs and descriptions:**
  - the language reference's scalar functions, in both copies (the docs and the MCP server's copy, kept identical by a test);
  - "Reading less" says a function of `message` counts as matching text;
  - `query_logs`' description names the functions in a few words;
  - `run_sql`'s description says that `query_logs` takes values out of messages.
- **`run_sql` is unchanged.** Putting SQL queries under the budget would need EventStore to size SQL it doesn't parse. It still stops at 10 seconds (ADR 0003).

## Testing Decisions

- **A good test runs a Query the way a caller does** and checks what comes back: the Logs, the table's rows, the error and its place, or the refusal. It never checks the SQL the compiler writes, so the mapping to ClickHouse can change without the tests changing.
- **One seam:** EventStore's Query API, run and validate, in the root package against a real ClickHouse with Logs sent in through ingest. Every story above that's about the language or the budget is checked there. No new seam is needed.
- **Prior art:**
  - `TestEachOperatorFindsLogsWhateverTheirTime` and `TestQueriesAgreeAcrossDaysWithManyKeys`: send Logs, run Queries, compare what comes back;
  - `TestValidatePlacesEachStageAndSaysWhichTheBuilderCanShow`: errors and their places;
  - `TestASearchOverMoreThanTheBudgetIsRefusedWithANarrowerQueryThatFits` and `TestQueriesThatFitTheBudgetOrDontMatchTextRunOverABusyWeek`: a refused Query, and Queries that run over a busy week seeded through the Source counts.
- **What the tests cover:**
  - `extract` with group 0, group 1 and no match;
  - `substring` with and without a length, on text that isn't ASCII;
  - `round` with and without a precision, ties included;
  - `parse_json`: a nested field, a field of each type matched by number and by string, `["name"]`, `[0]`, a message that isn't JSON and a missing field;
  - `array_length` on an array and on a value that isn't one;
  - `tags.store_id` still reads the Attribute;
  - errors for a non-literal regex or group and for a wrong number of arguments;
  - the budget refuses a function of `message` over a range past it, runs it on one file that fits, and doesn't refuse a `project message` Log list.
- **No new UI or Rails tests.** The editor's list is data that the existing completions tests already exercise, and the docs copy test already guards the two copies of the reference.

## Out of Scope

- `extract_all`, `extract_json`, the `parse` stage, `has`, `mv-expand` and `join`.
- `parse_json` of an Attribute: Attributes that hold JSON are already read as dynamic values.
- New Builder chips for these functions.
- A budget, an estimate or a heavy-read slot for `run_sql`, or any change to SQL queries.
- Changing the text-match budget's size or how its estimate is worked out.

## Further Notes

- **Findings (2026-10-27 to 11-03, production's call timings in the Meta Organization's Project).** Of 105 `run_sql` calls:
  - 40 took values out of `message` (`JSONExtractString(message, 'resource_type')`, `JSONExtractInt(message, 'result', 'cache_keys_expired')`, `extract(message, '"store_id":([0-9]+)')`);
  - 39 matched text with `LIKE` or `ILIKE`;
  - 22 cut messages with `substring`;
  - 11 used arrays, such as `JSONLength(message, 'ids')`;
  - none joined tables or read `events` or `spans`.

  One call can count in several rows. `query_logs` failed on the same gaps: `unknown function "substring"`, `"extract"` and `"round"`.
- **The same check after release:** in Organization `solidtrace`, Project `solidtrace`, `where kind == "mcp_tool" and tool == "run_sql"`, with the SQL in `arguments.sql`. Over the week after the release, the share of `run_sql` calls that use `JSONExtract`, `extract` or `substring` should fall, and `query_logs` should show no `unknown function` errors for these names.
- **Related:** the text-match budget and heavy-read slots are query timeouts 02 and 04 (and 07 for the budget on Settings → Instance). The two languages were decided in logs 11 and ADR 0003.
