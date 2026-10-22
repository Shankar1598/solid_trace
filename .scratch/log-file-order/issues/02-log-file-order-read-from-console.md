# 02: A Project's Log file order, read from Console

**What to build:** each Project has its own Log file order, which the hourly export reads from Console, so a Project can group its files by other Fields or by time only. Changing the order applies from the next export. See the [spec](../spec.md).

- Console gains a nullable Project column holding a JSON array of Field names. NULL means the default, `["service", "file"]`; `[]` means time only. Console owns the column and its default (ADR 0001).
- EventStore's Console settings reader gains a read of a Project's Log file order, beside Retention and the Log transform. The hourly export asks for it once per Project per export, through a small interface the store is given (as Log cleanup takes `ProjectLogRetention` and the Log transform takes `LogTransform`); `*storage.ConsoleSettings` is the production adapter.
- If EventStore can't read a value, such as one written by hand that isn't a JSON array of names, it logs a warning and uses the default. The export never fails over the setting.
- The names are read as 01 reads them: `service`, `environment` and `level` by their columns, any other name as a string Attribute.

**Blocked by:** 01

**Status:** resolved

- [x] With no setting, the export writes 01's default order
- [x] A setting of `["level"]`, of `[]` and of an Attribute the Logs don't have each writes the order it names
- [x] A changed setting applies to the next export; the earlier hour's file keeps its order, and an hour's late file uses the order in force when it's written
- [x] A setting EventStore can't read exports in the default order and logs a warning
- [x] Queries list the same Logs before and after the export whatever the order

Tests go in EventStore's root tests, writing the Project's setting to Console's test database as the Retention tests do. They need the Console migration run on the test database first.

## Comments

**2026-10-22, built on `feature/log-file-order` (merge `2b0d9c2`).** Migration `AddLogFileOrderToProjects` (nullable `json`). `ConsoleSettings.LogFileOrder`, given to the store with `LogStore.UseLogFileOrders`; the export reads it once per Project per export, and an unreadable value logs "Failed to read a Project's Log file order; exporting its Logs in the default order". After the review (`3199eba`), a value Console would refuse (over 3 names, a repeat, `time`, `_time`, `id`, `message`, `trace_id`, `span_id`) is unreadable too, and JSON `null` reads as the default without a warning. Root tests `TestAnHoursFileIsSortedByItsProjectsLogFileOrder`, `TestAChangedLogFileOrderAppliesFromTheNextExport`, `TestALogFileOrderEventStoreCantReadExportsInTheDefaultOrder`.
