# 02: `rate()` and `peak_rate()` aggregates

**Status:** ready-for-agent
**Spec:** [spec.md](../spec.md)

**What to build:** two aggregates for `summarize`, so a Chart of requests per second is one line, such as `where path == "/v2/search" | summarize rate(), peak_rate() by bin_auto(time), shop`.

- **`rate()`**: the rows per second, averaged over the bin: the rows' count divided by the seconds of the bin that lie inside the Query's range. So a partial bin at either edge still reads a true rate. With no time bin in the summarize, the bin is the whole range.
- **`peak_rate()`**: the rows in the busiest whole second of the bin (UTC, from 1970, as `bin(time, 1s)` puts them), within the summarize's groups. With no time bin, the busiest second of the range. It needs a time bin that is a whole number of seconds; `bin(time, 500ms)` with `peak_rate()` is an error at the call, saying so.

Both take no arguments (filter with `where` first), are numbers, and are named as other aggregates are (`rate_`, `peak_rate_`, as `count()` makes `count_`).

## Where

- [ ] `event_store/logquery`: `aggregateFunctions` and `compiler.aggregate` in `expr.go`, the SQL in `sql.go` (`writeAggregate` can't write `peak_rate` alone; it needs each second's count first, as a window over the summarize's groups and the second, or an inner GROUP BY). The summarize stays one stage, so `Query.Bins()` still takes it and the bin cache keeps its bins. The cache keeps only whole bins inside the range, so dividing a partial bin by its part in range doesn't clash with it; check that.
- [ ] Column hints: `agg` is `rate` or `peak_rate`. In `console/app/frontend/lib/charts/series.ts`, both are zero in an empty bin (`ZERO_WHEN_EMPTY`), `rate` stacks (`ADDS_UP`: groups' rates add up to the whole) and `peak_rate` doesn't (groups peak in different seconds), and the two share one panel (`unitOf`: per second).
- [ ] The Builder's summarize offers both (`lib/logs/builder.ts`), and completions and the language's function list know them (`lib/logs/completions.ts`, `lib/logs/language.ts`).
- [ ] A Threshold monitor can end in `summarize peak_rate()` (Console's `MonitorQueryCheck` takes one number per group, so nothing should change; a test says so).
- [ ] Docs: the Aggregates table and a short "Requests per second" example in `docs/logs/query-language.md`, saying why `peak_rate()` keeps per-second accuracy at any bin size; the MCP server's Query help, if it lists aggregates.

## Tests

- [ ] Go (`go test -p 1`, only the touched packages and the root's logs tests): exact values on known Logs, for a whole bin, both partial edge bins, an empty second inside a bin (so `rate()` is below `avg` of per-second counts), groups by a `by` column, no time bin, a sub-second bin refused, and a bin-cache run that gives the same rows cold and warm.
- [ ] vitest for the series rules above.
- [ ] One check on the dev box's Logs: `peak_rate()` over 7 days against `count()` over the same range, both under a minute; the times go in the Comments.

## Comments
