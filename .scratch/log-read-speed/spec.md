# Log reads that don't match or read what they don't need

**Status:** ready-for-agent

**Tickets:**
- [01](issues/01-probe-for-log-read-speed.md): probe.
- [02](issues/02-newest-page-in-two-steps.md): newest page in two steps.
- [03](issues/03-text-matched-as-functions.md): text matched as functions.
- [04](issues/04-text-matches-run-last.md): text matches last.
- [05](issues/05-check-on-production.md): check on production, `ready-for-human`.

Planned with the user on 2026-10-21, after opening the Logs explorer on the `tagalys` Project held both of the production VM's CPUs at 100%. The user asked whether the first page could read only Log ids from DuckDB and fetch the 200 Logs it shows from Pebble, and whether conditions on `message` could run after every other condition. Pebble can't serve Logs, but both ideas carry over to DuckDB. Probes on the dev box found a third, larger cost: how a Query matches text.

## Problem Statement

A member who opens the Logs explorer on a busy Project waits seconds for its first page, and everyone else using the install waits too, because the run takes both of a small VM's CPUs. On 2026-10-21 the `tagalys` Project took about 3,300 Logs a second. Console's own Logs show each default load (no Query, the last hour) took 1.3 to 4.8 s for its first page of Logs and 0.3 to 2.5 s for its histogram. The first page was slower the later it was in the hour.

Searching is slow everywhere a Query runs. A search, or a `contains` on `message`, takes seconds over an hour of a busy Project in the explorer, on a Dashboard's Charts and in Monitors. Adding a narrower condition next to it often doesn't help: a condition on an Attribute that isn't promoted still leaves every Log's `message` to be matched.

## Solution

The same Queries give the same Logs, tables and cursors, but each does less work:

1. **The newest page reads two narrow steps instead of whole Logs.** A page of Log rows first finds how far back its newest Logs reach, reading only their times, then reads whole Logs only from there. Opening the explorer then reads the `message`, maps and promoted columns of about 200 Logs, not of every Log in the hour.
2. **Text is matched as text.** `contains`, `startswith`, `=~` and their negations, and `search`, match with DuckDB's substring and prefix functions, not `ILIKE`/`LIKE` patterns. They give the same answers about 6× faster (case-insensitive) and 9× faster (case-sensitive).
3. **Text matches run last.** Within an `and`, and across a Query's leading `where` and `search` stages, conditions that match text come after those that don't. So `message contains "x" and rare_key == "y"` matches `message` only on the Logs where `rare_key` is `y`, however the member wrote it.

Nothing changes in Console, in the API's answers, or in the Query language.

## User Stories

1. As a member, I want the Logs explorer to show its first page of a busy Project's Logs in well under a second, so that I can start reading at once.
2. As a member, I want the explorer's first page to cost about the same at 12:55 as at 12:05, so that how far we are into the hour doesn't decide how long I wait.
3. As a member, I want opening the explorer not to slow everyone else's Charts, Dashboards and Monitors, so that one person looking at Logs doesn't hold up the install.
4. As an operator on a 2 vCPU VM, I want loading the explorer not to hold both CPUs at 100%, so that Log ingest and Log processing keep up while people read Logs.
5. As a member, I want the newest page to list exactly the Logs it lists today, in the same order, so that nothing appears, vanishes or moves.
6. As a member, I want Logs that share a time with the last Log of a page to be split between pages exactly as today, by id, so that paging never skips or repeats a Log.
7. As a member, I want older pages fetched from a cursor to stay as fast as the first, so that scrolling back through an hour is smooth.
8. As a member, I want a page to read the hot table, late Logs and Log files together as today, so that a late Log or one just exported shows in its place.
9. As a member, I want an hour with fewer Logs than a page to answer as today, so that quiet Projects and narrow ranges are unaffected.
10. As a member, I want a run that spends its scan budget to answer what it found and a cursor, as today, so that rare searches still stream back.
11. As a member, I want `search "text"` to answer in a fraction of today's time, so that typing a search in the Builder feels immediate.
12. As a member, I want `search` to keep finding the text in `message` and in every string Attribute, promoted or not, so that a faster search finds the same Logs.
13. As a member, I want `contains` and `!contains` to stay case-insensitive and `contains_cs` and `!contains_cs` case-sensitive, so that my Queries mean what they meant.
14. As a member, I want case-insensitive matching to treat non-ASCII letters as today, so that searches in other languages find the same Logs.
15. As a member, I want a term containing `%`, `_` or `\` to be matched as those characters, so that searching for `100%` or `user_id` finds exactly that text.
16. As a member, I want `startswith` and `=~` and their negations to give the same Logs, faster, so that every text operator benefits.
17. As a member, I want a negated text match to keep Logs that lack the Field, as today, so that `!contains` still reads the way I expect.
18. As a member, I want `message contains "x" and rare_key == "y"` to run as fast as when I write the conditions the other way round, so that I needn't know which Attributes are promoted.
19. As a member, I want several `where` and `search` stages to run their cheap conditions first, so that a long Builder filter with a search stays fast.
20. As a member, I want conditions on Attributes promoted partway through a day, or kept as JSON, to go before text matches too, so that the speed-up holds whatever the day's promoted set is.
21. As a Dashboard viewer, I want Charts that filter on text to draw faster, so that a Dashboard with a search-based Chart loads quickly.
22. As a Dashboard viewer, I want cached bins to stay valid, so that faster matching doesn't throw away the bin cache.
23. As a Monitor owner, I want Threshold and Match monitors with text conditions to run faster and fire exactly as before, so that alerts are cheaper without changing.
24. As a member using the MCP server, I want `query_logs` and Chart data to answer faster with the same results, so that agents can search Logs without timing out.
25. As a member using SQL mode, I want the SQL a Query opens as to use the faster matching, so that editing it starts from a fast query.
26. As a member, I want the histogram to count the same Logs as the page, so that the strip and the stream still agree.
27. As an operator, I want a probe that times these reads in under a minute, so that a DuckDB upgrade that undoes them is noticed.

## Implementation Decisions

- **Pebble isn't a store for reads.** A Log batch is deleted from Pebble once Log processing has committed it to DuckDB (ADR 0004 and 0005). Pebble keeps batches by Project and batch, not by Log id, and holds Logs as they arrived, before the Log transform. Serving pages from it would need a second copy of every Log for the whole Retention period and would show pre-transform Logs. Every read stays in DuckDB.
- **Two steps per hour in the newest-first scan.** The LogStore's scan of a Project's newest Logs still reads hour by hour, newest first, inside one read (one snapshot). For each hour it:
  1. Reads only `time` from the hour's sources (late Logs, the hot table, the hour's Log files), with the scan's bounds, cursor and Query condition. It takes the time of the Log at the page's remaining limit, newest first.
  2. Runs today's whole-Log query with `time >= that time` added.

  If the hour has fewer Logs than the remaining limit, step 2 runs without the extra bound. Ties at the cutoff are all read in step 2, and today's `ORDER BY time DESC, id DESC LIMIT` picks among them. The scan's interface (its request, its page and its cursor) doesn't change.
- **Why the cutoff prunes.** Log files are written sorted by time and id in 30,720-Log row groups, and the hot table fills in arrival order, close to time order. DuckDB's per-row-group minimum and maximum of `time` then skip all but the last row group or two. This is never needed for correctness: a hot table out of order only makes step 2 read more.
- **Why not rely on DuckDB.** DuckDB 1.5.6 reads only the needed rows' other columns when a top-N reads one table. It doesn't for today's union of sources, which reads every column of every Log in the hour. Splitting the union per source would depend on that optimisation surviving upgrades. The explicit two steps don't.
- **Text matches as functions, not patterns.** The Query compiler's text match writes:
  - case-insensitive `contains` as `contains(lower(x), lower('term'))`;
  - `contains_cs` as `contains(x, 'term')`;
  - `startswith` as `starts_with` on the same lowercasing for the case-insensitive form;
  - `=~` as an equality of lowercased values.

  The term is no longer escaped for `LIKE`, since no pattern is involved. The term is lowercased by DuckDB's `lower`, the same Unicode lowercasing `ILIKE` uses for non-ASCII text, so answers don't change. Negated forms keep today's `NOT coalesce(…, false)`, so a Log lacking the Field still passes a negation. `matches regex` is unchanged.
- **`search` uses the same function.** It does so on `message`, on each value of the string map and on each promoted string column, still joined by `or`.
- **Text matches last.** When the compiler writes an `and` (written, or one made by pushing a negation through an `or`), the operands that contain no text match go first and those that do go after, each group in its written order. The leading `where` and `search` stages, joined by `and` in the Query's condition, are ordered the same way. A `search` stage counts as a text match. This relies on DuckDB 1.5.6 evaluating a scan's non-pushable filters in the order written, which the probe showed. Simple comparisons on built-in fields and promoted columns are pushed into the scan and run first whatever the order. `or` isn't reordered: it can't save work, since a Log failing one side must still be tested on the other.
- **Where it applies.** Text matching and ordering live in the compiler's condition writer. That writer is shared by the explorer's pages, the histogram, Charts and the bin cache, Threshold and Match monitors, the MCP tools and the readable SQL that seeds SQL mode. The two-step read is only in the newest-first scan of Log rows. Tables, the histogram, a Match monitor's scan of Logs ingested since its last run, and fetching one Log by id read as today.
- **The bin cache stays valid.** Cached bins are keyed by the Query, its bin size and start, and Log file generations, not by the SQL written. A bin cached before this change gives the same counts after it.
- **No schema, API, Console or language change.** The Query route's request and answer, cursors and elapsed time keep their shape.

## Testing Decisions

- **One seam: EventStore's Query route**, through the root tests that post to `/logs/query` and `/logs/histogram`, and table runs, as the existing Query tests do. They test what a caller sees: which Logs, in what order, with what cursor, and which tables. They don't test the SQL written, the order of conditions or the number of steps. Speed is shown by a probe, not asserted.
- **The newest page.**
  - Logs at the same time as a page's last Log, spread across the hot table, late Logs and a Log file of one hour, are split across pages by id, with none skipped or repeated.
  - An hour with fewer Logs than the limit lists them all with no cursor.
  - A cursor continues inside an hour and across hours.
  - A Query condition with a page limit lists the same Logs as listing all and filtering.

  Prior art: the root tests for pages continuing from the cursor across hours and sources, and for a rare search across days.
- **Text matching.** Each text operator and its negation, and `search`, find the same Logs in the hot table, late Logs and Log files for:
  - mixed case;
  - non-ASCII text whose case differs (`ÉCOLE`/`école`, `STRASSE`/`Straße` as `ILIKE` treats them today);
  - terms with `%`, `_` and `\`;
  - Logs lacking the Field.

  Prior art: the root test of each operator across the hot table, files and late Logs, whose cases extend naturally.
- **Order of conditions.** The same Query written with its conditions in each order lists the same Logs. The conditions are a text match on `message`, a condition on an Attribute only in the maps, one on a promoted Attribute and one on a key promoted partway through a day. Prior art: the root test of Queries agreeing across promotion, the daily set and demotion.
- **Charts and Monitors.** A text-filtered summarize gives the same table, and a cached Chart's bins the same counts, as before. Prior art: the bin cache and Chart root tests.
- **The probe.** A probe gated by an environment variable, like the storage layout probes, builds about 1.5M synthetic Logs (one hour in the hot table, the same hour as a Log file). On 2 threads, under a minute, it times:
  - the newest page in one step and in two;
  - `search` and `contains` in the old and new forms;
  - a text match and a map Attribute condition in both orders.

  It isn't part of the suite.
- **On production, after release:** time default explorer loads on `tagalys` from Console's own Logs, as on 2026-10-21, and compare them with the figures below.
- Targeted Go root tests and the `logquery` and `storage` packages are enough to merge. The user runs the full suites.

## Out of Scope

- Serving Log rows from Pebble, or keeping Log batches in Pebble after Log processing.
- A search index, and any change to how `message` is stored or compressed.
- Making a lone match on `message` avoid reading it: a search over an hour still reads that hour's `message`.
- Reordering `or`, and any cost model beyond "text matches last".
- `matches regex`.
- Two-step reads for tables, the histogram, a Match monitor's scan of ingested Logs, and fetching one Log by id.
- Console changes, including the explorer firing its histogram and first page at once.
- Raising the scan budget or the page size.

## Further Notes

- **Production, before (2026-10-21, `tagalys`, default load, from Console's own Logs).** First page 2,292 to 4,798 ms after 10:15 into an hour, 1,304 to 1,332 ms at a quarter past. Histogram 325 to 2,514 ms, slowest while the first page ran beside it. About 3,300 Logs a second. An hour of `tagalys` Parquet is 563 MB, of which `message` is 367 MB stored and 2,986 MB decompressed.
- **Probe figures** (dev box, 2 threads, DuckDB 1.5.6, synthetic Logs, scratch code not kept in the repo).

  Newest 200 of 1.2M Logs, hot table:

  | Read | Time |
  |---|---|
  | Today's union | 245 ms |
  | Two steps | ~25 ms |

  1.5M Logs, one rare term:

  | Condition | Hot table | Log file |
  |---|---|---|
  | `ILIKE … ESCAPE`, as compiled today | 1,857 ms | 1,922 ms |
  | `contains(lower(message), …)` | 274 ms | 696 ms |
  | `LIKE … ESCAPE` (case-sensitive) | 1,358 ms | n/a |
  | `contains(message, …)` (case-sensitive) | 141 ms | n/a |
  | Reading `message` without matching | 124 ms | 323 ms |
  | Map Attribute `and` message, message first | 1,820 ms | 2,058 ms |
  | Map Attribute `and` message, message last | 129 ms | 360 ms |
  | Promoted or built-in `and` message, either order | 130 ms | 260 ms |
- **Unpacking `message` is cheap only in the hot table.**
  - In the hot table, a case-sensitive `contains`, which reads every byte of every `message`, took 141 ms for 580 MB.
  - In a zstd Log file, reading `message` alone took 323 ms for 580 MB (129 MB stored).
  - Production's hour holds 2,986 MB of `message`, about 5× the probe's. So unpacking alone is likely 1.5 to 2 s of both CPUs per hour of Log files, before any matching (an estimate, not measured).
  - Most Logs a Query reads are in Log files. Once matching is fast, unpacking is the larger part of a lone `message` search there.
  - Ordering skips the match on Logs that fail a cheaper condition, not the unpacking: a Log file's page is decompressed whole.
  - Cutting the unpacking means changing how `message` is stored or indexed, which is out of scope here.
- `ILIKE` without `ESCAPE` was 1,297 to 1,438 ms, so dropping only the `ESCAPE` isn't enough. DuckDB rewrites `LIKE '%x%'` to `contains` only in simpler cases than ours.
- If a later DuckDB evaluates scan filters adaptively, "text matches last" becomes redundant but harmless.
