# 03: Logs stored in ClickHouse and read as pages

**What to build:** The tracer bullet. Log processing inserts Logs into ClickHouse's `logs` table, and the Logs explorer's newest page, its filters and `search`, a Log by id, Match monitors and the Fields list read them from there. See the [spec](../spec.md)'s "The `logs` table", "Log processing writes" and "Reads". After this ticket, a Log posted to a Project can be found by any filter on the Logs explorer, from ClickHouse alone.

**Blocked by:** 02

**Status:** ready-for-agent

- [x] **The table:** the `logs` table and the key counts table, as 01 settled them. The `file` sort column is filled from the `file` Attribute.
- [x] **Log processing** inserts each Project's Logs about once a second, after the Log transform, up to today's batch limit:
  - the range of Log batches is recorded in Pebble before the insert, and the insert carries a dedup token from it;
  - the Processing cursor moves in Pebble after the insert, and the batches are deleted;
  - key counts go to the key counts table.

  The hot table, `logs_late`, promotion, the hot window and the Log export's timer are no longer used. Their code goes with this ticket, or with 10 if it's still referenced.
- [x] **The commit counts line** (`kind=log_commits`) counts inserts.
- [x] **The Query compiler** emits ClickHouse SQL for `where`, `search`, `take`, `sort by time` and the text operators, with Attributes read through JSON paths and their types kept. The newest page reads in the way 01 chose.
- [x] **A Log by id** reads the partitions around its id's time first, then the Project, or only its time's partition when the time is given.
- [x] **Match monitors' `after_batch`** reads Logs after the cursor in Log batch order, with the `batch` minmax index.
- [x] **The Fields list** reads the key counts table. Its wording drops "a Log sent more than about 15 minutes after its time may not be counted", in EventStore and in the MCP `list_fields` description.
- [x] **Root tests that pass against ClickHouse** (helpers moved off DuckDB): `logs_newest_page_test.go`, `logs_text_match_test.go`, `logs_after_batch_test.go`, the `where`/`search` cases of `logs_language_test.go`, `logs_query_test.go`'s page cases, and the Fields tests.
- [x] **New root tests:**
  - a Log whose time is hours old is found at once by an Attribute filter;
  - a replayed range of Log batches is stored once;
  - a crash between the insert and the cursor repeats nothing and loses nothing.
- [x] Targeted tests only, `go test -p 1`.

## Answer

Built on `clickhouse/03-logs-stored-and-paged`, merged with `feature/clickhouse` after 06. Notes for later tickets (tables, how Attributes are written and read, the compiler's split, test helpers) are in `~/.cache/solid-trace/clickhouse-notes/03-notes.md`.

**What was built**
- **Tables** (migration 301; 06's events table is 601):
  - `logs` as 01 settled it, with `file` filled from the `file` Attribute when it's a string;
  - `log_key_counts`, a SummingMergeTree partitioned by Project and day;
  - `logs_try`, a Null table that Log processing's content-error search inserts into, so it stores nothing.
  - `logs` and the key counts table both have a dedup window of 10,000 blocks.
- **Log processing** inserts into ClickHouse after the Log transform, up to 10,000 Logs an insert:
  1. It records the range's last batch in Pebble (`loginsert/<pid>`, synced).
  2. It inserts the Logs, then the key counts, each with the token `logs-<pid>-<after>-<upTo>` and 01's settings.
  3. It moves the cursor, now in Pebble (`logcursor/<pid>`), and deletes the batches.

  While the recorded range is past the cursor, the same batches are sent again under the same token, and the Project goes round again for the batches after them. Attributes are typed values at JSON paths, with arrays and objects in `attrs_json` and `search_text` beside them.
  - The Log export's timer is gone.
  - The commit counts line keeps `kind=log_commits` and `commits=`, now counting inserts.
- **Key counts** are deltas from the Space-Saving top-1000 per Project-day. Log processing keeps them in memory and reads them from the table on first use. An evicted key gets minus its count, so its sum drops to 0.
- **The compiler** writes the head (`where`, `search`, text operators, scalar functions, literals) as ClickHouse SQL through `logquery.Columns`.
  - Attributes are read from typed subcolumns (`.:String`, `.:Float64`, `.:Bool`).
  - `search` reads `message` and `search_text`.
  - `file == "x"` filters on the sort column.
  - An empty Attribute name is read with `getSubcolumn`.
- **Reads:**
  - the newest page, by spans (see below);
  - a Log by id: with its time, that day's partition; without it, the days of its id's time ±1 h, then the whole Project;
  - `after_batch`: reads the cursor from Pebble first, then the batches after the Monitor's cursor;
  - the Fields from the key counts table.

  Every read statement is logged for `log_read_sql`. Fields count late Logs, and the `list_fields` description no longer says they may not be.
- **Root tests:**
  - These pass against ClickHouse: the newest page, text match, `after_batch` and Fields tests; the where/search/take cases of `logs_language_test.go`; the page and by-id cases of `logs_query_test.go`; Log processing; ingest/normalise/transform; and the SQL log.
  - New tests: a Log hours old is found at once; a range inserted again is stored once; a crash between the insert and the cursor repeats and loses nothing; Fields count late Logs; a Log fetched by id days after its time; awkward Attribute names.
  - The tests of the export, the quarter-hour export, promotion, the Log file order and the install from before the batch column are deleted. So is `bench/logreads`, since DuckDB's `ScanLogs` is gone.

**The multi-day newest-page check** (`03-probe/results.txt`): three synthetic days of 3M Logs each (5 Services × 30 files), page of 200, best of 3, in ms:

| Filter | Three days, one query | One day | Newest hour |
|---|---|---|---|
| none | 315 | 202 | 189 |
| `level` error | 348 | 215 | 193 |
| region | 360 | 226 | 199 |
| Service and file | 13 | 9 | 9 |
| rare search | 1,571 | 583 | 224 |

- Lazy materialization and the top-K filter are used.
- About 150 ms of every page is materialising 200 rows that lie in about 150 granules. Only the scan of time and id grows with the range.
- `toUInt128(id)` in ORDER BY costs nothing measurable.

**Where it differs from the spec**
- **The newest page is one query per span, not one over the range.** `ScanLogs` first reads the oldest and newest times from partition metadata (`_minmax_count_projection`, 3 ms). Then it reads the newest Log's hour, then spans of 1, 2, 4 … 24 hours. Each span is one lazily materialised query, and the scan budget is checked between spans. On the check above, this costs three days about what one hour does, and it keeps the budget's pages, which the root tests assert.
- **UUID order.** ClickHouse orders UUIDs by their second half. So newest first is `time DESC, toUInt128(id) DESC`, and the batch window and its minmax index use `toUInt128(batch)`.
- **Unstorable Logs.** A Log whose time is outside 1970 to 2149-06 (a Date partition) is a content error and is skipped. ClickHouse errors are store failures by code: memory, disk, too many parts, timeouts and network.
- **`service`** is not Nullable, since it's in the sort key. So `service == ""` now matches a Log with no Service, where DuckDB's NULL didn't. No test asserts either way.

**Left for later**
- **04:**
  - The tail of the compiler is still DuckDB's: aggregates, the CTE chain and its quoting, the dynamic struct, rates, `Select` and `ReadableSQL`.
  - The `message` PREWHERE split isn't emitted.
  - The histogram MV, and the dedup window on its target table, are 04's.
  - Since `bin` now writes `toStartOfInterval`, DuckDB tables and the histogram fail until 04. The consts `tablesOnClickHouse` (04) and `sqlOnClickHouse` (07) turn the guarded checks back on.
- **Expected to fail until their tickets (not run):**
  - 04: the table tests in `logs_language_test.go`, `logs_chart_hints`, `logs_rates`, `logs_histogram`, `logs_bin_cache`, the table and histogram cases in `logs_run_test.go`, and the handler's `TestLogQueryRoutesAreOnlyOnTheQueryApp` (histogram 500);
  - 05: `logs_retention`, `logs_bucket`, `logs_manifest` and `logs_reads`;
  - 07: the `logs_sql*` tests.
- **DuckDB code** (hot tables, `logs_late`, promotion, the export, uploads and cleanup) stays where `ScanLogTable`, Retention and the storage tests still use it, for 04, 05 and 10.
