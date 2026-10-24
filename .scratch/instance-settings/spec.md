# Instance settings in Console

Status: resolved

Planned with the user on 2026-10-23. Tickets: [01](issues/01-instance-settings-page.md), [02](issues/02-eventstore-config-route.md), [03](issues/03-instance-settings-over-mcp.md).

## Problem Statement

An **Instance admin** can't see how their install is set up. SQL queries, **Sign-up**, the default **Retention** and log-all-reads are only shown and changed with `bin/rails instance_settings:set` on the server. Everything Console and EventStore read when they start (the environment, `event_store.yml`) can only be seen by opening those files on the VM. Only email has a page. So answering "are SQL queries on?", "where do older Logs go?" or "what's the disk limit?" means a shell on the VM, and so does changing a setting that takes effect immediately anyway.

## Solution

A Settings → Instance page, for Instance admins only, next to Email in the Install group:

- The settings that take effect immediately are editable: SQL queries, the default Retention, log-all-reads.
- **Sign-up** is shown but read-only. A security note explains that opening it lets anyone who can reach Console make an account, so changing it takes a shell on the server (the rake command is shown). Then a stolen Instance admin session or API token can't open the install.
- Everything that needs a restart is shown read-only under "Restart to change", grouped by Console and EventStore. Each row says where its value comes from (an env var or an `event_store.yml` key) and whether it's the default. Secrets show only as set or not set.

The same appears over MCP: one tool reads it all and one changes the editable settings.

## User Stories

1. As an Instance admin, I want an Instance item under Settings → Install, so that I can find the install's settings next to Email.
2. As a member who isn't an Instance admin, I want the Instance item hidden and its page refused, so that install-wide settings stay with whoever runs the install.
3. As an Instance admin, I want to see whether SQL queries are on, so that I know what members can run.
4. As an Instance admin, I want to turn SQL queries on or off from the page, so that I don't need a shell to do it.
5. As an Instance admin, I want to read what turning SQL queries on allows next to the switch, so that I make the choice knowing the risk.
6. As an Instance admin, I want to see and change the default Retention in whole days, so that Projects without their own Retention keep Logs as long as I want.
7. As an Instance admin, I want the page to say the default applies only to Projects that set none, and that EventStore's next Retention run deletes older Logs, so that I'm not surprised when Logs disappear.
8. As an Instance admin, I want a Retention below one day or not a whole number refused, with the error on the form, so that a typo can't delete every Log.
9. As an Instance admin, I want to turn log-all-reads on or off, so that I can time every Log read while investigating slowness and stop afterwards.
10. As an Instance admin, I want a saved change to apply at once with no restart, so that I can check the result immediately.
11. As an Instance admin, I want to see whether Sign-up is open, and that the first user can always sign up, so that I know how people can join.
12. As an Instance admin, I want Sign-up read-only on the page with a note on why and the command that changes it, so that I know where to change it.
13. As an operator, I want Sign-up changeable only from a shell on the server, so that a stolen Instance admin session or API token can't open the install to anyone.
14. As an Instance admin, I want Console's URL, public ingest URL and EventStore URLs, so that I can check links and wiring.
15. As an Instance admin, I want the running SolidTrace version, so that I know which release is deployed.
16. As an Instance admin, I want Console's thread, job concurrency and log level settings, so that I can understand its capacity and logging.
17. As an Instance admin, I want `SECRET_KEY_BASE` and the Sentry DSN shown only as set or not set, so that the page leaks no secret.
18. As an Instance admin, I want each row to name its env var or `event_store.yml` key and say when it's the default, so that I know what to edit and where.
19. As an Instance admin, I want EventStore's ingest and query ports and ClickHouse address, database and user, so that I can check how it's wired.
20. As an Instance admin, I want EventStore's storage paths (Pebble, message queue, Console's SQLite, the **Log file store**), so that I know where the data is on disk.
21. As an Instance admin, I want the disk limit, both what's configured and the bytes in effect when it's left at half the disk, so that I know when the oldest days will move or be deleted.
22. As an Instance admin, I want the **Bin cache** size and the most ingest requests that may wait, so that I understand EventStore's memory and back-pressure.
23. As an Instance admin, I want the bucket's kind, name, prefix, region, endpoint and local cache hours, or "none", so that I know where older Logs go.
24. As an Instance admin, I want the ClickHouse password and the bucket's keys only as set or not set, so that they never leave the server.
25. As an Instance admin, I want EventStore's values to be the ones it loaded at start, not the file on disk now, so that the page shows what is running.
26. As an Instance admin, I want the CPU profile directory and debug flag, so that I know whether profiling is on.
27. As an Instance admin, I want the page to still work and say so when EventStore can't be reached, so that one outage doesn't hide the editable settings.
28. As an Instance admin using an MCP client, I want `get_instance_settings` to answer the same sections as the page, with a link to it, so that an agent can check the install.
29. As an Instance admin using an MCP client, I want `update_instance_settings` to change SQL queries, the default Retention and log-all-reads, so that an agent can change them for me.
30. As an operator, I want the MCP tools unable to change Sign-up or anything that needs a restart, so that MCP follows the page's rules.
31. As a member who isn't an Instance admin, I want both MCP tools to refuse me, so that the rule matches the page.
32. As an operator, I want `instance_settings:show` and `:set` to keep working, so that scripts and Sign-up changes still have a way in.

## Implementation Decisions

- **Settings page.** A new controller and Inertia page under the Organization's settings, guarded by Instance admin exactly as the Email settings controller is. The sidebar's Install group gains an Instance item. The update accepts only `sql_queries_enabled`, `default_log_retention_days` and `log_all_reads`; anything else, `sign_up_open` included, is ignored. Errors come back as Inertia form errors, and success flashes as Email does.
- **One answer builder** (a deep module, e.g. `InstanceSettingsAnswer`) builds the three sections for both the page and MCP: editable settings, Sign-up, and the restart-to-change groups. Its interface is "the settings as shown"; the env reading, EventStore call and redaction are inside it. Both the page and MCP use it, so they can't drift.
- **Console's environment** is read by a small object listing each setting: its name, source (env var), effective value, whether it's the default, and whether it's secret. It reuses the defaults the code already has (the EventStore service's URLs, the thread count) instead of copying literals. Secrets: `SECRET_KEY_BASE`, `SENTRY_DSN`.
- **EventStore's config** comes from a new loopback-only query API route, `GET /config`. It answers the configuration EventStore loaded at start, as JSON, with the ClickHouse password and bucket key id and secret as booleans (set or not). The disk limit answers both the configured bytes (0 meaning the default) and the effective bytes worked out at start. The DuckDB-era warning keys and test-only fields (the storage policy) are left out. Console's EventStore service gains a `config` call with a short timeout. When it fails, the builder answers the EventStore group as unavailable with the reason.
- **MCP.** `get_instance_settings` and `update_instance_settings`, with no Organization argument, Instance admin only, sharing the guard and link pattern with the email settings tools. Update takes the three editable settings, each optional, and turns validation errors into tool errors. Both are registered on the server, documented in the MCP docs, and the server instructions' Instance admin line names them.
- **Sign-up stays rake-only.** The rake task is unchanged. The comments in `InstanceSetting` and the rake task say which settings now have a UI, and why Sign-up doesn't.
- **SQL queries' default stays on.** With the switch on this page, an Instance admin decides per install; STATUS's open question is closed this way (see Further Notes).
- No schema change. No migration.

## Testing Decisions

- Good tests here check what an Instance admin or MCP client sees and can change: page props, redirects and errors, the stored settings afterwards, tool answers. They don't check how the builder reads the environment.
- **Rails controller tests** for the page (prior art: the Email settings controller test): a non-admin is refused; an admin sees all three sections; the three settings update; a bad Retention gives a form error; `sign_up_open` in params changes nothing; no secret value appears in the props; EventStore stubbed, and unreachable.
- **MCP tool tests** (prior art: the email settings tools test): a non-admin is refused; get answers the sections with no secrets; update changes the three; a `sign_up_open` argument changes nothing.
- **A Go route test** for `GET /config` on the query app (prior art: the root package tests that build the query app): values come through and secrets never do.
- **A Playwright flow** (prior art: the email-settings flow): an Instance admin opens Settings → Instance, changes log-all-reads and Retention, saves, reloads and sees them; Sign-up is read-only; the EventStore group shows on the dev stack. Functional checks only, no screenshots.
- Targeted runs only; the full suites are for the user.

## Out of Scope

- Changing anything in `deploy/.env` or `event_store.yml` from Console.
- Granting or removing Instance admin from the UI (`instance_admins:grant` stays).
- Showing the Meta Organization's bookkeeping (`meta_organization_id`, `meta_organization_seeded`).
- Email settings, which keep their own page and tools.
- ClickHouse's own server settings (`deploy/clickhouse`) and Caddy's.

## Further Notes

- The user's answers (2026-10-23): Sign-up read-only, with the security reason on the page. EventStore's config is included. On the SQL queries default, they answered "yes add", taken here as: add the switch to this page and leave the default on. STATUS's open question is removed on that reading; reopen it if they meant something else.
- On the ClickHouse release the disk limit's "half the disk" is worked out from ClickHouse's local disk at EventStore's start, so `/config` answers that value, not a fresh one.
- The Query API is bound to 127.0.0.1 only, so `/config` adds no exposure. It answers no secret anyway.
