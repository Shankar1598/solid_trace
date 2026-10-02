# 02: Mutex Event writer: store the Event in Pebble before the 200

Status: resolved
Blocked by: 01

**Spec:** [spec.md](../spec.md), sections "Event ingest" and "The Pebble Event writer"
**Area:**
- `event_store/storage/pebble.go`
- `event_store/ingest/service.go`, `event_store/ingest/repository.go`
- `event_store/handler/ingest.go`
- `event_store/main.go`
- `event_store/config/config.go`, `deploy/event_store.yml`
- `event_store/pipeline/pebble_ingester.go`
- `event_store/integration_test.go`

## What to build

After this ticket, a `200` means the Event is in Pebble. The Issue work stays in the request for now, and the DuckDB side is unchanged. Ticket 03 moves both.

1. **Pebble writer.** Add a method that writes one Event.
   - It takes a process-wide mutex, sets `EventUUID` to a new UUIDv7 inside the lock, and commits one batch (the Event key and the raw JSON) with `pebble.NoSync`.
   - It returns `ErrShuttingDown` once the writer is closed.
   - Remove `WriteBatch`.
2. **Receive-time helper.** Add a storage helper next to `KeyForEvent` that returns an Event key's receive time: the UUIDv7's millisecond timestamp. Add a comment stating that Event keys hold UUIDv7s whose time is the receive time, and a test that pins it.

   Ticket 03 uses it for the timestamp fallback. Add it here, because this ticket moves UUID generation into storage.
3. **Event ingest uses an interface for the writer.**
   - Declare an `EventWriter` interface in `ingest/repository.go`, next to `IssueRepository`.
   - `Service` depends on it instead of `pebbleChan`.
   - Remove UUID generation from `ingestEvent`.
   - After a successful write, forward the Event to `duckdbChan` with the existing non-blocking send. Keep the "DuckDB channel full" log; ticket 03 removes that path.
4. **Waiting-request limit.**
   - Add an atomic counter in `Service` around the write. Past `ingest_max_waiting` (default 10,000), return `ErrOverloaded`, which already maps to `503`.
   - Count before the SQLite Issue work, so that a rejected request never leaves an Issue with no Event.
   - There is no timeout on waiting for the lock.
5. **Shutdown** in `main.go`:
   1. `ShutdownWithTimeout(5s)` on both Fiber apps.
   2. Close the writer, which sets the flag under the mutex.
   3. Close `duckdbChan`, so the DuckDB ingester flushes what it has.
   4. The existing defers close the stores.

   Map `ErrShuttingDown` to `503` in `renderIngestError`.
6. **Pebble write error.** Return `500`. Don't retry.
7. **Remove** `pipeline/pebble_ingester.go`, `pebbleChan`, and the `pebble_batch_size`, `pebble_flush_timeout` and `pebble_channel_size` settings. Add `ingest_max_waiting` to `config.go`, and add it, commented, to `deploy/event_store.yml`.

## Acceptance

- An Event is readable by UUID through the query app as soon as its `200` returns, with no sleep.
- When `ingest_max_waiting` requests are already waiting, the next request gets `503`. Test this with a writer fake that blocks.
- After the writer closes, ingest returns `503`, and nothing is written after Pebble closes.
- A Pebble write error gives `500`.
- Concurrent writes produce keys in UUID order within a Project.
- The receive-time helper returns the UUID's millisecond time.
- `integration_test.go` no longer creates `pebbleChan` or a `PebbleIngester`. Its DuckDB wait can stay until 03.
- `go test ./...` passes.

## Comments

**2026-09-30, resolved** in merge `88f14f3` (branch `ingest-mutex-writer`).

- **Naming:**
  - The "close the writer" step is `PebbleWriter.StopWrites()`. `Close()` also stops writes before it closes the DB.
  - `ErrShuttingDown` is `storage.ErrShuttingDown`, so `storage` doesn't import `ingest`. The handler maps it to `503` alongside `ErrOverloaded`.
- **Beyond the brief:**
  - The two HTTP shutdowns run concurrently, so the worst case stays within Docker's 10 s stop grace period.
  - `main` waits for the DuckDB ingester to flush before the deferred store closes.
- **Accepted edge cases, until 03:**
  - If the ingest app's shutdown times out, `duckdbChan` is not closed, because a request still in flight could send on it and panic. That DuckDB buffer is then not flushed. Its Events are in Pebble.
  - A request that arrives during shutdown still does its Issue work before its write is refused, so it can leave an Issue with no Event. 03 removes the Issue work from the request.
- **Not validated:** an `ingest_max_waiting` of 0 or below rejects every request.
- **Tests:** the integration test learns the assigned UUID by wrapping the writer in a recording `EventWriter`, because the ingest response doesn't carry it.
