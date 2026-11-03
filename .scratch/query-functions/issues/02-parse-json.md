# 02: parse_json(message) in Queries

**What to build:** many Logs' messages are JSON, and agents read their fields with `run_sql`'s `JSONExtractString(message, ...)`. Add KQL's `parse_json`, with `.name`, `["name"]` and `[0]` to reach into its value, and `array_length`, so that `parse_json(message).result.cache_keys_expired` works in a Query. See the [spec](../spec.md) (decision 2: member access, not `extract_json`).

**Blocked by:** 01 (it changes the same function table, `MatchesText` and docs sections)

**Status:** ready-for-agent

- [ ] **`parse_json(x)`** takes a string, usually `message`, and gives a dynamic value: a string, number, bool, array or object, or no value when `x` isn't JSON.
- [ ] **Reaching into a value.** After a dynamic value, `.name`, `["name"]` and `[0]` give its member or element, also dynamic. A missing member, an index past the end, or reaching into a value that isn't an object or array gives no value.
  - This is new syntax after a call: today a dotted name such as `tags.store_id` is one Attribute's name (`event_store/logquery/parser`). That must stay so.
  - `["name"]` reaches a member whose name isn't a plain identifier, such as `["store-id"]`.
- [ ] **The value behaves as an Attribute read without a type does** (`dynamicType`):
  - `parse_json(message).store_id == 494` matches numbers, and `== "494"` strings;
  - `toint()`, `tostring()`, `isempty()`, `summarize ... by` and the aggregates work on it.
  - In ClickHouse, the parts by type can come from `JSONType` and `JSONExtractString` / `JSONExtractFloat` / `JSONExtractBool` / `JSONExtractRaw` with the path's keys and indexes. Write the path as arguments, never by joining text.
- [ ] **`array_length(x)`** gives an array's length, or no value when `x` isn't an array. Example: `summarize sum(array_length(parse_json(message).ids))`.
- [ ] **A text match.** A Query with `parse_json(message)` counts as matching text, as in 01.
- [ ] **The editor** lists `parse_json` and `array_length` in `SCALAR_FUNCTIONS`.
- [ ] **Docs:**
  - "Scalar functions" in both copies of `query-language.md`, with an example of a JSON message;
  - `query_logs`' description mentions `parse_json(message).field` in a few words;
  - `run_sql`'s description says that `query_logs` takes values out of messages with `extract` and `parse_json`.
- [ ] **Tests** (root, against ClickHouse):
  - a nested member;
  - a member of each type, matched by `==` with a number and with a string;
  - `["name"]` and `[0]`;
  - a message that isn't JSON, and a missing member;
  - `array_length`, on an array and on a value that isn't one;
  - `summarize count() by tostring(parse_json(message).resource_type)`;
  - `tags.store_id` still reads the Attribute;
  - the budget refuses `parse_json(message)` over a range past it.

## Comments
