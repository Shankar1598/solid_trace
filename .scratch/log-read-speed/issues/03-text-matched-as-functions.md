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
