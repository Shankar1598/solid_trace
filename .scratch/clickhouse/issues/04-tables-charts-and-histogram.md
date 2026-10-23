# 04: Tables, Charts and the histogram on ClickHouse

**What to build:** Every Query that answers a table runs on ClickHouse: `summarize`, `project`, `extend`, `top`, `count`, `distinct`, rates and totals. That covers Charts, Dashboards, Threshold monitors and the MCP `query_logs` tables. The histogram reads its MV when it can, and the bin cache keeps working with new generations. The Readable SQL is ClickHouse SQL. See the [spec](../spec.md)'s "Reads".

**Blocked by:** 03

**Status:** ready-for-agent

- [ ] **The rest of the compiler emits ClickHouse SQL.** This covers the table stages, the CTE chain, `bin`/`bin_auto` with `toStartOfInterval` and the same origin, `rate()` and `peak_rate()`, totals and Chart hints. It also covers the aggregates: `count_if`, `dcount` as `uniq`, and the quantile function 01 chose. Any root test whose expected value changes for an aggregate is listed in this ticket's Answer for the user.
- [ ] **The histogram MV:** counts by Project, minute and level. The histogram reads it with no condition and bins of a minute or more, and through `ScanLogTable` otherwise.
- [ ] **The bin cache** keys bins by in-memory generations per (Project, hour). Log processing bumps them after each insert. Bins of an hour that hasn't ended aren't cached.
- [ ] **The Readable SQL** is ClickHouse SQL over SQL mode's `logs` table, as 07 will define it. Until 07 lands, `logs_language_test.go`'s run of each Query's Readable SQL through SQL mode is skipped, with a note pointing at 07.
- [ ] **Root tests that pass against ClickHouse:** the table cases of `logs_language_test.go`, `logs_query_test.go`, `logs_chart_hints_test.go`, `logs_rates_test.go`, `logs_histogram_test.go`, `logs_bin_cache_test.go` (rewritten around generations, not Log files).
- [ ] **New root tests:**
  - the histogram from the MV equals one counted from `logs`, including Logs inserted late into an old minute;
  - a cached bin misses after a Log is inserted into its hour.
- [ ] Targeted tests only, `go test -p 1`.
