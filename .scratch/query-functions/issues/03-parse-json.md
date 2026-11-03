# 03: parse_json(message) in Queries

**What to build:** many Logs' messages are JSON, and agents read their fields with SQL's JSON functions through `run_sql`. Add KQL's `parse_json`, with `.name`, `["name"]` and `[0]` to reach into its value, and `array_length`. Then `summarize sum(toint(parse_json(message).result.cache_keys_expired)) by tostring(parse_json(message).resource_type)` works wherever a Query runs: the Logs explorer, Charts, Monitors and `query_logs`. A Query that uses `parse_json(message)` over too wide a range is refused (01). See the [spec](../spec.md)'s Implementation Decisions: `parse_json` with member access, not `extract_json`.

**Blocked by:** 01 (A function of message counts as matching text)

**Status:** ready-for-agent

- [ ] **`parse_json(x)`** takes a string, usually `message`, and gives a dynamic value: a string, a number, a bool, an array or an object. When `x` isn't JSON, it gives no value.
- [ ] **Reaching into a value.** After a dynamic value, `.name`, `["name"]` and `[0]` give its member or element, also dynamic, to any depth.
  - `["name"]` reaches a member whose name isn't a plain identifier, such as `["store-id"]`.
  - A missing member, an index past the end, or reaching into a value that isn't an object or array gives no value, never an error.
  - This is new syntax after a call. A dotted name on its own, such as `tags.store_id`, stays one Attribute's name.
- [ ] **The value behaves as an Attribute read without a type does:**
  - `parse_json(message).store_id == 494` matches numbers, and `== "494"` strings;
  - `toint()`, `tostring()`, `isempty()`, `summarize ... by` and the aggregates work on it.
- [ ] **The path is data:** the members' names and indexes reach ClickHouse as arguments, never joined into the SQL's text.
- [ ] **`array_length(x)`** gives an array's length, or no value when `x` isn't an array.
- [ ] **Mistakes are placed:** `.name` or `[...]` after a value that can't be dynamic, an index that isn't a whole number written in the Query, and a wrong count of arguments are errors with a line and column.
- [ ] **A text match:** a Query with `parse_json(message)` over a busy week is refused with a narrower Query, and runs on one file that fits.
- [ ] **The Query box** offers `parse_json` and `array_length` as you type, with their arguments and one line each.
- [ ] **Docs:**
  - the language reference's scalar functions, in both copies, with an example on a JSON message;
  - `query_logs`' description mentions `parse_json(message).field` in a few words;
  - `run_sql`'s description says that `query_logs` takes values out of messages, naming `parse_json`. Add the sentence if 02 hasn't, or add the name to it.
- [ ] **Tests** run Queries through EventStore's Query API, run and validate, against ClickHouse:
  - a nested member;
  - a member of each type, matched by `==` with a number and with a string;
  - `["name"]` and `[0]`;
  - a message that isn't JSON, and a missing member;
  - `array_length` on an array and on a value that isn't one, summed in a summarize;
  - `summarize count() by tostring(parse_json(message).resource_type)`;
  - `tags.store_id` still reads the Attribute;
  - the errors above, with their places;
  - the refusal over a busy week.
