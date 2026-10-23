# 06: Events in ClickHouse

**What to build:** Event processing inserts Events into ClickHouse's `events` table, and the Issue pages, `get_issue` and integration thresholds read them from there. The nightly archive is gone. See the [spec](../spec.md)'s "Events".

**Blocked by:** 02

**Status:** resolved

- [x] **The `events` table:**
  - `ORDER BY (project_id, issue_fingerprint_id, timestamp)`, `PARTITION BY toYYYYMM(timestamp)`;
  - the nine columns, with `tags` a `Map(String, String)`;
  - the storage policy from 02.
- [x] **Event processing** records each batch's range in Pebble, then inserts it with a dedup token, then moves its cursor. `indexedUpTo`, `HotUUIDsBetween` and the archive's share of `batchLock` go. Issues, `times_seen` and Console messages are unchanged.
- [x] **The three reads keep their answers:**
  - the list, newest or oldest first with offset;
  - the context, previous and next over the Issue's history, with ClickHouse's window functions;
  - the count over a window.

  While ClickHouse is unreachable they answer 503, as now without the view.
- [x] **Cleanup's deleted-Project pass** deletes the Project's Events in ClickHouse, then its Issues and Pebble data, as now.
- [x] **Gone:** `events_hot`, `ArchiveEventsUpTo`, the archive consumer, `event_archive_bucket`, and Console's `Maintenance::EventStoreArchiveJob` with its `recurring.yml` entry and tests. `archive_events` messages still waiting are ignored.
- [x] **Root tests pass against ClickHouse:** `processing_test.go` (replays, held rows, seen counts), the Event cases of `integration_test.go`, `handler/events_test.go` and `routes_test.go`. `event_archive_bucket_test.go` is deleted. A new root test: a replayed batch of Events is stored once.
- [x] **Rails:** `event_store_test.rb`, `issues_controller_test.rb`, `mcp/issue_tools_test.rb` and the integration notification tests pass, and the archive job's tests are removed.
- [x] Targeted tests only.

## Answer

Built on `clickhouse/06-events-in-clickhouse`. Notes for 07 and 10 are in `~/.cache/solid-trace/clickhouse-notes/06-notes.md`.

**What was built**
- **The `events` table** is migration step 601 (`storage/clickhouse_events.go`; the hundreds are the ticket, so 03's steps can't take its number). It has the nine columns: `uuid UUID`, `project_id UInt32`, `issue_fingerprint_id Int64`, `timestamp DateTime64(6, 'UTC')`, four `LowCardinality(String)`s and `tags Map(String, String)`. It has the spec's key and partitions, the `solidtrace` policy and a dedup window of 1000.
- **Event processing**, for each batch:
  1. does the Issue work;
  2. records the range (after the cursor, through the last Event) in Pebble, synced;
  3. inserts the rows with a dedup token made from the range, `async_insert=0` and `max_insert_threads=1`;
  4. counts the Events, tells the Console, and moves the cursor, which deletes the range in the same Pebble batch.

  After a crash, a recorded range is processed again, all of it, and ClickHouse drops the repeated insert. `indexedUpTo`, the held-row checks and the archive's share of `batchLock` are gone. A row ClickHouse can't hold (a time outside 1900–2299) is skipped and logged before the insert. The skip depends only on the row, so a replay skips the same rows. Every insert failure is retried with the same token.
- **Reads** keep their answers. Ties in time are now ordered by `uuid`, so paging and prev/next agree. Time bounds are compared with `toDateTime64` only. An error that isn't ClickHouse's own answers 503 "Events can't be read now: ClickHouse doesn't answer: …".
- **Cleanup** deletes the Project's rows (`DELETE FROM events WHERE project_id = ?`), then its Issues, then its Pebble Events, cursor and range.
- **Gone:**
  - in EventStore: `events_hot` reads and writes, the archive (`event_archive_bucket.go`), `pipeline/`, and the message queue reader, which only the archive read;
  - in Console: `EventStoreArchiveJob` and its schedule.

  `NewDuckDBWriter` lost its parquet path. An old `events.duckdb` is left as it is.

**Tests**
- Root:
  - new `events_reads_test.go`: the list, the count and the context; tied times paged once; 503 while the link to ClickHouse is cut;
  - new `events_cleanup_test.go`: a deleted Project's Events are gone;
  - `processing_test.go` reworked. `TestABatchStoredBeforeACrashIsStoredOnce` is the new replay test: a real crash after the insert, made by failing the seen counts and stopping. A mutation check showed that the replay tests fail with an unstable token.
- `handler` tests run on ClickHouse (a new `TestMain`). Also run: storage, config, processing, ingest and `go vet ./...`.
- Rails: `event_store_test`, `issues_controller_test`, `mcp/issue_tools_test`, `integration_notification_test`, `recurring_test` and `notification_cleanup_job_test`. 112 runs, 0 failures.

**Differs from the ticket or spec**
- **DuckDB SQL mode's `events`** is an empty view with the nine columns until 07. `logs_sql_scope_test.go` now expects no Events, and its `events_hot` WITH case is gone.
- **There's no one-row-at-a-time insert fallback**, since it can't be exactly-once.

**Not done**
- The Playwright flows (`issues`, `project-delete`) weren't run.
- `GLOSSARY.md` and the docs are left for 10.
