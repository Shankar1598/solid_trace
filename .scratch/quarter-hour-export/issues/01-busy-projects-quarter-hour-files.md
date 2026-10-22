# 01: Busy Projects' quarter-hours are exported to their own Log files

**What to build:** every quarter-hour, the export moves a busy Project's quarter-hours that ended 15 minutes ago into their own sorted Log files, so its hot table no longer waits a whole hour to empty; a quiet Project keeps one file an hour, as today. The hot window doesn't change yet (02 does that). See the [spec](../spec.md); code map `~/.cache/solid-trace/export-15m-notes/code-map.md`.

- Prefactor first, with no change in behaviour: the export stages a file for a time range and a name, not only for a whole hour.
- The export's timer runs at each quarter-hour start by its clock, once at start-up, and a minute after a failure.
- Each run has a quarter cutoff (its quarter-hour start minus 15 minutes) and an hour cutoff (the start of the previous hour, today's cutoff). A Project is busy when its hot table holds at least 122,880 Logs of the quarter-hour before the quarter cutoff, a value set once on the LogStore that tests lower. A busy Project's Logs before the quarter cutoff are exported, a quiet Project's before the hour cutoff; late Logs at every run, by the same rules.
- A quarter-hour exported on its own is `data-<mm>.parquet` (00, 15, 30, 45); an hour exported whole is `data.parquet`; Logs of a range that already has its file go to the hour's next `late-<n>.parquet`, at most one per hour per run. An hour that started by quarter-hours keeps going by quarter-hours. Every file is sorted by the Log file order, with the same COPY settings.
- The manifest sorts an hour's files `data.parquet`/`data-<mm>` by minute, then late files by number; the staged-file recovery and the pre-manifest settling handle the new names. A run after a gap writes each missed quarter-hour's file.
- While the hot window is unchanged, a Log arriving after its quarter-hour was exported but inside the hot window is stored hot and written by the next run to a late file.
- The export's log line says which range each file holds; its messages and doc comments stop saying "hourly" and "hour h-2".
- A new ADR records the quarter-hour export and why quiet Projects stay hourly, superseding ADR 0005's "exported every hour, one file per Project per hour". GLOSSARY.md (**Log export**, with _Avoid_: hourly export; **Log file store**; **Late file**) and `docs/logs/object-store.md` say the same.

**Blocked by:** None (can start immediately)

**Status:** ready-for-agent

- [ ] A busy Project's quarter-hours are written to `data-00` … `data-45` by the runs 15 minutes after each ends, and its Logs list the same before and after each run
- [ ] A quiet Project, in the same runs, gets `data.parquet` once its hour reaches the hour cutoff
- [ ] A Log of an exported quarter-hour, arriving inside the hot window, is in the hour's late file after the next run, found by its key's promoted column
- [ ] An hour whose first quarters went out on their own keeps going by quarter-hours after the Project turns quiet
- [ ] A run after an hour's gap writes each missed quarter-hour's file
- [ ] Staged quarter-hour files left by a crash are settled as staged hour files are
- [ ] The timer runs at each quarter-hour start, at start-up and a minute after a failure (a `logprocessing` test with `clock.Manual`)
- [ ] The existing export, bin cache, bucket, manifest, Retention, after-batch, file order and newest page root tests pass unchanged
- [ ] ADR, GLOSSARY.md and `docs/logs/` updated

Tests go in EventStore's root tests beside `logs_export_test.go`, with the busy threshold lowered for the test's LogStore. They check what can be seen (an hour's files and their names, a file's rows, the Logs a Query lists, the log line), never the SQL the export runs.
