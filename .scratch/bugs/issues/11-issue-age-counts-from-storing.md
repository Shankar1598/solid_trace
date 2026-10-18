# 11: An Issue's age counts from when Console stored it, not its first Event

**Status:** resolved
**Area:** Console
**Found:** 2026-10-18, reported by the user

## What happens

The Issues page's Age column is `formatDistanceToNow(issue.created_at)` (`console/app/frontend/pages/Issues/Index.tsx`). `created_at` is when Event processing inserted the `issues` row, not when the Issue's first Event happened. Event processing catches up from its processing cursor, so Issues whose Events were stored over days but processed together, after a restart or a backlog, all show the same age.

`issues.first_seen_at` already holds the earliest Event time: `RecordSeenEvents` keeps it as `MIN(first_seen_at, event timestamp)` (`event_store/storage/sqlite.go`). `IssueSerializer` sends it as `first_seen_at`, falling back to `created_at` until an Event is counted, and the list's "First seen" sort already uses it.

## Fix

- The Age column counts from `first_seen_at`.
- A vitest test for the age, and a Playwright step that sees two Issues of different first Events show different ages.

## Comments

### 2026-10-18 resolved

The Age column now shows `issueAge(issue)` (`lib/issues/times.ts`), the time since `first_seen_at`, which the serializer already sends, falling back to `created_at` until an Event is counted. No Rails or EventStore change was needed.

Tests:

- `app/frontend/lib/issues/times.test.ts`: an Issue stored an hour ago whose first Event was 5 days ago is 5 days old.
- `e2e/flows/issues.spec.ts`: three Issues stored together, with first Events 2 hours, 5 days and 20 days ago, show those ages. Run on this worktree's own Console and passed.
