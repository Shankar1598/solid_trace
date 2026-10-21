# 02: Newest page read in two steps

**What to build:** A page of Log rows, as the Logs explorer, MCP `query_logs` and the Query route list them, reads each hour in two steps inside its one read:

1. It reads only `time` from the hour's sources (late Logs, the hot table and the hour's Log files), with the scan's bounds, cursor and Query condition, and takes the time of the Log at the page's remaining limit.
2. It reads whole Logs only at or after that time.

An hour with fewer Logs than the remaining limit is read as today. The page's Logs, order and cursor don't change. The scan's interface doesn't change. See the [spec](../spec.md)'s Implementation Decisions.

**Blocked by:** 01.

**Status:** ready-for-agent

- [ ] Logs at the same time as a page's last Log, spread across the hot table, late Logs and a Log file in one hour, are split across pages by id, with none skipped or repeated (root test through the Query route)
- [ ] An hour with fewer Logs than the limit lists them all, with no cursor
- [ ] A cursor continues inside an hour and across hours
- [ ] A Query condition with a small page limit lists the same Logs as listing all and filtering
- [ ] A run that spends its scan budget still answers what it found and a cursor
- [ ] The existing Query root tests pass
- [ ] The probe's newest-page timing, before and after, is in this ticket's Comments

## Comments

### 2026-10-21 built, with probe figures

What `scanHour` (`event_store/storage/logs_query.go`) now does for each hour, inside the scan's one read:

1. It reads only `time` and `id` of the newest `limit` Logs within the bounds, cursor and condition: `SELECT time, id … ORDER BY time DESC, id DESC LIMIT limit`.
2. If it found none, the hour isn't read again. Else it reads those Logs whole: each source within `time >= oldest found AND time <= newest found`, then `WHERE list_contains([found ids], id)`, then `ORDER BY time DESC, id DESC`.

How this differs from the spec:

- **Step 2 picks Logs by id, not by running the condition again.**
  - As written in the spec, step 2 ran the condition again, from the cutoff to the end of the hour. In an hour with fewer matches than the page, that is the whole hour, so a rare search matched every Log twice. The probe showed it: `search "zebrafish"` went from 1,032 to 2,264 ms at 300k, before 03.
  - Picking by id never runs the condition twice. The time span still prunes row groups as the spec intends.
- **`list_contains`, not `IN`.** DuckDB 1.5.6 turns `id IN (…)`, and an `OR` of `id = …`, into a join after the scan. The scan then holds the list only as an optional zone-map filter (EXPLAIN ANALYZE: 125,001 rows read with every column). `list_contains` runs as a filter inside the scan, so DuckDB reads the other columns only where an id matches.
- **Tried and dropped:** splitting step 2 into one branch for each cluster of found Logs. It halved step 2 in the hot table, but each extra `read_parquet` made Log files slower.

Probe figures. Same command as 01's Comments, run alone under the probe lock. "Before" is `feature/log-read-speed` at 6cad5af (01 and 03); "after" is this branch with 03 merged. Each pair was run back to back on the dev box (2 threads, best of 3, in ms). The 300k figures are the second of two runs; the first agreed within 17%.

| Read | 150k hot | 150k file | 300k hot | 300k file |
|---|---|---|---|---|
| Newest page, no Query | 241 → **60** | 226 → **90** | 323 → **52** | 366 → **106** |
| `search "zebrafish"` | 265 → 280 | 228 → 309 | 447 → 489 | 378 → 522 |
| `where message contains "zebrafish"` | 76 → 88 | 145 → 215 | 125 → 160 | 265 → 373 |
| `where message contains_cs "zebrafish"` | 53 → 62 | 96 → 139 | 85 → 96 | 146 → 221 |
| `contains … and shop == "s500"` | 80 → 85 | 148 → 230 | 125 → 147 | 257 → 384 |
| `shop == "s500" and contains …` | 77 → 87 | 154 → 228 | 126 → 145 | 278 → 378 |

- **The newest page** is 4 to 6.5 times faster in the hot table and 2.5 to 3.5 times faster in a Log file. It barely grows with the hour: 52 ms at 300k against 60 at 150k.
- **The trade-off:** an hour with fewer matches than the page (all the probe's searches find 3 to 12) now costs step 1 plus step 2.
  - In the hot table that adds 0 to 15%. In a Log file it adds 40 to 50%, because step 2 decompresses again the row groups that hold the matches.
  - An hour with no match reads only step 1.
  - Against 01's baseline (before 03), these searches are still faster. For example, `contains` in a Log file at 300k: 523 ms then, 373 now.
- **A possible follow-up, not built:** once an hour of a scan finds fewer than its share, read the later hours in one step.

### 2026-10-21 follow-up: later hours of a sparse scan in one step

Once an hour has fewer Logs than the page still wants, `ScanLogs` reads the scan's later hours in one step, as before 02. The loop returns as soon as an hour fills the page, so this is every hour after the first (`i > 0`). The first hour, the one a cursor points into, still takes two steps.

The probe reads one hour, so the follow-up can't change its figures. I judged it with a scratch test instead, not committed, kept in `~/.cache/solid-trace/log-read-speed-notes/lrs02-probe/scratch_test.go.txt`. It used the probe's builders: dev box, 2 threads, best of 3, in ms.

A scan of 3 hours, each a Log file of 150,000 Logs, read three ways:

| Read | Two steps every hour | Switch (this follow-up) | One step every hour (before 02) |
|---|---|---|---|
| Newest page | 86 | 85 | 200 |
| `contains "zebrafish"` (18 Logs) | 659 | 564 | 435 |
| `contains_cs "zebrafish"` (9) | 441 | 341 | 257 |
| `search "zebrafish"` (18) | 943 | 820 | 686 |

- The newest page doesn't slow.
- The later hours cost about what one step does: for `contains`, about 170 ms an hour against 145.
- Only the first hour still pays for two steps.

**Why a rare match in a Log file costs more in two steps:** at 300,000 Logs, the probe's file has 10 row groups (9 of 30,720 Logs and 1 of 23,520), and all 10 hold a hit (rows 12,500 + 25,000·k). So step 2 decompresses the whole file again.

The same 12 hits clustered in one row group (300,000 Logs, one hour):

| Read | Two steps | One step |
|---|---|---|
| `contains`, hits spread over 10 row groups | 356 | 249 |
| `contains`, hits in 1 row group | 235 to 255 | 199 |
| `search`, hits spread | 508 | 376 |
| `search`, hits in 1 row group | 375 to 394 | 383 |

- Step 2 costs about as many row groups as hold a hit.
- Production's hour, about 12M Logs in about 390 row groups, would have a rare search's step 2 touch only the few that hold its hits.

**Costs of the switch:**
- If the newest hour of a range holds fewer Logs than the page, the hour before it is read in one step. This happens on a range ending just past the top of an hour, or on a page whose cursor sits near the start of an hour.
- Such a page costs what it did before 02.
