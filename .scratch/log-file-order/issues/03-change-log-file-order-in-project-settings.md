# 03: Admins change the Log file order on the Project settings page

**What to build:** an admin sees and changes a Project's Log file order on its settings page, beside Log Retention, and other members see it read-only. An MCP client sees it in `list_projects`. See the [spec](../spec.md).

- The Project model validates the order: at most three names, no name twice, each `service`, `environment`, `level` or an Attribute name the Query language accepts. `time`, `id`, `message`, `trace_id` and `span_id` are refused. NULL (the default) is valid.
- The Projects controller permits the order beside `log_retention_days`. The page gets the stored value and the default, so a NULL order shows as "Default (service, file)".
- The settings page's "Log file order" section is editable by whoever may edit Retention and read-only for other members. It shows the order as removable chips, offers `service`, `environment`, `level` and the Project's string Attributes (from the Fields Console already lists for the Builder), takes a typed Attribute name, and has "Reset to default" and "Time only". Validation errors show beside it.
- Its help text says the order applies to hours exported after a change, helps most for Fields that have a column of their own, and that each Field added makes reads over a narrow time span read a little more.
- `list_projects` shows each Project's Log file order next to its Retention. No MCP tool changes it.

**Blocked by:** 02

**Status:** resolved

- [x] Model: a fourth Field, a repeated Field and each refused built-in Field are refused with a message; NULL and `[]` are valid
- [x] Controller: an admin's update saves the order; a member who can't edit Retention can't change it
- [x] The page shows the default when no order is set, and "Reset to default" and "Time only" save NULL and `[]`
- [x] A new Playwright flow adds, removes and resets Fields and sees a validation error (prior art: `project-keys.spec.ts`, `project-delete.spec.ts`)
- [x] `list_projects` shows the order

## Comments

**2026-10-22, built on `feature/log-file-order` (merges `6705e2f`, review fixes `3199eba`).** `Project` validates the order (Attribute names: not empty, at most 256 bytes, UTF-8, no NUL, as ingest and promotion take them, since the Query language quotes any name; `_time` refused as the alias of `time`) and has `log_file_order_in_force`. The page's `LogFileOrderCard` edits a draft saved with "Save"; "Reset to default" and "Time only" save at once; a fourth Field or a repeat isn't added and the card says why. The Attributes offered are the Project's string Fields of the last 24 h (as `list_fields`), a deferred prop sent only to those who may edit. The built-in Fields come from the controller. `list_projects` shows the order in force. Tests: model, controller, `test/mcp`, vitest, and the flow `e2e/flows/project-log-file-order.spec.ts` (3 tests, with `project-keys` and `project-delete`: 7 passed).
