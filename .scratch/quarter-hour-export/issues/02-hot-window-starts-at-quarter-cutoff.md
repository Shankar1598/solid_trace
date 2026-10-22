# 02: The hot window starts at the quarter cutoff

**What to build:** the hot window shrinks to start at the quarter cutoff, so a busy Project's hot table only ever holds quarter-hours not yet exported, and a Log more than 15 to 30 minutes late is a late Log. See the [spec](../spec.md) ("The hot window starts at the quarter cutoff", "After an outage").

- `HotWindow` becomes [quarter cutoff, start of the next hour). Log processing's split by it doesn't change: inside goes to the hot table, outside to `logs_late`.
- A quiet Project's on-time Logs stay in its hot table until their hour is exported; only Logs more than 15 to 30 minutes late go to `logs_late`, and the hour's export takes them into `data.parquet`.
- Demoted columns are worked out from the hour cutoff, not from the cutoff plus a hard-coded two hours, so they're still dropped at 01:00 UTC.
- Promotion and Attribute counting are unchanged: hot Logs only.
- Text: `list_fields`'s "a Log sent more than about an hour after its time isn't counted" becomes about 15 minutes; the Log file order card's "hours exported after it" and `list_projects`'s "each hour's Log file" say what is now true. GLOSSARY.md's hot table and late Log wording, and the ADR from 01, say the window follows the quarter cutoff, with the outage trade-off.

**Blocked by:** 01

**Status:** resolved

- [x] A Log of an exported quarter-hour goes to `logs_late`, and the next run writes it to the hour's late file, found by its key's promoted column
- [x] A busy Project's hot table holds no Log before the quarter cutoff after a run
- [x] A quiet Project's Log arriving 20 minutes after its time, before its hour is exported, is in the hour's `data.parquet` after the hour's export
- [x] After a gap, delayed Logs of a range with no file yet go to that range's own file; of a range with a file, to a late file
- [x] Demoted columns are still dropped at 01:00 UTC and their values still read back
- [x] Logs list the same before and after each run
- [x] Console text, GLOSSARY.md and the ADR updated; the Rails tests and flows that read the changed text pass

Tests go in EventStore's root tests, as in 01. Existing root tests that post Logs more than 15 minutes before the clock and expect them hot may need their clock moved; keep what each test checks.

## Comments

**2026-10-22, built on `feature/quarter-hour-export` (merge `26205b9`, review fixes `9e445f8`).** `HotWindow` and the export's cutoffs come from one `exportCutoffsAt`; `dropDemotedColumns` keeps the sets of the days from the hour cutoff on, with no `+2h`, and a busy Project's quarter-hour runs across midnight are tested beside the quiet one. Existing root tests that meant their Logs to be hot now send each hour's Logs while the window takes them (`postWhileHot`, or the clock at hh:29); about 47 ingest and transform tests store their 11:00 Log as late and still check what they checked. GLOSSARY.md gained **Late Log**. `list_fields` and `docs/mcp.md` say a Log more than about 15 minutes late may not be counted; the Log file order card and `list_projects` say each Log file. The flow `project-log-file-order` renders the changed card but doesn't read its text, and wasn't rerun.
