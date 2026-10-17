# 08: Issue tools

**Status:** resolved
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

### 2026-10-17 resolved

Built on `feature/mcp` in workflow `wf_7acd8b1c-eef` (implement, one review and one verify, a skeptic on high and medium findings, fixes, recheck). Head `11e0b9f`. No finding confirmed above low. Decisions are under 08 in the spec's Implementation decisions; findings left for later are in [carry-over.md](../carry-over.md).
