# 06: A notice on SQL that matches message slowly

**What to build:** 03 made Queries match text with DuckDB's string functions, but SQL runs as written, so an MCP client's `run_sql` (or SQL mode) with `message ILIKE '%x%'` still takes the slow path. Asked for by the user on 2026-10-21: don't refuse such SQL, run it, and add a notice to the answer saying what to write instead; also say so in `run_sql`'s description.

- EventStore finds, in the parse its read-only check already makes, `ILIKE`/`NOT ILIKE` (with or without `ESCAPE`) and `LIKE`/`NOT LIKE` with `ESCAPE` on the `message` column, with a constant pattern that starts with `%`, and answers `"notice"` beside `"table"`.
- For `%term%` with no wildcard in `term`, the notice gives the `contains` rewrite; otherwise `regexp_matches` with `(?i)`.
- Console's `run_sql` passes the notice on; its description says to match with `contains(lower(message), …)` or `starts_with`, not `ILIKE`. `docs/logs/sql.md` gains "Matching text".

**Blocked by:** 03

**Status:** resolved

- [x] `ILIKE` on `message` with a leading `%` still answers its rows, with a notice
- [x] A plain `LIKE '%x%'`, a pattern with no leading `%`, other columns and text in string literals get no notice
- [x] `run_sql` passes the notice on and its description says how to match text
- [x] Docs

## Comments

**2026-10-21, built on `feature/sql-text-match-notice`.** Timed first on 1.5M synthetic messages in an in-memory DuckDB 1.5.6, 2 threads, best of 3, to settle which forms are slow (probe not kept):

| Condition | ms |
|---|---|
| `contains(message, 'x')` | 18 |
| `message LIKE '%x%'` (DuckDB rewrites it as `contains`) | 20 |
| `contains(lower(message), 'x')`, `lower(message) LIKE '%x%'` | 70–72 |
| `regexp_matches(message, '(?i)x')`, `'(?i)zz.rare'` | 88–90 |
| `message ILIKE 'x%'`, `LIKE 'x%'` (prefix) | 0–14 |
| `message ILIKE '%x%'`, `NOT ILIKE`, `ILIKE '%zz_rare%'` | 400–426 |
| `message LIKE '%x%' ESCAPE '\'` | 418 |
| `message ILIKE '%user%status%'` against `regexp_matches(message, '(?i)user.*status')` | 290 against 184 |

So a plain `LIKE` and prefix patterns get no notice, and the regexp advice says "faster", not "several times faster", as two wildcards gain only 1.6×. DuckDB parses `ILIKE` as `~~*`, `NOT ILIKE` as `!~~*`, and the `ESCAPE` forms as `like_escape`, `not_like_escape`, `ilike_escape` and `not_ilike_escape`; the column is the first child, the pattern the second, the escape the third.

Left as is: only `message` is checked (Attributes and other columns are far smaller); only the first slow match is named; `lower(message) ILIKE …` gets no notice; the explorer's SQL mode doesn't show the notice yet.

Tests: `storage` (`TestSQLThatMatchesMessageSlowlyIsToldWhatToWriteInstead`, on DuckDB's own parse) and `handler` pass, with the root test `TestSQLThatMatchesMessageSlowlyRunsAndSaysWhatToWriteInstead` and its SQL neighbours; Console's `test/mcp/logs_tools_test.rb`, `test/mcp/base_test.rb` and `test/integration/mcp_test.rb`, and RuboCop on the changed files.
