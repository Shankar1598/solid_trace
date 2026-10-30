# 07: The text-match budget on Settings → Instance

**What to build:** the budget for Queries that match text (02) lives in EventStore's `event_store.yml`. Changing it means editing the VM's config and restarting EventStore, and Settings → Instance doesn't show it at all. Make it an instance setting, like SQL queries and the default Retention:
- Instance admins edit it on Settings → Instance and over MCP (`update_instance_settings`).
- It applies from the next Query, with no restart.

EventStore already reads Console's `instance_settings` row from Console's SQLite on every call (`event_store/storage/console_settings.go`), so this uses the same path. See the [spec](../spec.md).

Asked for by the user on 2026-10-30. The 4 GiB default covers only the last 24 minutes of `tagalys-verbose` (about 233 GiB of text a day, all in one file). The VM runs 10 GiB from its `event_store.yml` until this ships.

**Blocked by:** None (can start immediately)

**Status:** resolved

- [x] **Console.** Add `instance_settings.logs_text_match_budget_mb`: an integer, default 4096, not null, 0 or more, where 0 refuses no Query.
  - It goes in `InstanceSetting::EDITABLE`, with its validation.
  - It shows on Settings → Instance, with text saying what it limits, that Dashboard Charts and Monitors are never refused, and what 0 does.
  - It's in `get_instance_settings` and `update_instance_settings`.
- [x] **EventStore reads it from Console's SQLite** on each read that checks the budget: the Logs explorer's and MCP's runs, estimates and histograms (`check_budget`).
  - With no `instance_settings` row, the default is 4096 MiB.
  - The heavy-read threshold, a quarter of the budget (04), follows it.
  - Charts and Monitors still only log their estimate.
- [x] **`logs_text_match_budget_mb` leaves EventStore's config** and `deploy/event_store.yml`. An `event_store.yml` that still sets it starts as before. EventStore logs a warning at start, saying the budget is set on Settings → Instance now, and ignores the value.
- [x] **`logs_heavy_read_slots` stays in `event_store.yml`.** EventStore sizes the slots at start from the CPU count. Settings → Instance lists it, with its value, among EventStore's restart-to-change settings (`event_store/handler/config.go`).
- [x] **Docs:**
  - "Reading less" in `docs/logs/query-language.md` and its copy `console/app/mcp/docs/query-language.md`;
  - the settings lists in `docs/install.md` and `docs/mcp.md`.
- [x] **Tests:**
  - EventStore picks up a changed budget without a restart: a Query refused under one budget runs after the budget is raised;
  - the default with no row;
  - the model's validation and `update_instance_settings`;
  - the field in `console/e2e/flows/instance-settings.spec.ts`.

**Upgrading:** after the install, set the budget on Settings → Instance. The 10 GiB in the VM's `event_store.yml` stops applying.

## Comments

**2026-10-30, built on `query-timeouts/07-budget-setting` (`8c3229dc`), from `main` at `987970ae`.**
- **Console:**
  - Migration `20261030120000_add_logs_text_match_budget_mb_to_instance_settings` adds the column (integer, default 4096, not null). `schema.rb` gains only that column, placed alphabetically.
  - It's in `InstanceSetting::EDITABLE`, validated as a whole number from 0. A locale entry names it "Text-match budget (MiB)", so `update_instance_settings` refuses -1 with "Not saved: Text-match budget (MiB) must be greater than or equal to 0."
  - Settings → Instance has the field under the default trace Retention. Its text says what it limits, that Dashboard Charts and Monitors are never refused, that a Query over a quarter of it is a heavy read, and that 0 refuses none.
  - It's in `get_instance_settings`' editable section and is an integer argument of `update_instance_settings`.
- **EventStore:**
  - `ConsoleSettings.TextMatchBudgetBytes()` reads the column apart from `Instance()`, so a Console that hasn't migrated yet fails only the budget, not SQL queries. No row gives 4096 MiB (`storage.DefaultTextMatchBudgetBytes`).
  - `sizeUp` reads it once per sized run. A failed read logs "Reading the text-match budget failed, so the default applies" and uses 4096 MiB; the Query goes on. The heavy threshold, a quarter of it, follows.
  - It's read for every sized run, checked or not, so Charts' and Monitors' logged estimates carry the current budget. They're still never refused.
  - `logs_text_match_budget_mb` left `Config`, `LogsHandler` and `main.go`. A file that still sets it, whatever its value, loads with the warning "logs_text_match_budget_mb is ignored: the budget of a Query that matches text is set on Console's Settings → Instance now, and applies with no restart. Remove it from the config".
  - `GET /config` answers `logs_heavy_read_slots`, and lists it among the defaults when it is one. Settings → Instance shows it as "Heavy Log reads at once", or "No cap" for 0.
- **Docs:** "Reading less" (both copies, still identical), `docs/install.md` ("Users and instance admins" lists the page's settings), `docs/mcp.md`, and `deploy/event_store.yml`, whose `logs_heavy_read_slots` comment now points to the page.
- **Tests:**
  - New root tests: `TestTheBudgetSetInConsoleAppliesFromTheNextQueryWithoutARestart` and `TestABudgetThatCantBeReadIsTheDefault`. The first checks four things: with no row, a week's search is refused at 4 GiB; at 256 GiB it runs and is heavy; at 16 GiB two hours aren't heavy; at 0 it runs. The second drops the column and gets the default.
  - New `storage` tests: the default with no row and with a row, changes seen without reopening, and a missing column failing only the budget.
  - Changed: the `config` test, and the `handler` `/config` test, which now has 3 slots.
  - Rails: the model's validation, `update_instance_settings` (10240, 0, -1 refused), the controller's save and form error, and the EventStore row.
  - The `instance-settings` flow: the field shows 4096, -1 gives the form error, and 10240 is saved and still there after a reload. It also checks the `logs_heavy_read_slots` row.
- **Results:**
  - `go test -p 1 -count=1 ./handler ./config ./storage`: pass. `go vet` is clean.
  - Root `-run 'Budget|Refused|Estimate|Force|Heavy|Checked|SQL|Histogram|Run|Query|Timeout|Cursor|Page'`: 63 pass, 3 fail. The 3 are old-files tests in `logs_old_files_test.go` that MinIO refused (`XMinioStorageFull`): the box's disk is at 100% (765 MB free after clearing this worktree's dev data). They don't touch this change.
  - Rails `test/models test/mcp test/controllers/instance_settings_controller_test.rb test/services/event_store_test.rb`: 355 runs pass. `bin/rubocop` on the changed files and `bun run typecheck` are clean.
  - Playwright `instance-settings.spec.ts`: 2 pass, against a dev stack started from this worktree with EventStore on its own ClickHouse database (`solidtrace_qt07`, dropped afterwards).
  - A live check on that stack: `estimate` answered `budget_bytes` 4294967296, then 1048576 (refused) right after `logs_text_match_budget_mb` was set to 1 with no restart. Then it was set back.
- **Upgrading:**
  - The release needs `db:migrate` (Console's start runs it).
  - On the VM, set 10240 on Settings → Instance after the install. Then remove `logs_text_match_budget_mb` from `deploy/config/event_store.yml`, or EventStore warns at each start.
- **Not done:** the `instance_settings:set` rake task doesn't take the budget (it doesn't take the trace Retention either). The spec's ticket list and `.scratch/STATUS.md` still say 07 is ready-for-agent.
