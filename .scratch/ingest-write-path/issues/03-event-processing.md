# 03: Event processing: bring Issues, DuckDB and the Console up to date from Pebble

Status: ready-for-agent
Blocked by: 02

**Spec:** [spec.md](../spec.md), sections "Event ingest", "Event processing", "Removed", "Glossary" and "Testing Decisions"
**Area:**
- `event_store/ingest/`
- `event_store/pipeline/duckdb_ingester.go`
- `event_store/storage/pebble.go`, `event_store/storage/duckdb.go`
- `event_store/pipeline/archive_consumer.go`
- `event_store/main.go`
- `event_store/config/config.go`, `deploy/event_store.yml`
- `event_store/integration_test.go`
- `CONTEXT.md`

## What to build

1. **Event ingest stops doing Issue work.** The request:
   1. resolves the Project,
   2. extracts the event item,
   3. checks `json.Valid` (`400` if not),
   4. calls the Pebble writer from 02,
   5. replies `200`.

   Classification, find-or-create, reopen and field extraction move to Event processing. Move the waiting-request counter so it wraps only the write. Remove `duckdbChan` from the request.
2. **Processing cursors in Pebble.**
   - Key: reserved Project id `0`, then `cursor/`, then the Project id.
   - Value: the last UUID handled.
   - Written with `NoSync`.
   - Add Pebble reads for one Project's range after a UUID, a cursor read and write, and a skip-scan over the Project prefixes that have Event keys.
3. **The Event processing worker** is one goroutine. It replaces `DuckDBIngester`.
   - **Wakeup.** A write adds its Project to a "has new Events" set and signals a channel with room for one. A 1 s tick also wakes the worker.
   - **Each batch** covers up to 10,000 Events of one Project, in this order:
     1. Parse each payload and extract the fields. The timestamp is the payload's `timestamp`, else the receive time from 02's helper. Classify the Event, then run find-or-create and reopen through `IssueRepository`.
     2. Append the rows to DuckDB in one transaction.
     3. Enqueue `issue_created` and `issue_received_event` using today's grouping rules.
     4. Move the cursor.
   - **Store errors** (SQLite, DuckDB, message queue, Pebble) are retried with backoff and never skip an Event. An enqueue error is now a store error; today it's only logged.
   - **Content errors** are retried 3 times, then the Event is skipped and logged with its key. If the DuckDB append fails, retry the batch one Event at a time to isolate the bad row.
   - **Backlog log.** Periodically log each Project whose oldest unprocessed Event is older than a threshold, with how far behind it is.
4. **Startup**, before the worker and the archive consumer start: for each Project, run `SELECT max(uuid) FROM events_hot WHERE project_id = ? AND uuid > <cursor>`. If it returns a UUID, move the cursor to it. Then seed the "has new Events" set with every Project.
5. **Archive lock.** The archive consumer and the worker share a lock. The archive only runs between batches, never between a batch's DuckDB commit and its cursor move. The spec explains why: the archive selects rows by the Event's own `timestamp` date.
6. **Shutdown order:**
   1. Stop the HTTP apps.
   2. Close the writer.
   3. Stop the worker. Abandoning a batch in progress is safe.
   4. Stop the archive consumer.
   5. Close the stores.
7. **Remove** `pipeline/duckdb_ingester.go`, `duckdbChan`, and the `duckdb_channel_size` and `duckdb_flush_timeout` settings.
8. **`CONTEXT.md`.**
   - Redefine **Event ingest**.
   - Add **Event processing** and **Processing cursor**.
   - Point **IssueRepository** at Event processing.

   The wording is in the spec's Glossary section.

## Tests

Extend the environment in `integration_test.go`. Replace `time.Sleep` with an explicit "process until caught up" step. The only fake is **IssueRepository**, used to inject transient errors. Cover:

- **Basic ingest:**
  - An Event is readable by UUID before processing, and appears in queries after processing.
  - Events stored, then followed by a restart (a new worker on the same stores), are indexed.
- **Crash states:**
  - DuckDB rows past the cursor: the fast-forward runs, and no rows are duplicated.
  - Events in Pebble with no cursor: they're caught up.
  - An Issue created with the cursor still behind: this shows the accepted gap. The replayed Event is not new.
- **Issue behaviour:**
  - The timestamp falls back to the receive time.
  - A resolved Issue reopens.
  - Concurrent first Events for one fingerprint produce one Issue.
  - `issue_created` and `issue_received_event` land in the message queue.
- **Errors:**
  - A poison Event (`[]`) is skipped, and later Events of its Project are processed.
  - A transient `IssueRepository` error delays processing but skips nothing.

Move the Issue-work cases from `ingest/service_test.go` to this seam. Keep the envelope parsing tests.

## Acceptance

- Every case above passes under `go test ./...`, with no sleeps in the pipeline tests.
- `main.go` has no ingest channels.
- `deploy/event_store.yml` and `config.go` have no removed settings.
- `CONTEXT.md` is updated.

## Comments
