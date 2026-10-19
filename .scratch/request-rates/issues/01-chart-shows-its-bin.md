# 01: A time Chart shows its bin size

**Status:** resolved
**Spec:** [spec.md](../spec.md)

**What to build:** a time Chart says the size of the bins it draws, so nobody reads a 5-minute count as a per-minute one. Console only: every table run already returns the size as `bin_seconds` on its time column (`event_store/logquery/table.go`, `Column`), and `lib/charts/series.ts` already reads it into the model's `binSeconds`.

- [x] `components/charts/QueryChart.tsx` shows the bin size on every time Chart, as a short label such as `1s bins`, `30s bins`, `5m bins`, `12h bins` or `7d bins`, small and out of the way of the plot and legend. One place covers the Logs explorer (`LogTableView`), Dashboards (`ChartBody`) and the Monitor form. Bars and single numbers have no bins and no label.
- [x] The tooltip gives the bin's span, start to end, such as `19:02:00–19:03:00`, keeping ` · partial bin` for the faded edge bins (`pointLabel`).
- [x] The label follows a zoom and a range change, since both run the Query again with new bins.
- [x] vitest for the size's text, from 1 second to 7 days, and for a size that isn't a whole unit, such as 90 seconds, which reads `90s`.
- [x] A Playwright flow: a `summarize count() by bin_auto(time)` Chart over the last hour reads `1m bins`, and over the last 15 minutes `10s bins`. No screenshots needed.

Nothing changes in EventStore, the MCP server (its tables already carry `bin_seconds`) or the docs, apart from a line in `docs/logs/query-language.md`'s Charts section saying the Chart shows its bin size.

## Comments

### 2026-10-19 built

Built on `feature/request-rates` (merge `400331e`; review fixes in `efb118c`). The label reads `<size> bins` at the right of the legend row (`data-chart-bin`); the text comes from `lib/charts/bins.ts`.

- **Decision:** the tooltip span takes the bin's own precision: minutes for whole-minute bins (`19:02–19:03`), seconds otherwise (`19:01:30–19:03:00`), milliseconds below a second. The date shows once when both ends fall on the same day.
- **Tests:** vitest (415) and typecheck pass. The flow "a time chart says its bin size, which follows a zoom and the range, and its tooltip gives the bin's span" (`e2e/flows/charts.spec.ts`) passes on the dev stack, as do the Charts, Dashboards and Monitors flows.
