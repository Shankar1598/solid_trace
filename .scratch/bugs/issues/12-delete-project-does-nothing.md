# 12: Delete project does nothing

**Status:** resolved
**Area:** Console
**Found:** 2026-10-18, reported by the user

## What happens

The Project page's Danger zone has a "Delete project" button (`console/app/frontend/pages/Projects/Show.tsx`) with no handler. There's no `destroy` route or action for Projects either, so nothing can delete one, from the page or anywhere else in Console.

The model half expects deleting: `Project` declares its Issues, Issue fingerprints, keys, Dashboard charts and Monitors `dependent: :destroy`. But two tables that EventStore writes, `project_issue_counters` and `project_seen_cursors` (ADR 0001), have foreign keys to `projects` and no association, so `project.destroy` would fail on them once Event processing had written their rows: once the Project has any Issue.

## Fix

- A `destroy` route and action, for the Organization's admins only, as editing a Project is (`ProjectPolicy`).
- `Project` deletes its `project_issue_counters` and `project_seen_cursors` rows too. No table or enum changes, so EventStore doesn't change.
- The button opens a confirmation, as deleting a Monitor or a Dashboard does, and only admins see it.
- What EventStore keeps for the Project: see Comments.
- Model and controller tests, and a Playwright flow that deletes a Project.

## Comments

### 2026-10-18 resolved (Console side)

- **Route and action:** `DELETE /:org_slug/projects/:slug` (`ProjectsController#destroy`) authorizes with `ProjectPolicy#destroy?` (admins, as `update?`), `destroy!`s the Project and redirects to the Projects list with "Project deleted.". A member gets the usual "not authorized" redirect.
- **Model:** `Project` now `has_one :issue_counter` and `has_one :seen_cursor` (a new `ProjectSeenCursor` model, keyed by `project_id` like `ProjectIssueCounter`), both `dependent: :delete`. Everything else was already `dependent: :destroy`: Issues (with their fingerprints and comments), keys, Monitors (which resolve their firing groups first) and Dashboard charts. The Dashboards themselves stay. Notifications already waiting for a deleted Issue are skipped as "Issue missing", as the outbox already does. No table or enum changed, so `storage/sqlite.go` didn't either.
- **Page:** only admins see the Danger zone (`can_delete_project`). Its button opens a confirmation, the same `AlertDialog` as deleting a Monitor or a Dashboard. The old text promised the Project's Events would go too, which isn't true (below), so it now lists what is deleted.

Tests:

- `test/models/project_test.rb`: deleting a Project with an Issue, a fingerprint, a comment, a key, a Monitor, a Dashboard chart and a `project_seen_cursors` row removes them all, keeps the Dashboard and other Projects' Issues. It failed on the foreign key before the change.
- `test/controllers/projects_controller_test.rb`: an admin deletes, a member can't, and only admins are offered it. All three failed before.
- `e2e/flows/project-delete.spec.ts`: a member sees no button, and an admin cancels once, then deletes. Run on this worktree's own Console and passed.

**What EventStore keeps** (no change made; it needs EventStore work):

- **Logs** go: Log cleanup's daily pass already deletes every Log of a Project whose `projects` row is gone (`logprocessing/cleanup.go`, `DeleteProjectLogs`), within a day.
- **Events stay.** Nothing deletes a Project's Events: their payloads in Pebble (`KeyForEvent`), their rows in `events_hot`, and the Event archive's Parquet files, which are moved by date, not by Project. Event processing's cursor for the Project stays in Pebble too. Console can no longer reach them, and Project ids aren't reused (SQLite `AUTOINCREMENT`), but they take disk until something deletes them.
- **A race:** Events stored but not yet processed when the Project is deleted are still processed. EventStore's SQLite connection doesn't turn on foreign keys, so `FindOrCreateIssue` would insert Issues and a counter row for the missing Project. They are unreachable but stay. New Events are refused once the keys are gone.

**To check with the user:** whether EventStore should delete a deleted Project's Events, as Log cleanup does its Logs. Deleting a Project with very many Issues runs one `destroy` per Issue in the request, as `dependent: :destroy` already asked; it may be slow on a big Project.
