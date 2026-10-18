# 13: What Claude Code found using the MCP server

**Status:** needs-triage
**Track:** Rails
**Blocked by:** none

## The check

On 2026-10-18 Claude Code was connected to the dev stack's MCP endpoint, which runs `main` (`d680843`), and told to use the tools as an agent would.

- **Setup:**
  - an API token for the dev admin
  - `claude -p --mcp-config <file> --strict-mcp-config` with the server at `http://localhost:4001/mcp` and the token in an `Authorization: Bearer` header
  - Sonnet 5.5
- **Left out:** the key tools, `invite_member`, `create_project` and `run_monitor`. Every other tool was called.
- **Two runs:**
  - The first was 30 calls in 54 s. It covered whoami, the Logs tools (`run_sql` included), a scratch Dashboard with a Chart and a scratch Monitor.
  - The second was 12 calls in 36 s. It covered `get_issue`, `update_issue`, `add_comment` and `get_monitor`. Before it, `dev:data`'s six web Issues were sent.
- **Result:** every call worked, and `run_sql` on `logs_hot_1` was refused with "SQL queries can read only the logs and events views: logs_hot_1 isn't allowed". Every answer was compact and ended with a Console link.
- **Clean-up:** the scratch Dashboard, Chart and Monitors were deleted through the tools. The test comment was deleted afterwards in Rails, since no tool deletes a comment.
- **Kept:** the transcripts (`run.jsonl`, `run2.jsonl`) and the config (with the token, mode 600) are in `~/.cache/solid-trace/mcp-check/`. The token is still valid; it's named "Claude Code MCP check, 2026-10-18".

## Findings

Checked against the tool list:

1. **`create_monitor` can't make a Monitor that is off.** It has no `enabled` parameter, though `update_monitor` has one. So a new Monitor runs at once and tells its Integrations, before the agent or the member has checked it. The agent worked around it by making one with no Integrations and a 1,440-minute interval, then turning it off. Add `enabled` to `create_monitor`, or make agent-made Monitors start off.
2. **The update tools describe creation defaults.** For example:
   - `update_monitor`'s `interval_minutes` says "1 when made"
   - `range_minutes` says "5 when made"
   - `update_chart`'s `w` says "6 when added"
   An agent can read that as "leaving it out resets it". On an update, say that a value left out stays as it is.
3. **A 7-day Chart's table is cut at 100 rows.** For example, Logs by level in 3-hour bins comes to 280 rows, so `get_chart_data` answers "177 more: narrow the Query.". That's documented, but the agent can't see the whole Chart.
4. **Paging a relative range is awkward.** The next page needs "the same query, from and to" with the cursor, so the agent had to move from `range` to the first page's `from` and `to`. Either take `range` with a cursor (and resolve it as the first page did), or put the first page's `from` and `to` beside the cursor.
5. **Smaller ones:**
   - `get_issue` leaves out the trace id when the Event has none, so "none" and "not supported" read the same.
   - A `match` Monitor is made with `range_minutes` 5, which it doesn't use.
   - `query_language_reference` is about 3,000 words in one answer. A section parameter would let an agent fetch only a stage or an aggregate.
   - An empty `query_logs` answer doesn't say whether the Project has Logs at all.

## Not checked

- `run_monitor`, so Integrations weren't told anything.
- The key tools and `invite_member`.
- claude.ai's web connector, which needs OAuth (docs/mcp.md).
