# 06: Builder mode and result visualisation

**What to build:** A Builder | KQL toggle on the Logs page. Builder offers filter rows (field, operator, value), a visualisation (count, distinct, avg, sum, min, max, percentiles) with group-by and a bin size, and compiles one-way to KQL text. Results render by shape: a table for rows, a time series chart for `summarize ... by bin(_time)` output, a statistic for single values; `render` hints and the server-chosen `bin_auto` width are respected.

**Blocked by:** 05

**Status:** wontfix

- [ ] Builder state compiles to valid KQL and switching to the editor shows the generated text
- [ ] Series results draw one line per group with the bin width from `stats`; single-value results show a statistic tile; everything else shows the table
- [ ] Histogram drag-to-zoom narrows the time range in the URL and re-runs the Query
- [ ] Tests cover Builder-to-KQL compilation and result-shape detection

## Comments

2026-09-27: Closed as wontfix. This plan was superseded by the [logs map](../../logs/map.md). The file stays here as reference only.
