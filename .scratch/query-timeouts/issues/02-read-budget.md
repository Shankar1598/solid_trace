# 02: Size a Query before running it, and refuse with narrower alternatives

**What to build:** before EventStore runs a Query, it estimates how much the Query would read, from `log_source_counts` and `log_minute_counts`, without reading `logs`. If the estimate is over the budget, EventStore answers at once with a refusal that does the narrowing for the agent:

- the range's volume and the budget;
- the largest range that fits;
- the busiest files in the range, with their Logs;
- a narrower Query ready to run.

A `force` parameter skips the check. MCP `query_logs` passes the refusal back as its answer and takes `force` and `estimate`. See the [spec](../spec.md)'s Solution and Implementation Decisions.

**Blocked by:** None (can start immediately)

**Status:** resolved

- [x] **Calibrate first.** From production's `query_log` (Log reads carry a `log_comment`), pick the bytes and Logs limits so that the reads that finish today wouldn't be refused, and the big scans that timed out would be. Record the figures in a comment here.
- [x] **The estimate.**
  - It uses only the counts tables: each day's Logs and bytes per service and file, scaled by the share of the day's Logs in the range (`log_minute_counts`).
  - The Query's `service` and `file` conditions (`==`, `in`) pick the files; no other condition narrows it.
  - Queries that match text in `message` or `search_text` (`search`, and `contains`, `startswith` or `=~` on `message`) are measured in bytes, and only they can be refused.
  - All other Queries, plain Log lists and tables, are never refused. They log their estimate in Logs, so a limit can be calibrated later (changed 2026-10-30, see Comments).
- [x] **The refusal** names the estimate, the budget, the largest range that fits, the top files in the range by Logs (up to 5), and a narrower Query. It never mentions `force`. It isn't a timeout error: MCP returns it as the tool's text, and the API marks it so the explorer can show it (03).
- [x] **`force: true`** (API parameter and MCP `query_logs` argument) skips the estimate. A forced read keeps the 30 s timeout and runs with a lower ClickHouse `priority`.
- [x] **`query_logs`'s description** says `force` is only for when the user asks for the Query to run as written.
- [x] **`estimate: true`** on MCP `query_logs` answers the estimate without running the Query.
- [x] **Dashboard Charts and Monitors** aren't refused. Their runs log the estimate, so the budget can be checked against them later (see the spec's Open Questions).
- [x] **Tests:**
  - a `search` over a range past the budget is refused, with a narrower Query that the budget accepts;
  - a `file ==` Query over the same range runs;
  - `force` runs the refused Query;
  - an estimate over a range that crosses days adds up the days.
- [ ] **On production after a release:** the `search` Queries that timed out on 2026-10-28 and 2026-10-30 (in the spec) are refused within a second, with alternatives.

## Comments

**2026-10-30, calibration (orchestrator, from production `query_log`, 2026-10-27 to 30, `table` reads, planning stalls left out).** Text-match reads by ClickHouse `read_bytes`:

| read_bytes | reads | failed | p50 | max |
|---|---|---|---|---|
| under 4 GB | 28 | 1 (a planning stall) | 0.4–7.1 s | 7.1 s apart from the stall |
| 4–13 GB | 9 | 2 | 8.2–13.3 s | 38.8 s |
| 13 GB and up | 7 | 7 | 30.1 s | 43 s |

Starting defaults, configurable:
- **Text-match Queries:** 4 GiB of kept bytes in the range's matching files. A full `message` and `search_text` scan reads about 0.8× kept bytes, so this is about 3.2 GiB read.
- **Other Queries:** not refused (see the next comment).
- **Heavy (04):** a text-match estimate over a quarter of the budget, or forced.

The figures are also in `~/.cache/solid-trace/query-timeouts-notes/notes.md`.

**2026-10-30, scope change: only text-match Queries are refused.** A side review pointed out that a limit in Logs would refuse plain Log browsing:
- the explorer with no Query, or with only `where` filters, reads only the newest page, so it's cheap at any range;
- a 100M-Logs limit would still refuse "Last 7 days" on `tagalys` (about 27 hours fits) and "Last 24 hours" on `tagalys-verbose` (about 9 hours fits);
- that figure had no data behind it, and all 9 big-scan timeouts were text matches.

So non-text Queries log their estimate and run. Tests: a plain Log list over 7 days on a busy Project runs, and so does a 7-day `summarize` without text matching.

**2026-10-30, built on `query-timeouts/02-read-budget`, merged into `feature/query-timeouts` (`94e88375`).**
- **The estimate:** `storage/logs_read_volume.go` and `logquery/reads.go`.
  - It reads only the counts tables, without a `log_comment`, so the Overview doesn't count it as a Log read.
  - A part-day is scaled by its share of the day's Logs by minute, or by time when the counts by minute have nothing for that day.
  - It's narrowed by `service`/`file` `==` and `in` conditions in the leading `where`s, Dashboard filter values included.
  - A text match is `search`, or `contains`, `startswith`, `=~` or `matches regex` (and their negations) on `message`, anywhere in the Query.
- **The refusal:** HTTP 200, with a top-level `refusal` key: the message, the estimate, the largest range that fits (to the minute), up to 5 busiest files, and a narrower Query. The narrower Query is the busiest top file that fits over the whole range, otherwise the same Query over the largest range. API types are in `handler/logs_budget.go`.
- **Who is checked:** callers opt in with `check_budget` (the explorer and MCP). Charts and Monitors log `kind=log_read_estimate` only, and Match monitors aren't sized.
- **`force`:** sets `"forced":true` in `log_comment` and runs at ClickHouse `priority` 10. Every other tagged Log read now runs at `priority` 1, because ClickHouse only weighs statements that have one; ingest has none.
- **Config:** `logs_text_match_budget_mb` (default 4096; 0 turns refusals off). It isn't on Console's Instance settings page.
- **Beyond the ticket:**
  - The explorer's histogram is sized and refused the same way, since a search's histogram reads the same text.
  - A page after the first (with a `cursor`) isn't refused.
- **Tests:** `event_store/logs_read_budget_test.go` (6 tests), the existing run, timeout, SQL-logging, histogram, Overview and newest-page Go tests, `./handler` and `./config`, and Rails `test/mcp`, `logs_controller_test`, `event_store_test` and `call_timings_test` (295 runs).
- **To watch:**
  - A Log-row search on `tagalys-verbose` (about 10 GB an hour) is refused even over the last hour, though a common term would find its page fast. That's the spec's out-of-scope "searching in slices".
  - Until 03 lands, the explorer shows "The Query failed (HTTP 200)" for a refused Query.
