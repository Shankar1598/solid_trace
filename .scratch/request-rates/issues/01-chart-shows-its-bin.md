# 01: A time Chart shows its bin size

**Status:** ready-for-agent
**Spec:** [spec.md](../spec.md)

**What to build:** a time Chart says the size of the bins it draws, so nobody reads a 5-minute count as a per-minute one. Console only: every table run already returns the size as `bin_seconds` on its time column (`event_store/logquery/table.go`, `Column`), and `lib/charts/series.ts` already reads it into the model's `binSeconds`.

- [ ] `components/charts/QueryChart.tsx` shows the bin size on every time Chart, as a short label such as `1s bins`, `30s bins`, `5m bins`, `12h bins` or `7d bins`, small and out of the way of the plot and legend. One place covers the Logs explorer (`LogTableView`), Dashboards (`ChartBody`) and the Monitor form. Bars and single numbers have no bins and no label.
- [ ] The tooltip gives the bin's span, start to end, such as `19:02:00–19:03:00`, keeping ` · partial bin` for the faded edge bins (`pointLabel`).
- [ ] The label follows a zoom and a range change, since both run the Query again with new bins.
- [ ] vitest for the size's text, from 1 second to 7 days, and for a size that isn't a whole unit, such as 90 seconds, which reads `90s`.
- [ ] A Playwright flow: a `summarize count() by bin_auto(time)` Chart over the last hour reads `1m bins`, and over the last 15 minutes `10s bins`. No screenshots needed.

Nothing changes in EventStore, the MCP server (its tables already carry `bin_seconds`) or the docs, apart from a line in `docs/logs/query-language.md`'s Charts section saying the Chart shows its bin size.

## Comments
