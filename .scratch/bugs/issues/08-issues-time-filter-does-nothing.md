# 08: The Issues page's time filter does nothing

**Status:** resolved
**Area:** Console
**Found:** 2026-10-18, reported by the user

## What happens

The Issues page has a range select (Last 24 hours, Last 14 days, Last 30 days), but it only has a `defaultValue` (`console/app/frontend/pages/Issues/Index.tsx`). It has no `onValueChange`, the filters don't carry a range, and neither `IssuesController#index` nor `IssueList` reads one. Choosing a range changes the label and nothing else: the page lists every Issue, however long ago it was seen.

## Fix

- `IssueList` takes a range, one of `24h`, `14d` and `30d`, and keeps the Issues last seen within it. An Issue with no counted Event is last seen when it was created, as the list already sorts it.
- The Issues page defaults to `14d`, the range the select already shows, and carries the range in its filters and URL like the other filters.
- The MCP's `list_issues` doesn't pass a range, so it still lists every Issue.
- Controller tests for the default and a chosen range, a vitest test for the filter URL, and a Playwright step that picks a range.

## Comments

### 2026-10-18 resolved

- `IssueList` takes `range:`, a key of `IssueList::RANGES` (`24h`, `14d`, `30d`), and keeps the Issues whose `COALESCE(last_seen_at, created_at)` falls within it. A blank range keeps every Issue, so the MCP's `list_issues` is unchanged.
- `IssuesController#index` defaults the range to `14d`, the range the select showed, falls back to it for an unknown range, and passes it back in `filters`.
- The page's select is controlled by `filters.range` and visits the page with the new range. The filter URL moved to `lib/issues/filters.ts`, which leaves `range` out at `14d` as it does the other defaults.

Tests:

- `test/controllers/issues_controller_test.rb`: the 14-day default, a chosen range, and an unknown range. Both new tests failed before the change. Two older tests, whose Issues were seen in January, now travel to January.
- `app/frontend/lib/issues/filters.test.ts`: the filter URL.
- `e2e/flows/issues.spec.ts`: picks Last 24 hours and Last 30 days and sees the list change. Run on this worktree's own Console (port 4011) and passed.

**To check with the user:** the page now hides Issues not seen in 14 days unless the range is widened, as the select always claimed. There is no "all time" option. An MCP `list_issues` link to the Issues page doesn't carry a range, so the page it opens shows 14 days while the tool lists every Issue.
