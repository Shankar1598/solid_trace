# Log files sorted by the Fields Queries filter on

**Status:** resolved

Planned with the user on 2026-10-22, after Dashboard 1 ("Sidekiq — jobs & queues") took 15.5 s a Chart on its first 6 h load on `tagalys`. Each Chart reads only `sidekiq_metrics.log`, about 0.1% of the Project's 13 M Logs an hour, but every hour's Log file holds its Logs in time order, so the Logs of one log file are spread through every row group and DuckDB reads them all. The user asked how Axiom and Better Stack (ClickHouse) avoid this; ClickHouse keeps each table sorted by a key of its own. The user chose `service, file, time` as the default order, changeable in the Project's settings.

## Problem Statement

A member who builds a Chart, a Monitor or a search on one source of a busy Project, such as one Service or one log file, waits as if it read the whole Project. A Chart over 6 hours of `sidekiq_metrics.log` reads every row group of six hourly Log files, about 2,500 row groups, to find the few dozen that hold its Logs, and searching its message text reads every message of every source. The bin cache makes the second load fast, but the first load of a Dashboard, a range that moves past what's cached and every Monitor run over a new range pay the whole cost. On a 2-vCPU production VM the reads also hold up ingest.

Filtering on an Attribute's own column, as Dashboard 1 now does (`file == "sidekiq_metrics.log"`), cut the cost of finding the Logs, but DuckDB still opens every row group of the hour to look, and a Query that then reads `message` decompresses the messages of every source.

## Solution

The hourly export writes each hour's Log file sorted by the Project's **Log file order**, a short list of Fields, then by time. By default it is `service`, then the `file` Attribute. Logs of one Service and one log file then sit together in a few row groups, and DuckDB skips the rest by each row group's minimum and maximum. A Query filtered on `service` or `file` reads only those row groups, whatever else it reads, `message` included.

An admin changes the order on the Project's settings page, choosing up to three Fields, or none for time only. A change applies to the hours exported after it; Log files already written keep their order. Queries answer the same whatever the order; only how much they read changes.

## User Stories

1. As a member viewing a Dashboard, I want a Chart filtered on one log file to load in about the time its own Logs take to read, so that a busy Project's other sources don't slow it.
2. As a member, I want the first load of a Dashboard over 6 hours to take a few seconds, not tens, so that I can use it without waiting for the bin cache.
3. As a member, I want a Chart that filters on `file` and reads `message`, such as Sidekiq's `INFO: done:` lines, to read only that file's messages, so that searching one source's text isn't searching all of them.
4. As a member, I want a Query filtered on `service` to read only that Service's row groups, so that a Project shared by many Services stays fast for each.
5. As a member, I want a Query to answer the same Logs, in the same order, whatever the Project's Log file order, so that I never have to think about it to trust an answer.
6. As a member paging back through the Logs explorer into hours already in Log files, I want the newest page to stay fast, so that the new order doesn't trade one slow read for another.
7. As a member using a Monitor on one source, I want its runs to read only that source's row groups, so that Monitors cost less of the VM.
8. As an admin, I want each Project to have a Log file order of `service`, then `file`, without setting anything, so that a Project shipped by Vector or an SDK gains from it at once.
9. As an admin, I want to change a Project's Log file order on its settings page, so that I can match it to the Fields my team filters on.
10. As an admin, I want to choose up to three Fields for the order, so that I can group by, say, `service`, then `file`, then `level`.
11. As an admin, I want to be told that the order is limited to three Fields when I try a fourth, so that I don't wonder why it wasn't saved.
12. As an admin, I want to choose from the built-in Fields `service`, `environment` and `level` and the Project's Attributes, so that I can pick a source Field my Logs actually have.
13. As an admin, I want to type an Attribute's name that the list doesn't show yet, so that I can set the order before those Logs arrive.
14. As an admin, I want to be told when I name the same Field twice, so that the order says what I meant.
15. As an admin, I want to clear the order to sort by time only, so that I can go back to how Log files were written before.
16. As an admin, I want to reset the order to the default, so that I don't have to remember it.
17. As an admin, I want the settings page to say that a change applies to hours exported after it, so that I don't expect older hours to speed up.
18. As an admin, I want the settings page to say that the order helps most for Fields that have a column of their own, which the busiest Attributes of a Project get, so that I choose Fields that pay off.
19. As an admin, I want the settings page to say that each Field added to the order makes reads over a narrow time span read a little more, so that I keep the order short.
20. As a member who isn't an admin, I want to see the Project's Log file order on its settings page, so that I know why some Queries are faster than others.
21. As a member who isn't an admin, I want the order's controls to be read-only for me, as Retention's are, so that I can't change how the Project's Logs are stored.
22. As an operator, I want the hourly export to cost about what it costs now, so that the VM's two CPUs aren't busier every hour.
23. As an operator, I want the export's log line to say which order it wrote each Project's file in, so that I can tell when a change took effect.
24. As an operator, I want a Project's Log files to be smaller when its Logs are grouped, so that disk and bucket use falls.
25. As an operator, I want a Log file order naming an Attribute the Project's Logs don't have to cost nothing, so that the default is safe for every Project.
26. As an operator, I want a Log file order that EventStore can't read, such as one written to the database by hand, to fall back to the default with a warning in the log, so that the export never stops over a setting.
27. As an operator, I want late Logs' files to be written in the same order as the hour's first file, so that every file of an hour skips alike.
28. As an operator, I want the order to change nothing in the hot table, so that ingest and Log processing are untouched.
29. As a developer, I want the order to be one setting that EventStore reads from Console's Project, as it reads Retention and the Log transform, so that there's one place it lives (ADR 0001).
30. As a developer, I want EventStore to sort by each Field as the hour's file holds it, built-in column, promoted column or map value, so that the order works for any Field.
31. As a developer, I want Logs without a Field of the order to sort after those that have it, so that missing values don't scatter.
32. As an agent using the MCP server, I want Queries to answer the same as before, so that nothing an agent learned about SolidTrace changes.

## Implementation Decisions

**The setting**

- A new nullable Project column in Console, the Log file order, holds a JSON array of Field names, such as `["service", "file"]`. NULL means the default, `["service", "file"]`; an empty array means time only. Console owns the column and its default (ADR 0001); EventStore reads it, read-only, through its Console settings reader, beside Retention and the Log transform.
- Console's Project model validates it: at most three names, no name twice, each the name of a built-in Field (`service`, `environment`, `level`) or an Attribute name the Query language accepts. Other built-in Fields, `time`, `id`, `message`, `trace_id` and `span_id`, are refused: `time` and `id` always end the order, and the rest are near-unique or long, which groups nothing.
- An Attribute in the order is read as a string value (`attrs_string`), the type a source Field like `file` has.

**Console**

- The Project's settings page gains a "Log file order" section beside Log Retention, editable by whoever may edit Retention and read-only for other members. It lists the order as removable chips, offers `service`, `environment`, `level` and the Project's string Attributes (from the Fields Console already lists for the Builder), takes a typed Attribute name, and has "Reset to default" and "Time only". Its help text says: the order applies to hours exported after a change; it helps most for Fields that have a column of their own; each Field added makes reads over a narrow time span read a little more.
- The Projects controller permits the new attribute alongside `log_retention_days`. The Project serializer sends the stored value and the default, so the page shows "Default (service, file)" when the column is NULL.
- The MCP server's `list_projects` adds each Project's Log file order next to its Retention. No new MCP tool changes it.

**EventStore**

- The hourly export asks, once per Project per export, for the Project's Log file order. The export's adapter for it reads Console's settings; an unreadable value logs a warning and uses the default.
- For each staged hour file, the export sorts by the order's Fields, each as the hour's day reads it (its built-in column; its promoted column if the day's set has it; `coalesce(column, map value)` if promoted partway; else its map value), with NULLs last, then `time, id`. The COPY, its row group size (30,720) and its compression don't change. The same order is used for an hour's late files.
- The export's "Exported a Project's Logs" log line gains the order it used.
- Nothing on the read side changes. Reads already never depend on a file's row order: the newest page's second step reads by id from the span its first step found, and DuckDB skips row groups by minimum and maximum whatever the order. The comment that says "Log files are sorted by time" is corrected.
- Docs: the Log file store's description in GLOSSARY.md and the export's description in `docs/logs/` say Log files are sorted by the Project's Log file order, then time. GLOSSARY.md gains **Log file order**.

## Testing Decisions

- A good test posts Logs, runs Log processing and the hourly export through the test environment, and checks what a member or operator can see: the Logs a Query lists, the order of the rows in the hour's Log file, and the export's log line. It never asserts the SQL the export runs.
- **One seam: EventStore's root tests**, as the export tests do (`TestLogsListTheSameBeforeAndAfterTheHourlyExport`, `TestLateLogsOfAnExportedHourGetLateFilesAndListTheSame`), with the Project's setting written to Console's test database as the Retention tests do. They cover:
  - With no setting, an hour of Logs from two Services and three log files is written grouped by `service`, then `file`, then time: reading the file in its own row order gives that order.
  - The same Logs list the same before and after the export, newest first, every field and Attribute alike, whatever the order.
  - A setting of `["level"]`, of `[]` and of an Attribute the Logs don't have each writes the order it names; Logs without a Field of the order come last.
  - A Field promoted for the day, one promoted partway and one only in the map each sort the same.
  - A changed setting applies to the next export, and the earlier hour's file keeps its order; an hour's late file uses the order in force when it's written.
  - A setting EventStore can't read exports in the default order and logs a warning.
- Console: the Project model's validations (three Fields, no repeat, refused built-in Fields, the NULL default), the controller's update with and without the right to edit, and a new Playwright flow for the settings page's Log file order section (the Project settings page has none yet; `project-keys.spec.ts` and `project-delete.spec.ts` are its nearest prior art).
- No benchmark ships with it. The probe below settled the decision on real data; the user's production check after a release confirms it.

## Out of Scope

- Rewriting Log files already exported. Hours already in the Log file store keep their order until Retention deletes them.
- Sorting the hot table. It fills in arrival order and holds two hours at most.
- A text index (per-row-group token filters for `search`). That helps rare terms, not the common phrases Charts filter on, and is a separate piece.
- Changing which Attributes are promoted, or promoting a Field because it's in the order.
- An MCP tool to change the order.
- Daily compaction or bigger row groups (ADR 0008).

## Further Notes

**The probe (2026-10-21, throwaway, code in `~/.cache/solid-trace/probe-sort/`).** On `tagalys`'s real 10:00 hour (12.8 M Logs), on the dev box with 2 threads, warm, best of 2, the file as exported against the same Logs sorted by `file, time`:

| ms | `time, id` (today) | `file, time` |
|---|---|---|
| `file == sidekiq.log and message contains 'INFO: done:'` | 1,384 | 210 |
| Chart 8's shape (`file` and `latency_seconds`, promoted) | 123 | 73 |
| `summarize count() by file` | 176 | 107 |
| `file == sidekiq_metrics.log`, count | 98 | 83 |
| `message contains 'timeout'`, no `file` filter | 4,309 | 4,131 |
| Newest 200 at 10:32, no filter | 584 | 726 |
| Newest 200 at 10:32, `file == sidekiq.log` | 540 | 477 |
| File size | 572 MB | 498 MB |

Writing the hour from a DuckDB table (2 threads, 4 GB, spilling about 5 GB): `time, id` 1 min 43 s and 2 min 03 s, `file, time` 1 min 40 s and 3 min 09 s, so between no extra cost and about 50% more, with disk noise. Today's export already sorts the whole hour, as DuckDB doesn't know the hot table is nearly in time order. Production's `tagalys` export copies took 1 min 58 s to 2 min 18 s for 13 to 15.5 M Logs (2026-10-21, 20:02 to 22:02).

A synthetic run (3.2 M Logs, the same mix of files) compared time slots before `file`: 1-minute slots were no better than today, and 5- and 15-minute slots fell between today and `file, time`. The user chose no slot: narrow time spans read little either way, and long ranges are what's slow. The newest page with no filter costs about 25% more, since the newest Logs of an hour come from the end of each file's run; DuckDB still skips row groups by time within each run.

**A key promoted partway through a day** is read as `coalesce(column, map value)`, which DuckDB can't skip by; on such a day the order still groups the Logs, but a filter on that key reads every row group. The default's `service` is a built-in column, and `file` had a column of its own in `tagalys`'s 10:00 file on 2026-10-21.
