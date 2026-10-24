# 01: Settings → Instance page with the settings that apply immediately

Spec: [../spec.md](../spec.md)

**What to build:** An Instance admin opens Settings → Instance from the sidebar's Install group, next to Email. They can change SQL queries (with what turning them on allows shown beside the switch), the default Retention in whole days, and log-all-reads, and the change applies at once with no restart. Sign-up is shown read-only: open or closed, that the first user can always sign up, and a security note. Opening it lets anyone who can reach Console make an account, so it changes only from a shell on the server with `bin/rails instance_settings:set SIGN_UP_OPEN=true|false`, and a stolen session or token can't open the install. Console's environment is shown read-only under "Restart to change". Each row has its env var, effective value, and whether it's the default; `SECRET_KEY_BASE` and `SENTRY_DSN` show only as set or not set. Members who aren't Instance admins don't see the item and are refused the page. One answer builder builds the page's sections, so 03's MCP tools reuse it. It leaves room for 02's EventStore group.

**Blocked by:** None (can start immediately)

**Status:** resolved (merged into `feature/instance-settings` at 9291930; 16 Rails runs and 2 Playwright flows pass)

- [x] A member who isn't an Instance admin doesn't see the Instance item and is refused the page and its update
- [x] An Instance admin sees the editable settings, Sign-up and Console's environment
- [x] Saving changes SQL queries, the default Retention and log-all-reads; a reload shows them
- [x] A Retention below one day, or not a whole number, shows a form error and saves nothing
- [x] An update carrying `sign_up_open` (or any other setting) changes nothing else
- [x] No secret's value appears in the page's props
- [x] `InstanceSetting`'s and the rake task's comments say which settings have a UI and why Sign-up doesn't
- [x] Rails controller tests (like the Email settings ones) and a Playwright flow (like email-settings: change and save, reload, Sign-up read-only) pass
