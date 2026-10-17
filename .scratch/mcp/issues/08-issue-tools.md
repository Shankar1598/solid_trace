# 08: Issue tools

**Status:** ready-for-agent
**Track:** Console
**Blocked by:** 02

## What to build

The Issues row of the spec's Tool catalogue.

- **`list_issues`** takes project (optional), status, sort and page, and filters as `IssuesController#index` does.
- **`get_issue`** returns the Issue and its latest or a given Event, through `EventStore.query_event_with_context!` as `IssuesController#load_events` reads it.
  - The Event is compact: exception type and value, the top in-app frames, tags and the trace id.
  - When the Event has a trace id, the answer hints at `query_logs` with `where trace_id == "…"`.
- **`update_issue`** resolves, unresolves or assigns (by member email, or `unassigned`).
- **`add_comment`** adds a comment.
- `IssuesController#resolve` and `#unresolve` use the status enum instead of `0` and `1`.
- Every answer carries the Issue's link.

## Acceptance

- `console/test/mcp/issue_tools_test.rb`:
  - list filters;
  - an Event shown while EventStore is down;
  - resolve, unresolve and assign;
  - a non-member's email refused;
  - comment.
- `bin/rails test` passes.

## Comments
