# Busy Projects' Logs exported every quarter-hour

**Status:** ready-for-agent

Planned with the user on 2026-10-22, after the Log file order ([spec](../log-file-order/spec.md)). Sorted Log files let a Query filtered on `service` or `file` skip row groups, but the hot table can't be kept sorted: it fills in arrival order, and it holds the current hour and the previous one, so up to two hours of a 6 h Dashboard read it whole. The user chose to export more often, every 15 minutes, so that less of every range is in the hot table. Code map from main at `a0562f8`: `~/.cache/solid-trace/export-15m-notes/code-map.md`.

## Problem Statement

A member's Chart, Monitor or search over a busy Project reads its last one to two hours from the hot table, where nothing is sorted, so a filter on one Service or one log file reads every Log of those hours, `message` included. On `tagalys` that is 13 to 30 M Logs. The rest of the range is in Log files, sorted by the Project's Log file order, and skips most of its row groups. The bin cache can't help either: a bin with any Log in the hot table is never cached.

The hourly export is also one large job: on production, `tagalys`'s hour takes 1 min 53 s to 2 min 32 s to write (11.7 to 15.5 M Logs, 2026-10-21/22), and its commit holds up Log processing for 2.5 to 6 s each hour, which is when Log processing falls behind.

## Solution

Every 15 minutes, the export moves a busy Project's quarter-hours that ended at least 15 minutes ago into their own Log files, sorted by its Log file order, as the hourly export does for whole hours. The hot table of a busy Project then holds its last 15 to 30 minutes, not one to two hours. A quiet Project, whose hot table holds too few Logs to gain from it, keeps one Log file per hour, exported as today.

Which Logs count as late doesn't change: a Log that arrives after its quarter-hour was exported, but still inside the hot window, goes to the hot table as now, is promoted and counted as now, and the next export writes it to a late file of its hour. Queries answer the same; only how much they read changes.

## User Stories

1. As a member viewing a Dashboard over a busy Project, I want its last hour or two to read only the Logs its filters name, as the hours before them do, so that the first load doesn't slow down at its newest end.
2. As a member, I want a Chart filtered on `file` over the last hour to read about a quarter of what it reads now, so that the most-viewed range is the fast one.
3. As a member, I want the Logs explorer's newest page to stay as fast as it is, so that the change doesn't trade one slow read for another.
4. As a member, I want a Query to list the same Logs, in the same order, before and after a quarter-hour is exported, so that I never have to think about when the export runs.
5. As a member, I want more of a Dashboard's bins to come from the bin cache, since bins become cacheable once their quarter-hour is exported, so that a refresh reads less.
6. As a member using a Monitor on one source of a busy Project, I want its runs over the last 15 to 30 minutes to read a small hot table, so that Monitors cost less of the VM.
7. As a member, I want a Log that arrives a few minutes late to be found by the same Attribute columns as its neighbours, so that a slow shipper doesn't hide Logs from a filter.
8. As a member, I want `list_fields` to count a Log sent up to an hour after its time, as it does now, so that a Field's popularity doesn't depend on the export.
9. As an operator, I want the export of a busy Project to be four smaller jobs an hour, not one large one, so that Log processing is held up for about a second at a time, not several.
10. As an operator, I want a quarter-hour's export to sort in memory where today's hour spills to disk, so that the export's total cost doesn't grow.
11. As an operator, I want a quiet Project to keep one Log file per hour, so that its file count, its bucket objects and the cold reads of its long ranges don't grow fourfold.
12. As an operator, I want the line between busy and quiet to be one that only matters for speed (a hot table holding at least one DuckDB row group of a Project's Logs), so that I never have to tune it.
13. As an operator, I want the export's log line to say which quarter-hours or hour it wrote, so that I can see the new timing in production.
14. As an operator, I want the export to catch up on the quarter-hours it missed while EventStore was down, as the hourly export catches up on hours, so that a restart loses nothing and writes the same files.
15. As an operator, I want a failed quarter-hour export to retry a minute later, as the hourly export does, so that one failure costs a minute, not 15.
16. As an operator, I want each Log file's name to say which part of its hour it holds, so that I can read the Log file store by eye.
17. As an operator, I want the files already in the Log file store, `data.parquet` and `late-<n>.parquet`, to read as they do, so that an upgrade changes nothing on disk or in the bucket.
18. As an operator, I want Retention, the disk limit, uploads and eviction to treat a quarter-hour's file as a file of its hour, so that they work as they do.
19. As an operator, I want demoted columns still dropped at 01:00 UTC, once no Log of the previous day can reach the hot table, so that the promoted columns stay right across midnight.
20. As a developer, I want the 15-minute interval, the 15-minute wait and the busy threshold each defined once, so that a later change is one edit.
21. As a developer, I want root tests to make a Project busy with a handful of Logs, so that the quarter-hour export is tested without posting a row group's worth.
22. As a developer, I want the export's timer tested with a manual clock, so that a quarter-hour start is never missed silently.
23. As an agent using the MCP server, I want Queries to answer the same as before, so that nothing an agent learned about SolidTrace changes.

## Implementation Decisions

**When the export runs, and what it takes**

- The export's timer runs at each quarter-hour start (:00, :15, :30, :45) by its clock, once when EventStore starts, and a minute after a failure. Today it runs at each hour start.
- Each run has two cutoffs: the **quarter cutoff**, the run's quarter-hour start minus 15 minutes (at 11:15 it is 11:00, so the quarter-hours up to 10:45–11:00, which ended 15 minutes ago, are taken), and the **hour cutoff**, the start of the hot window as today (`HotWindow`'s `from`).
- A Project is **busy** at a run when its hot table holds at least 122,880 Logs (one DuckDB row group) before the quarter cutoff. Below that, a filter reads one row group of the hot table anyway, so there is nothing to skip. A busy Project's Logs before the quarter cutoff are exported; a quiet Project's Logs before the hour cutoff are exported, as today. The threshold is one value on the LogStore, which root tests lower.
- Late Logs (`logs_late`) are exported at every run, by the same rules, as today.

**The hot window stays as it is**

- The ingest split (`HotWindow`, the current hour and the previous one), promotion and the counting of Attribute keys don't change. A Log whose quarter-hour has been exported but whose hour is still in the hot window goes to the hot table, gets its promoted columns, and is written by the next run to a late file of its hour. Shrinking the hot window instead would store more Logs MAP-only in `logs_late`, unpromoted and uncounted.
- Demoted columns are still dropped at the first export after the hot window has left the previous day (01:00 UTC). The code that works this out from the cutoff and a hard-coded two hours works it out from the hot window, so it no longer depends on when the export runs.

**Log files and their names**

- Files stay in their hour's folder (`project=<id>/day=<d>/hour=<h>/`), and the manifest's key (Project, day, hour, name) doesn't change. Reads, Retention, the disk limit, uploads, eviction and the bin cache already work by hour and read every file of an hour; they need no change.
- An hour exported whole is `data.parquet`, as today. A quarter-hour exported on its own is `data-<mm>.parquet`, `mm` being 00, 15, 30 or 45; its Logs all have a time inside that quarter-hour. Logs of an hour or quarter-hour that already has its file go to the hour's next late file, `late-<n>.parquet`, as today; a run writes at most one late file per hour.
- An hour whose first quarters were exported on their own keeps going by quarter-hours, even if the Project turns quiet before the hour ends, so an hour never has both `data.parquet` and `data-<mm>.parquet`.
- Every file is sorted by the Project's Log file order, then time, with the same COPY settings (zstd, row groups of 30,720).
- The manifest's sort of an hour's files puts `data.parquet` and the `data-<mm>` files first, by minute, then the late files by number. The staged-file recovery and the pre-manifest settling handle the new names.

**Domain words and docs**

- GLOSSARY.md: the **Hourly export** becomes the **Log export** (with _Avoid_: hourly export for what now runs every quarter-hour), and the **Log file store**, **Late file** and **Log file order** entries say an hour's Logs are in one file, or in a file per quarter-hour for a busy Project, plus late files. A **Quarter-hour file** term may be needed.
- A new ADR records the decision, superseding the "exported every hour, one file per Project per hour" lines of ADR 0005, and saying why the hot window was kept and why quiet Projects stay hourly. ADR 0007's and 0008's hour-based keys stand.
- `docs/logs/object-store.md`, the export's doc comments ("hour h-2"), its log messages ("Hourly Log export failed"), the cleanup timer's comment, and the Console text that says "hours exported" (the Log file order card, `list_projects`) are updated. `list_fields`'s "a Log sent more than about an hour after its time isn't counted" stays true.

**Production effect, expected**

- `tagalys`: four files an hour of about 3.3 M Logs each, each written in about 30 s without spilling, each commit about 1 s under the write lock. `solidtrace` (about 12 k Logs an hour) stays hourly.
- Bucket objects and cold-read requests for a busy Project's hours grow about fourfold (ADR 0008 counts about 3 requests per hour file today); daily compaction stays the way to cut them if long-range first runs get slow.

## Testing Decisions

- A good test posts Logs at a manual clock's times, runs Log processing and the export as the timer would, and checks what a member or operator can see: the Logs a Query lists, the files of an hour (`assertHourFiles` and the files' names), the rows of a file, and the export's log line. It never asserts the SQL the export runs.
- **The seam: EventStore's root tests**, beside `logs_export_test.go` and `logs_file_order_test.go`, with the busy threshold lowered for the test's LogStore. They cover:
  - A busy Project's quarter-hours are written to `data-00` … `data-45` at the runs 15 minutes after each ends; its hot table then holds only Logs after the quarter cutoff; the Logs list the same before and after each run.
  - A quiet Project, in the same runs, gets `data.parquet` once its hour leaves the hot window, as today.
  - A Log arriving after its quarter-hour was exported, inside the hot window, is promoted (found by its key's column), counted, and written to the hour's late file by the next run; one arriving after the hot window goes to `logs_late` and a late file, as today.
  - An hour whose first quarters went out on their own keeps going by quarter-hours after the Project turns quiet.
  - A run after a gap (EventStore down for an hour) writes each missed quarter-hour's file.
  - Demoted columns are still dropped at 01:00 UTC and their values still read back.
  - The existing hourly root tests (`logs_export_test.go`, `logs_bin_cache_test.go`, `logs_bucket_test.go`, `logs_manifest_test.go`, `logs_retention_test.go`, `logs_after_batch_test.go`, `logs_file_order_test.go`, `logs_newest_page_test.go`) still pass with the default threshold, since their Projects are quiet.
- The timer: a `logprocessing` package test with `clock.Manual` checks it runs at each quarter-hour start, at start-up, and a minute after a failure. The package has no tests today.
- The Console text changes need no new tests beyond the existing flows that read them.

## Out of Scope

- Changing the hot window, what counts as a late Log, promotion or Attribute counting.
- Sorting the hot table, or keeping a sorted copy of it.
- A setting for the interval, the wait or the busy threshold.
- Changing the manifest's key, the folder layout or the bin cache's keys. Each quarter-hour's export changes its hour's generations, so the cached bins of that hour's earlier quarters are dropped up to three times; those bins weren't cacheable at all before.
- Daily compaction or merging an hour's quarter-hour files (ADR 0008).
- Rewriting Log files already exported.

## Further Notes

Production, 2026-10-21 20:00 to 2026-10-22 06:00 UTC, from EventStore's own Logs (`solidtrace`): `tagalys` (Project 3) exports took 1 min 53 s to 2 min 32 s to copy and 2.5 to 6.0 s to commit, for 11.7 to 15.5 M Logs; `solidtrace` (Project 2) took 99 to 191 ms for 11 to 14 k Logs; each checkpoint took 1 to 304 ms. The probe of 2026-10-21 wrote a `tagalys` hour while spilling about 5 GB at 4 GB of memory.
