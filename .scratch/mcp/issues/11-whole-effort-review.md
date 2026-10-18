# 11: Whole-effort review

**Status:** ready-for-human
**Track:** both
**Blocked by:** 10

## What to build

One review of `feature/mcp` against `feature/logs-v2` for standards and spec, then one verify pass. Only high and medium findings are fixed. Lows go in Comments. Then hand to the user to try `claude mcp add` against the dev stack, and decide with them where `feature/mcp` merges.

## Comments

### 2026-10-18 review

One review of `feature/logs-v2...feature/mcp`, which covers tickets 01–10, bugs 01–04 and the looks-pass merge. It looked only at what the units' own reviews couldn't see. It found nothing high and nothing medium in the code.

**One medium, in this effort's spec, fixed:** commit `43bf45e`, which recorded the build's decisions, had replaced the plan's text instead of adding after it. These were lost from `spec.md`:
- the Console and Go tracks' steps;
- G2's rules and test cases;
- "Running it", Critical files and Verification.

The `## Implementation decisions` heading ended up inside an unclosed code span, and G1 and G2 read as Set-up decisions. They are restored from `6774046`, with the decisions under their own heading again.

**Checked and sound:**
- **Authorization across all 34 tools.** Organizations come from the user's kept memberships, and every record is found through them: Projects, Dashboards, Charts, Monitors, `integration_ids`, Issues, assignees, keys and members. `ProjectKeyPolicy` and `MemberInvite` refuse a member before changing or revealing anything. A Query's `['slug']` is only a label; EventStore reads by `project_id`.
- **UI and tools agree:** `LogMonitor#save_with`, `ChartRuns.run_chart`, `IssueList`, `MemberInvite`, comments, the Project validations and `can_manage_keys`.
- **Spec coverage:** every catalogue tool exists and matches its row or a recorded decision. Each of the Verification section's integration cases is tested; "another Organization's Project" is tested in `base_test.rb` and in each tool area's tests.
- **Tokens:**
  - only the digest is stored, and the secret is answered once as JSON;
  - `filter_parameters` covers `token` and `secret`, and Sentry drops `Authorization`;
  - a reset revokes the tokens, and deleting a user deletes them;
  - `last_used_at` is written at most once a minute.
- **The read-only SQL check:** a scratch probe on DuckDB 1.5.6 found no escape. Denied calls are found inside DESCRIBE, SUMMARIZE, SHOW, ORDER BY, QUALIFY, PIVOT … IN, lambdas, FILTER, LIMIT, VALUES, GROUPING SETS and AT (VERSION). Beyond that:
  - no built-in macro or alias reaches a denied function;
  - `duckdb_secrets(redact := false)` is refused;
  - `arrow_scan` can't be called;
  - `read_csv`'s rejects tables die with their connection.
- **The bug fixes and `d3de5fa`:** the profile form, the tokens section, the key button, the reset pages, email normalization and the migration.

**Verify:** the medium was confirmed by reading `git show 6774046:.scratch/mcp/spec.md` against the file, and fixed. No code changed in the fix. These passed after the last code change, in ticket 10:
- Rails (575 runs), vitest (391) and typecheck;
- all 77 Playwright flows.

EventStore is unchanged since the Go suite passed at `41d462f`.

**Left for the user:** try `claude mcp add` against the dev stack, which runs `feature/mcp` from the worktree, and decide where `feature/mcp` merges. Lows stay in [carry-over.md](../carry-over.md), the eleven marked 11 there.
