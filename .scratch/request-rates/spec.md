# Request rates on Charts

Planned with the user on 2026-10-19, after Better Stack's White Fox chart read 61.8 req/s for a true peak of 41: it stored counts in 15-second buckets, then divided them by a 10-second bin. SolidTrace can't make that mistake, because it bins each Log's own time when the Query runs, but it has no way yet to chart a rate, and a Chart doesn't say what bin it used.

## What we're building

1. **Every time Chart says its bin size** ([01](issues/01-chart-shows-its-bin.md)), such as "1m bins", and its tooltip gives the bin's span, not only its start.
2. **Two rate aggregates** ([02](issues/02-rate-and-peak-rate-aggregates.md)): `rate()`, the Logs per second averaged over the bin, and `peak_rate()`, the Logs in the bin's busiest second. Over a month, a Chart of `summarize rate(), peak_rate() by bin_auto(time)` has 12-hour bins, and each one's `peak_rate_` is still the true busiest second.

## Decisions

1. **`bin_auto` stays at about 100 bins** (the user, 2026-10-19). 1s bins over 3 hours would be 10,800 rows, past the 10,000 a table returns, and about 9 seconds a pixel on a chart. Per-second accuracy comes from `peak_rate()` at any range, and a drag across a Chart zooms to bins small enough to see single seconds.
2. **Rates are aggregates, not a Chart setting**, so the Explorer, Dashboards, Monitors, and the MCP server all get them the same way, in the Query's text, and a Threshold monitor can fire on a peak rate.
3. **One summarize.** `peak_rate()` is worked out inside its summarize, so a Query using it is still made bin by bin and the bin cache keeps its bins. The two-summarize way to get it today (`summarize n = count() by t = bin(time, 1s) | summarize max(n) by bin_auto(t)`) compiles, but the bin cache never takes it, and `avg(n)` overstates the average because seconds with no Logs have no row.

## Out of scope

- A finer `bin_auto` ladder, or one per Chart.
- Raising the 10,000-row table limit.
- Rates finer than a second, and rates of SQL queries.
