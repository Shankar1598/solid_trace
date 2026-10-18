# M1-02: Console instance settings and Project Retention

**What to build:** Console gains the settings Logs need, and EventStore can read them. An operator sets instance settings (SQL queries enabled, default Log Retention) with a Rails task or console command; an Organization admin sets a Project's own Retention in Project settings. Spec: [Console](../spec.md#console) (Settings storage, Who edits what), [Further Notes](../spec.md#further-notes) (instance settings edited by the operator).

**Blocked by:** None (can start immediately).

**Status:** resolved

- [x] One instance settings record holds **SQL queries enabled** (default off) and **default Log Retention** in days (default 30). Its SQL setting's description lists what turning it on allows, as [ADR 0003](../../../docs/adr/0003-unrestricted-sql-queries-behind-an-instance-setting.md) requires.
- [x] The operator changes both with a Rails task or console command; there is no UI for them.
- [x] Projects gain a nullable **Log Retention** in whole days; Organization admins edit it in Project settings, members can't.
- [x] EventStore reads the instance settings and a Project's effective Retention (its own, else the instance default) read-only from Console's SQLite, as it reads `project_keys`. No Go writes to Rails tables ([ADR 0001](../../../docs/adr/0001-eventstore-writes-console-tables.md)).
- [x] Console request tests (Minitest): admins change a Project's Retention, members can't; invalid values (zero, negative, non-integer) are rejected.
- [x] A Go test reads the settings and a Project's effective Retention from a Console-shaped SQLite database.

## Comments

### 2026-10-02 built (logs-v1/m1-02)

What was built:
- **Instance settings:** model `InstanceSetting`, table `instance_settings`, with `sql_queries_enabled` (default false) and `default_log_retention_days` (default 30). There is one row, id 1. `InstanceSetting.current` returns that row. If the row doesn't exist yet it returns an unsaved record with the column defaults, so reading never writes. The row is created the first time the operator saves. `InstanceSetting::SQL_QUERIES_DESCRIPTION` lists what turning SQL on allows (ADR 0003).
- **Operator commands:** `bin/rails instance_settings:show` and `bin/rails instance_settings:set SQL_QUERIES_ENABLED=true|false DEFAULT_LOG_RETENTION_DAYS=<days>` in `console/lib/tasks/instance_settings.rake`. Both print the settings and the SQL description. `set` accepts true/false/on/off only; it refuses anything else rather than casting a typo to true. Invalid days abort with exit 1. From a Rails console, `InstanceSetting.current.update!(...)` also works. No UI.
- **Project Retention:** `projects.log_retention_days` is a nullable integer; NULL means the instance default. The Project model requires a positive whole number or nil, and an empty form value clears it. Admins edit it in a new "Log Retention" card on `Projects/Show`. Members see it disabled.
- **Admin check:** a new `ProjectPolicy#update?` admits only the Organization's admins. `ProjectsController#update` calls `authorize`, so a member gets the usual Pundit 302 back with an alert. The show page gets `default_log_retention_days` and `can_manage_project`.
- **EventStore reader:** `storage.ConsoleSettings` in `event_store/storage/console_settings.go`.
  - `NewConsoleSettings(sqlitePath)` opens the database.
  - `Instance() (InstanceSettings, error)` returns the instance settings.
  - `LogRetentionDays(projectID uint32) (int, error)` returns the effective Retention.
  - `Close()` closes it.

Choices the ticket left open:
- **Whole update is admin-only.** All of Project update is now for admins, not only the Retention field. Renaming has no UI today. The existing "update project" test now runs as an admin.
- **Truly read-only reader.** `ConsoleSettings` opens Console's SQLite as `file:<path>?mode=ro`. go-sqlite3 honours `mode=ro` only for a `file:` URI. A one-off probe confirmed writes fail with "attempt to write a readonly database". `ProjectAuth` is unchanged; it still opens the database read-write in practice, a deferred item.
- **Defaults when the row is missing.** If the `instance_settings` row is missing (a fresh install, or `db:reset`, which loads the schema), Go uses the same defaults: SQL off, 30 days. These are copied from the Rails column defaults; change both together.
- **No Project row.** A Project with no `projects` row gets the instance default. Project 123 in the Go test env is like this.
- **No cache.** Every call reads SQLite, so a change in Console shows at once. No existing pattern for a cache exists (`ValidateKey` has none).
- **No upper bound and no DB CHECK constraints.** Rails validations only.

Follow-ups for later tickets:
- **M1-09 / M1-03:** `main.go` doesn't wire `ConsoleSettings` yet. Construct it from `cfg.SQLitePath` where it's used. If you convert days to a `time.Duration`, guard against overflow on absurd values: there is no upper bound. Also treat a non-positive value as invalid: only Rails validations prevent one.
- **M3-03 / M3-04:** read `Instance().SQLQueriesEnabled` in Go and `InstanceSetting.current.sql_queries_enabled?` in Rails. A future settings UI can reuse `SQL_QUERIES_DESCRIPTION`.
