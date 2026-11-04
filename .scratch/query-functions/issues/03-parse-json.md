# 03: parse_json(message) in Queries

**What to build:** many Logs' messages are JSON, and agents read their fields with SQL's JSON functions through `run_sql`. Add KQL's `parse_json`, with `.name`, `["name"]` and `[0]` to reach into its value, and `array_length`. Then `summarize sum(toint(parse_json(message).result.cache_keys_expired)) by tostring(parse_json(message).resource_type)` works wherever a Query runs: the Logs explorer, Charts, Monitors and `query_logs`. A Query that uses `parse_json(message)` over too wide a range is refused (01). See the [spec](../spec.md)'s Implementation Decisions: `parse_json` with member access, not `extract_json`.

**Blocked by:** 01 (A function of message counts as matching text)

**Status:** resolved

- [x] **`parse_json(x)`** takes a string, usually `message`, and gives a dynamic value: a string, a number, a bool, an array or an object. When `x` isn't JSON, it gives no value. *(Decided otherwise, as KQL's docs say: it gives `x` as a string. A member of it gives no value. See Comments.)*
- [x] **Reaching into a value.** After a dynamic value, `.name`, `["name"]` and `[0]` give its member or element, also dynamic, to any depth.
  - `["name"]` reaches a member whose name isn't a plain identifier, such as `["store-id"]`.
  - A missing member, an index past the end, or reaching into a value that isn't an object or array gives no value, never an error.
  - This is new syntax after a call. A dotted name on its own, such as `tags.store_id`, stays one Attribute's name.
- [x] **The value behaves as an Attribute read without a type does:**
  - `parse_json(message).store_id == 494` matches numbers, and `== "494"` strings;
  - `toint()`, `tostring()`, `isempty()`, `summarize ... by` and the aggregates work on it.
- [x] **The path is data:** the members' names and indexes reach ClickHouse as arguments, never joined into the SQL's text.
- [x] **`array_length(x)`** gives an array's length, or no value when `x` isn't an array.
- [x] **Mistakes are placed:** `.name` or `[...]` after a value that can't be dynamic, an index that isn't a whole number written in the Query, and a wrong count of arguments are errors with a line and column.
- [x] **A text match:** a Query with `parse_json(message)` over a busy week is refused with a narrower Query, and runs on one file that fits.
- [x] **The Query box** offers `parse_json` and `array_length` as you type, with their arguments and one line each.
- [x] **Docs:**
  - the language reference's scalar functions, in both copies, with an example on a JSON message;
  - `query_logs`' description mentions `parse_json(message).field` in a few words;
  - `run_sql`'s description says that `query_logs` takes values out of messages, naming `parse_json`. Add the sentence if 02 hasn't, or add the name to it.
- [x] **Tests** run Queries through EventStore's Query API, run and validate, against ClickHouse:
  - a nested member;
  - a member of each type, matched by `==` with a number and with a string;
  - `["name"]` and `[0]`;
  - a message that isn't JSON, and a missing member;
  - `array_length` on an array and on a value that isn't one, summed in a summarize;
  - `summarize count() by tostring(parse_json(message).resource_type)`;
  - `tags.store_id` still reads the Attribute;
  - the errors above, with their places;
  - the refusal over a busy week.

## Comments

**2026-11-04, built on `query-functions/03-parse-json`, from `feature/query-functions` at `32548021`, with 02 merged in at `29d59df1`.**
- **Parser** (`logquery/parser`): `MemberExpr` (`.name`) and `IndexExpr` (`[…]`) follow any operand, any number of them. `.name` follows a value that isn't a name and must touch it, so `tags.store_id` still scans as one name and `['a b'].c` is still an error. `[…]` follows anything, a name too: `["name"]` scans as one quoted-name token, which after a value is read as a key.
- **Compiler** (`logquery/expr.go`):
  - `parse_json` and `array_length` are in the function table, in their own block after 02's, with `usage` for a wrong count ("parse_json takes one string, as in parse_json(message)").
  - A path is a `member{x, path}`, a dynamic value; a path into a path is one path. It is read as want asks, as a name of no fixed type is (`typed`, which wraps a dynamic call too), so `== 494`, `== "494"`, `in`, the casts, `isempty()`, the aggregates and `summarize … by` need no new rules. An unnamed column is named after the path's last name (`resource_type`).
  - `contains` on a value from `parse_json` or a path looks in its string, or in an array's or object's JSON text, as on an Attribute (`jsonValueText`). The ticket didn't ask; without it `parse_json(message).ids contains "8"` found nothing. A dynamic column still compiles as before.
- **SQL** (`logquery/sql.go`): a path is one `JSONExtractRaw(text, 'name', 1, …)`, each name a quoted argument and each index a number, never a JSONPath string. Its parts are read from that raw text only once `JSONType` says their type, since `JSONExtractFloat` reads `"12"` as 12. An index needs `JSONType(…) = 'Array'` at that step, since ClickHouse's index takes an object's nth member too. `[0]` is ClickHouse's 1; `[-1]`, the last, as in KQL.
- **Decided otherwise / left open:**
  - **A string that isn't JSON:** KQL's docs say `parse_json` gives "the original string value", so `parse_json(message)` on `not json` gives that string; any member of it gives no value. The ticket said no value. Switching is one line in `parsedParts`.
  - **Negative indexes** count from the end, as KQL's dynamic docs show (`arr[(-1)]`). "A whole number" in the error means an integer; `[1.5]`, `[x]` and `[1+1]` are refused.
  - **`.name` after a column** isn't member access: `extend j = parse_json(message) | where j.a == 1` reads the Attribute `j.a`, as a dotted name always has (spec: a Query that compiled before compiles the same). `j["a"]` reaches into the column. The reference says a dotted name stays an Attribute.
  - **`[…]` after an Attribute** reaches into its JSON text, so `codes[1]` and `array_length(codes)` work on an array Attribute.
  - The arity message comes from 02's `usage`; my test checks its place and that it starts "parse_json takes".
- **Console and docs:** `parse_json` (writes `parse_json(message)`) and `array_length` after 02's `round` in `SCALAR_FUNCTIONS`. Two rows after `round` in both copies of the reference, and a "JSON messages" paragraph with the `cache_keys_expired` example. `query_logs` names `parse_json(message).result.count`; `run_sql`'s sentence now ends "with extract and parse_json."
- **Tests** (`event_store/logs_parse_json_test.go`): a nested member summed and grouped, also by the dynamic value and in hourly bins (a Chart's); each type by `==` with a number and a string, `!=`, `in`; `["name"]`, `['name']`, `[0]`, `[-1]`, nested arrays; past the end, missing, not JSON, a member of a string, an index into an object; names with quotes, dots, `$`, brackets and a backslash; `tags.store_id`; an array Attribute; `contains`; `array_length` on arrays and not, summed; a function of 02's on a member; each mistake with its place; the refusal over `tagalysSources`' week with its narrower Query found, and the run on `cron.log`. Two mutation checks (no array guard; no `member` in `anyOf`) failed the tests as they should.
- **Run:**
  - Go: `./logquery/...` and `./handler/` pass. The root package with `-run 'Budget|Refused|Heavy|Estimate|Histogram|Force|FunctionOfMessage|Operator|Validate|Search|Queries|Tabular|Scalar|Pasted|Contains|Where|Parameter|Filter|Chart|BinCache|Sort|Take|ParseJSON|Paths|Totals|Readable|SQL|Extract|Substring|Round'` passes after the merge with 02 (227 s).
  - Console: `bun run typecheck` passes; `completions.test.ts` passes (23). `test/mcp/logs_tools_test.rb` passes (27), the copies matching. Rubocop is clean on the two tools.

**2026-11-04, review fixes on `query-functions/review-fixes`, from `feature/query-functions` at `d2e6d877`.** Findings 1–7 of the whole-effort review.
- **`message` carried through a stage** (finding 1, test-first): a column that holds `message` as it is, renamed or not, counts as `message` through `project`, `extend`, `summarize … by` and `distinct`. A function of it, or a text match on it, is a text match. A stage marks the column when its value is `message` or another such column (`column.message`, `isMessage` in `sql.go`). `project time, message | take 10`, `extend m = message | project m`, and a column named `message` that holds another Field, are unchanged. "Reading less" in both copies of the reference says so. This closes 01's left-open note.
- **`parse_json`'s own expression** (finding 3): `parsedJSON` replaces the `call` that four places recognised by its name. They are now type switches beside `member`'s. The compiler makes one after the usual checks of arity and argument type. The SQL is unchanged.
- **One refusal helper** (finding 4): `assertRefusedAsASearchIs` in `logs_read_budget_test.go`, which 01's, 02's and 03's busy-week tests and finding 1's new test use. It checks what each of them checked. So 02's and 03's tests now check the 5 busiest files, the message and the on-file estimate too, and 01's checks the answer on `cron.log` as well.
- **Text** (findings 2, 5, 6, 7): ADR 0002 has an amendment for the path syntax; `run_sql` names `query_logs`; `query_logs` names `array_length` and says "member"; `writeMember`'s comment says "the empty string". Two older curly quotes from before this effort (`sql.go`'s `stringColumns` and dynamic-tuple comments) are left as they were.
- **Tests:** `TestMessageCarriedThroughAStageIsRefusedAsASearchIs` failed before the fix (the first Query ran over the week) and passes after it. Go: the root package with `-run 'Budget|Refused|Heavy|Estimate|Histogram|Force|FunctionOfMessage|Operator|Validate|Search|Queries|Extract|Substring|Round|ParseJSON|Paths'` passes (138 s), and `./logquery/...` and `./handler/` pass. Console: `bun run typecheck` passes, `test/mcp/logs_tools_test.rb` passes (27) with the copies matching, and Rubocop is clean on the two tools.
