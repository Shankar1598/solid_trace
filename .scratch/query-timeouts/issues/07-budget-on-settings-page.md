# 07: The text-match budget on Settings → Instance

**What to build:** the budget for Queries that match text (02) lives in EventStore's `event_store.yml`. Changing it means editing the VM's config and restarting EventStore, and Settings → Instance doesn't show it at all. Make it an instance setting, like SQL queries and the default Retention:
- Instance admins edit it on Settings → Instance and over MCP (`update_instance_settings`).
- It applies from the next Query, with no restart.

EventStore already reads Console's `instance_settings` row from Console's SQLite on every call (`event_store/storage/console_settings.go`), so this uses the same path. See the [spec](../spec.md).

Asked for by the user on 2026-10-30. The 4 GiB default covers only the last 24 minutes of `tagalys-verbose` (about 233 GiB of text a day, all in one file). The VM runs 10 GiB from its `event_store.yml` until this ships.

**Blocked by:** None (can start immediately)

**Status:** ready-for-agent

- [ ] **Console.** Add `instance_settings.logs_text_match_budget_mb`: an integer, default 4096, not null, 0 or more, where 0 refuses no Query.
  - It goes in `InstanceSetting::EDITABLE`, with its validation.
  - It shows on Settings → Instance, with text saying what it limits, that Dashboard Charts and Monitors are never refused, and what 0 does.
  - It's in `get_instance_settings` and `update_instance_settings`.
- [ ] **EventStore reads it from Console's SQLite** on each read that checks the budget: the Logs explorer's and MCP's runs, estimates and histograms (`check_budget`).
  - With no `instance_settings` row, the default is 4096 MiB.
  - The heavy-read threshold, a quarter of the budget (04), follows it.
  - Charts and Monitors still only log their estimate.
- [ ] **`logs_text_match_budget_mb` leaves EventStore's config** and `deploy/event_store.yml`. An `event_store.yml` that still sets it starts as before. EventStore logs a warning at start, saying the budget is set on Settings → Instance now, and ignores the value.
- [ ] **`logs_heavy_read_slots` stays in `event_store.yml`.** EventStore sizes the slots at start from the CPU count. Settings → Instance lists it, with its value, among EventStore's restart-to-change settings (`event_store/handler/config.go`).
- [ ] **Docs:**
  - "Reading less" in `docs/logs/query-language.md` and its copy `console/app/mcp/docs/query-language.md`;
  - the settings lists in `docs/install.md` and `docs/mcp.md`.
- [ ] **Tests:**
  - EventStore picks up a changed budget without a restart: a Query refused under one budget runs after the budget is raised;
  - the default with no row;
  - the model's validation and `update_instance_settings`;
  - the field in `console/e2e/flows/instance-settings.spec.ts`.

**Upgrading:** after the install, set the budget on Settings → Instance. The 10 GiB in the VM's `event_store.yml` stops applying.
