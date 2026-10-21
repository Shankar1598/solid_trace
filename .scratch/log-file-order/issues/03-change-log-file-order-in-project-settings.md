# 03: Admins change the Log file order on the Project settings page

**What to build:** an admin sees and changes a Project's Log file order on its settings page, beside Log Retention, and other members see it read-only. An MCP client sees it in `list_projects`. See the [spec](../spec.md).

- The Project model validates the order: at most three names, no name twice, each `service`, `environment`, `level` or an Attribute name the Query language accepts. `time`, `id`, `message`, `trace_id` and `span_id` are refused. NULL (the default) is valid.
- The Projects controller permits the order beside `log_retention_days`. The page gets the stored value and the default, so a NULL order shows as "Default (service, file)".
- The settings page's "Log file order" section is editable by whoever may edit Retention and read-only for other members. It shows the order as removable chips, offers `service`, `environment`, `level` and the Project's string Attributes (from the Fields Console already lists for the Builder), takes a typed Attribute name, and has "Reset to default" and "Time only". Validation errors show beside it.
- Its help text says the order applies to hours exported after a change, helps most for Fields that have a column of their own, and that each Field added makes reads over a narrow time span read a little more.
- `list_projects` shows each Project's Log file order next to its Retention. No MCP tool changes it.

**Blocked by:** 02

**Status:** ready-for-agent

- [ ] Model: a fourth Field, a repeated Field and each refused built-in Field are refused with a message; NULL and `[]` are valid
- [ ] Controller: an admin's update saves the order; a member who can't edit Retention can't change it
- [ ] The page shows the default when no order is set, and "Reset to default" and "Time only" save NULL and `[]`
- [ ] A new Playwright flow adds, removes and resets Fields and sees a validation error (prior art: `project-keys.spec.ts`, `project-delete.spec.ts`)
- [ ] `list_projects` shows the order
