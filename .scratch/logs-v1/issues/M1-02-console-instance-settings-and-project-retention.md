# M1-02: Console instance settings and Project Retention

**What to build:** Console gains the settings Logs need, and EventStore can read them. An operator sets instance settings (SQL queries enabled, default Log Retention) with a Rails task or console command; an Organization admin sets a Project's own Retention in Project settings. Spec: [Console](../spec.md#console) (Settings storage, Who edits what), [Further Notes](../spec.md#further-notes) (instance settings edited by the operator).

**Blocked by:** None (can start immediately).

**Status:** ready-for-agent

- [ ] One instance settings record holds **SQL queries enabled** (default off) and **default Log Retention** in days (default 30). Its SQL setting's description lists what turning it on allows, as [ADR 0003](../../../docs/adr/0003-unrestricted-sql-queries-behind-an-instance-setting.md) requires.
- [ ] The operator changes both with a Rails task or console command; there is no UI for them.
- [ ] Projects gain a nullable **Log Retention** in whole days; Organization admins edit it in Project settings, members can't.
- [ ] EventStore reads the instance settings and a Project's effective Retention (its own, else the instance default) read-only from Console's SQLite, as it reads `project_keys`. No Go writes to Rails tables ([ADR 0001](../../../docs/adr/0001-eventstore-writes-console-tables.md)).
- [ ] Console request tests (Minitest): admins change a Project's Retention, members can't; invalid values (zero, negative, non-integer) are rejected.
- [ ] A Go test reads the settings and a Project's effective Retention from a Console-shaped SQLite database.
