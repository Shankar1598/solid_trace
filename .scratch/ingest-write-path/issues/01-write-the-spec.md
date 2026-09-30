# 01: Write the spec for the ingest write-path change

Type: grilling
Status: resolved
Blocked by: none

**Source:** `review.md` §4, "Events acknowledged with 200 can vanish silently" ([review](../../prod-readiness/review.md)), and the design sessions of 2026-09-26 and 2026-09-27
**Area:** `event_store/ingest/`, `event_store/storage/pebble.go`, `event_store/storage/duckdb.go`, `event_store/pipeline/`, `event_store/main.go`, `event_store/config/config.go`

## Goal

Write `.scratch/ingest-write-path/spec.md` for the new ingest write path, then split it into implementation tickets numbered from `02` in this folder.

Today a `200` means only that the Event is in `pebbleChan`'s buffer. A restart, a crash, a full DuckDB channel or a DuckDB write error can lose an acknowledged Event, or leave it in Pebble but missing from every query.
The new design makes Pebble the durable record, written before the `200`. DuckDB becomes an index that catches up from Pebble.

## Settled

From the 2026-09-26 design session (Claude session `3849aaae`), unless noted:

- **Write before 200.** Each request writes its own Event to Pebble and replies `200` only after the commit. There is no batching: Pebble merges concurrent writers itself. `PebbleIngester`, `WriteBatch`, the Pebble batch size and the flush timer go away.
- **Mutex, not a channel with an ack.** The benchmarks showed the same throughput, and the mutex had a lower p50 and fewer parts.
- **`NoSync` stays.** A process crash or OOM kill is safe, because the write is in the page cache. A power loss can lose the last few milliseconds. `Sync` managed about 18k Events/s with a p50 of ~2 ms and was not adopted.
- **The UUID is generated under the lock**, so keys land in UUID order and a reader offset never skips an Event. The Sentry response doesn't need it. Accepted risk: the clock going backwards across a restart.
- **Overload answers 503**, not 429 (`bbfbaf1`). SDKs pause for 60 s on a 429.
- **Memtable size** is a separate task: [prod-readiness 011](../../prod-readiness/issues/011-pebble-memtable-size.md).
- **No migrations.** The project is not live, so key and value formats can change freely.
- **The target is the full design** (2026-09-27): the mutex writer, and a reader that brings DuckDB up to date from Pebble. Both are specified together and split into tickets.

Settled on 2026-09-27 (this ticket's grilling, round 1):

- **Mutex and UUID live in storage.** `PebbleWriter.WriteEvent(*models.Event)` takes the mutex, sets `EventUUID` and commits with `NoSync`. `ingest.Service` depends on an `EventWriter` interface declared in `ingest/repository.go`, and tests use a fake.
- **Waiting-request cap.**
  - An atomic counter in `Service`, counted at the start of `ingestEvent`, before the SQLite issue work. That way a rejected request never leaves an Issue with no Event.
  - The default is 10,000.
  - The setting `ingest_max_waiting` replaces `pebble_channel_size`, `pebble_batch_size` and `pebble_flush_timeout`.
  - There is no timeout on waiting for the lock.
- **Shutdown.**
  1. `ShutdownWithTimeout(5s)` on both Fiber apps.
  2. A `closed` flag set under the mutex. Any later write returns `ErrShuttingDown`, which maps to 503.
  3. The stores close through the existing defers.

  Docker's default stop grace period stays.
- **A Pebble write error returns 500**, with no retry under the lock.

Settled on 2026-09-27 (round 2):

- **Per-Project cursors, not an Event log.** The Event value stays the raw JSON, and `GetEvent` doesn't change. The reader reads each Project's range of Event keys forward from that Project's cursor.
- **The timestamp fallback is the UUIDv7's time**, not `time.Now()`, so a replay gives the same value.
  - A helper next to `KeyForEvent` (for example `storage.ReceivedAt(key)`) reads it.
  - A comment states that Event keys hold UUIDv7s whose time is the receive time, and a test pins it.
- **The cursor lives in Pebble, not DuckDB.** DuckDB is an OLAP store.
  - Key: reserved Project id `0`, then `cursor/`, then the Project id.
  - Value: the last UUID the reader handled.
  - It is written with `NoSync`.
- **Each reader batch runs in this order:**
  1. SQLite: find or create the Issues, and reopen them if resolved.
  2. DuckDB: append the rows in one transaction.
  3. Pebble: move the cursor.
- **Startup fast-forward.** A crash between steps 2 and 3 leaves DuckDB ahead of the cursor. At startup, the reader runs `SELECT max(uuid) FROM events_hot WHERE project_id = ? AND uuid > <cursor>` for each Project and moves the cursor to the result.
  - This runs before the reader loop and the archive job start.
  - It uses no deletes and creates no duplicate rows.

Settled on 2026-09-27 (round 3):

- **The request only stores the Event, and the reader does the rest.**
  - The request resolves the Project from the key, extracts the event item from the envelope, checks the JSON is valid (400 if not), writes the raw JSON to Pebble and replies `200`.
  - The reader parses the JSON, extracts the fields, classifies the Event, finds or creates the Issue, reopens it if resolved, writes to DuckDB and sends the Rails messages.
  - Consequences:
    - Round 1's placement of the waiting-request counter no longer matters.
    - The §5 race goes away, because a single reader creates Issues.
    - The `CONTEXT.md` definition of **Event ingest** ("owns the full Issue lifecycle at ingest time") must change.
- **Rails messages are sent at least once.** They are sent after the DuckDB commit and before the cursor moves. A crash between the two can resend them, so a duplicate "new Issue" notification is accepted.
- **An Event the reader can never process** is retried 3 times, then skipped and logged with its key. The raw JSON stays in Pebble. Transient SQLite or DuckDB errors keep the cursor and retry.
- **The backlog is made visible, not capped.** The reader logs how far behind each Project's cursor is. `/health` or metrics can show it later.
- **Reader wakeup and batch size.**
  - A write adds its Project to a "has new Events" set and signals a channel with room for one. A 1 s tick also wakes the reader.
  - Up to 10,000 Events per Project per batch.
  - The reader starts after the startup fast-forward and before the HTTP listeners, and stops after them. There is no new setting.

- **Known gap, accepted for now: `issue_created` can be lost after a crash.**
  - The reader creates the Issue in SQLite before the DuckDB commit. If the process crashes between the two (about 1 s per batch), the replay finds the Issue and treats the Event as not new.
  - The Issue and the Event are still stored, but no Integration is told about the new Issue.
  - The spec records this under known gaps, with the fixes considered:
    - `first_event_uuid` on `issues` (preferred if we revisit this)
    - a marker in Pebble keyed by fingerprint
    - asking DuckDB
    - a cursor in SQLite

Settled on 2026-09-27 (round 4):

- **Ticket split.**
  - **02 Mutex Event writer.**
    - `WriteEvent` takes the mutex and generates the UUID under the lock. The request replies `200` after the write.
    - It adds `ingest_max_waiting`, the `closed` flag and `ShutdownWithTimeout`.
    - It removes `pebbleChan`, `PebbleIngester` and the three Pebble batch settings.
    - The Issue work stays in the request for now, and the forward to `duckdbChan` stays.
    - Blocked by 01.
  - **03 Event processing.**
    - Processing cursors in Pebble, with the startup fast-forward.
    - Moves the Issue work out of the request, and uses the UUIDv7 time as the timestamp fallback.
    - DuckDB append, Rails messages sent at least once, skip after 3 retries, and a log line showing the backlog.
    - Removes `duckdbChan` and `DuckDBIngester`, and updates `CONTEXT.md`.
    - Blocked by 02.
  - **The side findings** go to prod-readiness as `needs-triage` tickets 013 (re-archiving overwrites `data.parquet`) and 014 (Rails messages stuck in `processing` are never retried).
- **Glossary.**
  - **Event ingest** is redefined: it authenticates the request, checks the payload, stores the Event in Pebble and acknowledges it.
  - New term **Event processing**: it works out the Issue, indexes the Event in DuckDB and tells the Console.
  - New term **processing cursor**: the per-Project position in Pebble.
  - **IssueRepository** becomes the seam of Event processing.

## Inputs

- The benchmark numbers are in [011](../../prod-readiness/issues/011-pebble-memtable-size.md).
- The harness `s7.go` was lost from disk. Its source and raw results survive in the 2026-09-26 session transcript (`3849aaae-9e35-4350-b874-61d2293d93be.jsonl`, near JSONL lines 1028 and 1066).
- The current write path: `handler/ingest.go` → `ingest/service.go:166-171` (non-blocking send) → `pipeline/pebble_ingester.go` → `pipeline/duckdb_ingester.go`. Nothing drains on shutdown (`main.go:104-111`), and nothing catches DuckDB up from Pebble at startup.

## Acceptance

- `spec.md` in this folder covers every settled and open point above, with each open point decided.
- Implementation tickets `02`… exist, each with a `Status:` line and a `Blocked by:` line.
- `review.md` §4 points to this effort.

## Answer

The spec is [spec.md](../spec.md), `ready-for-agent`. Tickets:
- [02 Mutex Event writer](02-mutex-event-writer.md)
- [03 Event processing](03-event-processing.md)
- Side findings filed as `needs-triage`:
  - [prod-readiness 013](../../prod-readiness/issues/013-archive-overwrites-parquet.md)
  - [prod-readiness 014](../../prod-readiness/issues/014-stuck-processing-messages.md)

`review.md` §4 now points here.

One decision was added while writing the spec: the archive and Event processing share a lock. The archive selects rows by the Event's `timestamp` date, so it could otherwise move a just-committed batch before the cursor moves, and the startup fast-forward would miss it.

## Comments

**2026-09-27, fact check: the DuckDB offset can commit with the rows.** This is no longer used, because the cursor moved to Pebble. It is kept in case a DuckDB transaction ever has to cover more than the appended rows.
Tested against the pinned DuckDB 2.0 alpha and duckdb-go `v2.20000.0-6.preview`.
The Appender works on the connection's own handle and never issues BEGIN or COMMIT itself, so it joins an explicit transaction on the same connection. Rows and the offset `UPDATE` then commit or roll back together, including when the process dies before COMMIT.

The pattern:
1. Pin one `*sql.Conn`.
2. Call `BeginTx`.
3. In `c.Raw`, run `NewAppenderFromConn`, append the rows and `Close` the appender (this flushes it) before COMMIT.
4. Run `tx.ExecContext` for the offset `UPDATE`.
5. Call `tx.Commit()`, with `defer tx.Rollback()` in place.

Caveats for the spec:
- **COMMIT on an aborted transaction returns nil but rolls back.** Check every step's error and never reach Commit after a failure.
- **Run every statement on the pinned connection.** A pooled `db.Exec` runs outside the transaction.
- **Keep one writer.** DuckDB's optimistic concurrency conflicts on the shared offset row.
