# 02: extract, substring and round in Queries

**What to build:** agents use `run_sql` to take a value out of a message, cut it short or round a number, because the Query language can't do any of the three. Add KQL's `extract`, `substring` and `round`, with KQL's arguments and meaning. They then work wherever a Query runs: the Logs explorer, Charts, Monitors and `query_logs`. A Query that uses them on `message` over too wide a range is refused (01). See the [spec](../spec.md).

**Blocked by:** 01 (A function of message counts as matching text)

**Status:** ready-for-agent

- [ ] **`extract(regex, captureGroup, source)`** gives the text that the capture group matched, as a string. Group 0 is the whole match.
  - The regex and the group must be written in the Query: a string, and a whole number of 0 or more.
  - The regex is checked as `matches regex` checks its pattern (RE2 in both), so a bad regex fails before the Query runs.
  - No match, or a group the regex doesn't have, gives no value, so `isempty()` is true. Read KQL's docs for whether that is an empty string or null, and follow them.
  - KQL's optional fourth argument (`typeof(...)`) isn't taken: `toint(extract(...))` does the same.
- [ ] **`substring(source, startingIndex [, length])`** counts characters, not bytes, from a zero-based index. Without a length, it runs to the end. Follow KQL's docs for a negative index and a range past the end.
- [ ] **`round(number [, precision])`** rounds to a whole number when the precision is left out. Ties go as KQL's docs say. ClickHouse rounds ties in a `Float64` to even, so check that before mapping straight to its `round`.
- [ ] **Optional last argument:** `substring` and `round` take one fewer argument than their full form. Functions take a fixed number of arguments today, so this has to change. A wrong count gives an error that says what each function takes.
- [ ] **Mistakes are placed:** a regex or group not written in the Query, a bad regex and a wrong count are errors with a line and column, which the Query box marks.
- [ ] **A text match:** `summarize count() by extract("…", 1, message)` over a busy week is refused with a narrower Query, and runs on one file that fits.
- [ ] **The Query box** offers the three as you type, with their arguments and one line each.
- [ ] **Docs:**
  - the language reference's scalar functions, in both copies, with an example each;
  - `query_logs`' description names the three in a few words;
  - `run_sql`'s description says that `query_logs` takes values out of messages, naming `extract`. Add the sentence if 03 hasn't, or add the name to it.
- [ ] **Tests** run Queries through EventStore's Query API, run and validate, against ClickHouse:
  - `extract` with group 0, group 1 and no match, and `toint(extract(...))` summed;
  - `substring` with and without a length, on text that isn't ASCII;
  - `round` with and without a precision, ties included, in a summarize;
  - the errors above, with their places;
  - the refusal over a busy week.
