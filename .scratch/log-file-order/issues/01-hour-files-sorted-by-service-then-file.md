# 01: Hour files sorted by service, then file, then time

**What to build:** the hourly export writes every Project's hour files sorted by the default Log file order, `service` then the `file` Attribute, then `time, id`, so a Query filtered on `service` or `file` reads only the row groups that hold those Logs. EventStore only; the order is built in, and no setting changes it yet (02 adds that). See the [spec](../spec.md).

- Each Field of the order is sorted as the hour's day holds it: `service` by its column; an Attribute by its promoted column if the day's set has it, by `coalesce(column, map value)` if it was promoted partway, else by its string map value (`attrs_string`). NULLs sort last, then `time, id`.
- The COPY's row group size (30,720) and compression don't change, and an hour's late files are sorted the same way.
- The "Exported a Project's Logs" log line gains the order it used.
- Nothing on the read side changes. The comments that say Log files are sorted by time (`ExportLogs`'s and any others) are corrected.
- CONTEXT.md gains **Log file order**, and the Log file store's description there and the export's in `docs/logs/` say Log files are sorted by it, then time.

**Blocked by:** None (can start immediately)

**Status:** resolved

- [x] An hour of Logs from two Services and three log files is written grouped by `service`, then `file`, then time; reading the file in its own row order shows that order
- [x] Logs without a `service` or `file` sort after those that have one
- [x] `file` promoted for the day, promoted partway and only in the map each sort the same
- [x] An hour's late file is sorted the same way
- [x] The same Logs list the same before and after the export, newest first, every field and Attribute alike
- [x] The export's log line names the order
- [x] CONTEXT.md and `docs/logs/` updated

Tests go in EventStore's root tests, beside `TestLogsListTheSameBeforeAndAfterTheHourlyExport` and `TestLateLogsOfAnExportedHourGetLateFilesAndListTheSame`. They check what can be seen (the file's row order, the Logs a Query lists, the log line), never the SQL the export runs.

## Comments

**2026-10-22, built on `feature/log-file-order` (merge `cbfbf66`).** `storage.LogFileOrder` with `DefaultLogFileOrder` (`service, file`); `orderBy` reads each Field through `LogColumns`, as reads do (column, `coalesce` when promoted partway, map value), `NULLS LAST`, then `time, id`. `daySet` returns a `PromotedSet`. The log line gains `order="service, file, time"`. Root tests `TestAnHoursFileIsSortedByServiceThenFileThenTime` and `TestAFilePromotedForTheDayPartwayOrOnlyInTheMapSortsTheSame` (in `logs_file_order_test.go`), with the export, manifest, bucket, bin cache and Retention root tests and `./storage/`.
