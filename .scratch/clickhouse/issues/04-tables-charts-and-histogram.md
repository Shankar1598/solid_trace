# 04: Tables, Charts and the histogram on ClickHouse

**What to build:** Every Query that answers a table runs on ClickHouse: `summarize`, `project`, `extend`, `top`, `count`, `distinct`, rates and totals. That covers Charts, Dashboards, Threshold monitors and the MCP `query_logs` tables. The histogram reads its MV when it can, and the bin cache keeps working with new generations. The Readable SQL is ClickHouse SQL. See the [spec](../spec.md)'s "Reads".

**Blocked by:** 03

**Status:** ready-for-agent

- [x] **The rest of the compiler emits ClickHouse SQL.** This covers the table stages, the CTE chain, `bin`/`bin_auto` with `toStartOfInterval` and the same origin, `rate()` and `peak_rate()`, totals and Chart hints. It also covers the aggregates: `count_if`, `dcount` as `uniq`, and the quantile function 01 chose. Any root test whose expected value changes for an aggregate is listed in this ticket's Answer for the user.
- [x] **The histogram MV:** counts by Project, minute and level. The histogram reads it with no condition and bins of a minute or more, and through `ScanLogTable` otherwise.
- [x] **The bin cache** keys bins by in-memory generations per (Project, hour). Log processing bumps them after each insert. Bins of an hour that hasn't ended aren't cached.
- [x] **The Readable SQL** is ClickHouse SQL over SQL mode's `logs` table, as 07 will define it. Until 07 lands, `logs_language_test.go`'s run of each Query's Readable SQL through SQL mode is skipped, with a note pointing at 07.
- [x] **Root tests that pass against ClickHouse:** the table cases of `logs_language_test.go`, `logs_query_test.go`, `logs_chart_hints_test.go`, `logs_rates_test.go`, `logs_histogram_test.go`, `logs_bin_cache_test.go` (rewritten around generations, not Log files).
- [x] **New root tests:**
  - the histogram from the MV equals one counted from `logs`, including Logs inserted late into an old minute;
  - a cached bin misses after a Log is inserted into its hour.
- [x] Targeted tests only, `go test -p 1`.

## Answer

Built on `clickhouse/04-tables-charts-histogram` from `feature/clickhouse` at 62993aa, which hadn't moved when it finished. Notes for later tickets (the compiler's quirks, the cache's keys, the MV, what 05, 07, 09 and 10 inherit) are in `~/.cache/solid-trace/clickhouse-notes/04-notes.md`.

**For the user: no root test's expected aggregate value changed.** `dcount` is `uniq`, and `percentile` is `quantileTDigest`, so `percentile(duration, 100)` is still 900. What did change in the tests:
- The bin cache tests' hit counts, because only bins of hours that have ended are kept now. At noon that's 16 of the 29 hours, where it used to be 27 of them.
- TestTabularStagesReturnTables checks a restart where it checked the export. The rates test's export step is deleted.
- The bucket and pre-generations bin cache tests are deleted with the Log files they tested.

**What was built**
- **The compiler is ClickHouse SQL throughout.**
  - Table stages, the CTE chain, `bin`/`bin_auto` with `toStartOfInterval`, rates, totals and Chart hints.
  - Aggregates: `count()`, `countIf`, `uniq` and `quantileTDigest`. `sum`, `avg`, `min`, `max` and `percentile` use their `-OrNull` forms, so they stay NULL over no rows, as before.
  - Each stage reads its relation's columns by qualified name. ClickHouse lets a SELECT's alias stand in for a column of the same name, so a project that swaps names was wrong until then; a root test now covers it.
  - An Attribute of no fixed type is a tuple of its typed parts.
- **The PREWHERE split from 01's S8.** A Query's tests that read `message` (or `search`) go in WHERE, and the rest in PREWHERE, whenever a Query has both kinds. This applies to pages, after_batch reads and tables.
- **The histogram MV** (migration 401).
  - It keeps `log_minute_counts` by Project, minute and Level, partitioned like `logs` and with its dedup window. A re-sent range is counted once, and a test fails without the window.
  - With no filter and buckets of whole minutes, the histogram reads the MV. Only the part-minutes at the range's ends are counted from `logs`. Any other histogram goes through `ScanLogTable` and the bin cache.
- **The bin cache** now lives on `ClickHouseLogs`.
  - Keys use in-memory generations per (Project, hour).
  - `InsertLogBatches` bumps the generations of the hours it touched after every logs insert, whether or not the insert succeeded.
  - Bins of hours that haven't ended, by the handler's clock, aren't kept.
- **Readable SQL** is ClickHouse SQL over SQL mode's `logs`. `sqlOnClickHouse` still skips the SQL-mode runs, until 07. A throwaway check ran 25 Queries' Readable SQL directly on ClickHouse's `logs` and got the run's row counts.
- **A run's timeout** also counts a deadline that has already passed. Under load, clickhouse-go's connection can time out just before the context says so, which turned a 504 into a 500 once.

**New root tests:**
- an unfiltered (MV) histogram equals one filtered to keep every Log. It covers Logs late into old minutes and part-minute range ends.
- a cached bin misses once a Log is inserted into its hour.
- the bin cache answers as no cache does as Logs arrive and hours end, and after a restart.
- `assertStoredOnce` also checks the histogram.

**Left for later**
- **05:** Retention and Project deletion must bump the generations of the hours they drop, and drop `log_minute_counts` partitions. The "misses after Retention" bin cache test is 05's.
- **07:** SQL mode's `logs` must expose `attributes`, `attrs_json`, `search_text`, `file` and `id` for the Readable SQL, or the Readable SQL must take other columns. Then flip `sqlOnClickHouse`.
- **09:** the MV and the table source read only `logs`. Old files must join `tableSource`, and a histogram over old hours must add their counts or take the table path.
- **10:** DuckDB's table scan and bin cache (`LogTableScan`, `logs_bin_cache.go`) are now used only by DuckDB's storage tests. The bin cache's GLOSSARY and docs wording should say "generations per hour".
- **11:** the PREWHERE split wasn't timed on production-sized data.
