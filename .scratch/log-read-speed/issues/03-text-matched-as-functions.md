# 03: Text matched with contains and starts_with, not ILIKE and LIKE

**What to build:** Every text operator in a Query matches with DuckDB's substring and prefix functions instead of `ILIKE`/`LIKE` patterns with `ESCAPE`:

- `contains` and `!contains` as `contains` on lowercased text, the term lowercased by DuckDB's `lower`;
- `contains_cs` and `!contains_cs` as plain `contains`;
- `startswith`, `startswith_cs` and their negations as `starts_with`, lowercased for the case-insensitive forms;
- `=~` and `!~` as equality of lowercased values;
- `search`, on `message`, each string map value and each promoted string column.

Negations keep passing Logs that lack the Field. `matches regex` is unchanged. The answers are the same everywhere a Query runs: the explorer, the histogram, Charts and the bin cache, Monitors, MCP, and the readable SQL that seeds SQL mode. Only the speed changes. See the [spec](../spec.md)'s Implementation Decisions.

**Blocked by:** 01.

**Status:** ready-for-agent

- [ ] Each text operator and its negation, and `search`, find the same Logs in the hot table, late Logs and Log files (root tests through the Query route) for:
  - mixed case;
  - non-ASCII text whose case differs (such as `ÉCOLE` and `école`);
  - terms with `%`, `_` and `\`;
  - Logs lacking the Field.
- [ ] A text-filtered summarize gives the same table, and a cached Chart's bins the same counts, as before
- [ ] The readable SQL of a Query with text matches runs in SQL mode and gives the Query's Logs
- [ ] The existing Query, Chart, bin cache and Monitor root tests pass
- [ ] The probe's `search`, `contains` and `contains_cs` timings, before and after, are in this ticket's Comments

## Comments

### 2026-10-21 probe, before and after

The probe of ticket 01, at its default 300,000 Logs, run back to back: before from `feature/log-read-speed` at 71e690b, after from this branch. Dev box, 2 threads, ms, best of 3; Logs found in brackets, the same before and after.

| Read | Before, hot table | After, hot table | Before, Log file | After, Log file |
|---|---|---|---|---|
| `search "zebrafish"` | 902 (12) | 439 (12) | 733 (12) | 389 (12) |
| `where message contains "zebrafish"` | 459 (12) | 131 (12) | 526 (12) | 284 (12) |
| `where message contains_cs "zebrafish"` | 361 (6) | 82 (6) | 425 (6) | 152 (6) |
| `where message contains "zebrafish" and shop == "s500"` | 492 (12) | 127 (12) | 511 (12) | 246 (12) |
| `where shop == "s500" and message contains "zebrafish"` | 494 (12) | 132 (12) | 496 (12) | 261 (12) |
| Newest page, no Query (unchanged) | 323 (200) | 309 (200) | 333 (200) | 386 (200) |

- `contains` is 3.5× faster in the hot table and 1.9× in a Log file; `contains_cs` 4.4× and 2.8×. A Log file's reads are dominated by unpacking `message`, as the spec expects.
- `search` is about 2× faster. It still matches each Log's string map values in a lambda and every promoted string column, so it stays the costliest.
- With a match, a run of Log rows is now faster than the newest page with no Query: DuckDB seems to read the other columns only for the Logs that match. Ticket 02 should make the newest page itself cheaper.

### Bin cache

The bin cache key is a hash of the SQL the Query writes (`logquery/bins.go`), not of the Query alone as the spec says. So bins cached before this change are misses after it and are worked out again, with the same counts. The cache is in memory and a release restarts EventStore, so nothing is lost. The bin cache tests now chart two text-filtered Queries (`!contains` with `!startswith_cs`, and `search`) and check their cached bins give the same counts as a run without the cache, and that a second run takes its hours from the cache.
