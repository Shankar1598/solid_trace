# 09: The Issues page's checkboxes select nothing

**Status:** resolved
**Area:** Console
**Found:** 2026-10-18, reported by the user

## What happens

Every row of the Issues page has a checkbox, and the header has a select-all checkbox (`console/app/frontend/pages/Issues/Index.tsx`), but none of them is wired to anything. They hold no state, the select-all box doesn't tick the rows, and there's no action to take on the ticked Issues. Console has no route that changes more than one Issue.

## Fix

- The page keeps the ticked Issues. The header box ticks or clears every Issue on the page, and shows when only some are ticked.
- With any ticked, the page offers the actions one Issue already has for its status: Resolve and Unresolve. They change the ticked Issues as the Issue page does, one `update!` each.
- A route for it, `PATCH /:org_slug/issues/status` with `ids` and `status`, which changes only the Organization's Issues.
- Assigning many Issues at once is left out: the Issue page's assignee picker searches members, and each change notifies the Integrations once per Issue.
- Controller tests, a vitest test for the selection, and a Playwright step that ticks Issues and resolves them.

## Comments

### 2026-10-18 resolved

- **Route and action:** `PATCH /:org_slug/issues/status` (`IssuesController#update_status`) takes `ids` and `status`, `update!`s each of the Organization's Issues among the ids, as `resolve` and `unresolve` do, and redirects back with "2 Issues resolved". An unknown status answers 400. Ids of another Organization's Issues are skipped.
- **Page:** the Issues page keeps the ticked ids (`lib/issues/selection.ts`). The header box ticks or clears the whole page, and shows a dash when only some are ticked. With any ticked, a bar above the table shows "N selected" with Resolve and Unresolve. A new page, filter or change clears the ticks.
- **Not built:** bulk assigning, as the Fix above says.

Tests:

- `test/controllers/issues_controller_test.rb`: resolve and unresolve, the redirect back, another Organization's Issues, an unknown status. All three failed before the route existed.
- `app/frontend/lib/issues/selection.test.ts`: ticking one, all, and the header box's state.
- `e2e/flows/issues.spec.ts`: ticks two Issues and resolves them, then ticks all and unresolves them. Run on this worktree's own Console and passed.

**Found on the way, not fixed:** the Issue page's own Resolve and Unresolve buttons send `PUT` (`pages/Issues/Show.tsx`), but the routes are `PATCH` only, so both answer a routing error. Checked with `recognize_path`. It dates from `6688f7b`.

### 2026-10-18 the user's answer

- **Bulk assigning:** skipped (user). It isn't planned.
- **The `PUT` found above:** fixed on `main` (`f7c85f9`). Resolve and Unresolve send `PATCH`, and a flow clicks both on the Issue's page.
