# Ingest write path: store the Event before acknowledging it

Status: ready-for-agent

**Origin:** `review.md` §4, "Events acknowledged with 200 can vanish silently" ([review](../prod-readiness/review.md)). Designed in Claude sessions on 2026-09-26 and 2026-09-27. Every decision and the reasoning behind it is in [ticket 01](issues/01-write-the-spec.md).

## Problem Statement

A Sentry SDK sends an Event to EventStore and gets a `200`. The SDK then forgets the Event: SDKs never resend after a `200`, and they drop an Event on any other status. So the `200` is a promise that SolidTrace has the Event. Today EventStore breaks that promise in four ways:

- **A restart loses Events.** The `200` only means the Event is in an in-memory buffer. On shutdown nothing drains that buffer or the DuckDB buffer behind it. A routine deploy can discard up to 150,000 acknowledged Events.
- **A crash or OOM kill loses Events**, in the same way.
- **A full DuckDB buffer loses Events from every query.** The Event is in Pebble, but it never reaches DuckDB. It never appears in an Issue's Event list or counts, and the Console is never told about it.
- **A failed DuckDB write discards the whole batch**, with no retry.

Nobody sees any of this. The operator of a self-hosted SolidTrace has no way to know that Events went missing, and an Organization can miss a new Issue entirely.

The request also does too much. Each request classifies the Event and finds or creates its Issue in SQLite before replying. Concurrent requests race on new fingerprints (review §5). A request rejected for overload can leave behind an Issue with no Event.

## Solution

Pebble becomes the durable record of every Event, and a `200` means the Event is in Pebble.

- **Event ingest** does the minimum: it resolves the Project from the key, checks the payload, writes the raw Event to Pebble, and replies `200` only after the write.
- **Event processing** is a single background worker. It reads each Project's new Events from Pebble in order, starting at that Project's **processing cursor**. For each batch it:
  1. works out the Issue,
  2. indexes the Events in DuckDB,
  3. tells the Console,
  4. then moves the cursor.

  A crash, restart, DuckDB error or slow DuckDB never loses an Event. Event processing picks up where its cursor stopped, and the same loop handles both startup catch-up and steady state.

For the operator:
- a restart or crash loses nothing that was acknowledged,
- a backlog is visible in the logs,
- DuckDB and the Console catch up by themselves.

For an SDK, a `200` now means the Event is stored.

## User Stories

1. As a developer whose application reports errors, I want a `200` from SolidTrace to mean the Event is stored, so that an error my SDK reported is never silently lost.
2. As an operator, I want to restart EventStore for a deploy without losing acknowledged Events, so that routine operations are safe.
3. As an operator, I want an EventStore crash or OOM kill to lose no acknowledged Event, so that a process failure doesn't become data loss.
4. As an operator, I want to know that a power loss or kernel crash can lose the last few milliseconds of acknowledged Events, so that I understand the durability I'm getting.
5. As an operator, I want every stored Event to appear eventually in its Issue's Event list and counts, so that Pebble and DuckDB never disagree for good.
6. As an operator, I want DuckDB to catch up by itself after a DuckDB error, so that I don't have to replay anything by hand.
7. As an operator, I want DuckDB to catch up by itself after a restart, so that Events acknowledged just before the restart are indexed after it.
8. As an operator, I want a crash between the DuckDB write and the processing cursor update to cause no duplicate Events in DuckDB, so that Event counts stay right.
9. As an operator, I want EventStore to answer `503` when too many requests are waiting to be stored, so that SDKs drop only the Events over the limit and keep sending.
10. As an operator, I want overload to never answer `429`, so that SDKs don't pause all sending for 60 seconds after a short spike.
11. As an operator, I want requests that arrive during shutdown to get `503`, so that no write lands after the stores close.
12. As an operator, I want a failed Pebble write to answer `500`, so that the fault is reported as ours.
13. As an operator, I want a log line whenever a Project's Events are waiting to be processed for too long, so that I can see a backlog.
14. As an operator, I want ingest to keep accepting Events while Event processing is behind, so that a slow DuckDB doesn't turn into rejected Events.
15. As an operator, I want an Event that Event processing can never handle to be skipped and logged with its key, so that one bad payload doesn't block the rest of its Project.
16. As an operator, I want a skipped Event's raw payload to stay in Pebble, so that I can inspect it later.
17. As an operator, I want transient SQLite or DuckDB errors to be retried without skipping any Event, so that a busy database doesn't lose Events.
18. As an operator, I want the settings for the old in-memory buffers removed, so that the configuration only contains what still has an effect.
19. As an operator, I want one setting for the waiting-request limit, so that I can size it for my hardware.
20. As a developer whose SDK sends an invalid JSON payload, I want a `400`, so that the error is reported as mine.
21. As a developer whose SDK sends an unknown key, I want a `401`, as today.
22. As a developer whose SDK sends a transaction or other non-Event item, I want it accepted and ignored, as today.
23. As an Organization member, I want an Event to reach the Issue it belongs to within about a second in normal operation, so that the Console stays current.
24. As an Organization member, I want a resolved Issue to reopen when a new Event arrives for it, as today.
25. As an Organization admin, I want a new Issue to trigger an `issue_created` notification to the Console, as today.
26. As an Organization admin, I want an Event on an existing Issue to trigger `issue_received_event` to the Console, as today, so that threshold rules keep working.
27. As an Organization admin, I want Console messages to be sent even if EventStore crashes right after writing to DuckDB. A rare duplicate is acceptable, but a missing message is not.
28. As an Organization member, I want an Event's timestamp to be the SDK's `timestamp` when present, and the time SolidTrace received it otherwise, so that Events without a timestamp still sort correctly.
29. As an Organization member, I want to open an Event's raw payload as soon as the SDK gets its `200`, even before Event processing has run, so that a freshly reported error can be inspected at once.
30. As an Organization member, I want two concurrent first Events for the same new fingerprint to produce one Issue, so that one failure is never split in two.
31. As a SolidTrace maintainer, I want the request path to do no SQLite Issue work, so that the Issue lifecycle lives in one place and can't race.
32. As a SolidTrace maintainer, I want Event processing to derive everything from the stored raw payload, so that a replay gives the same result as the first run.
33. As a SolidTrace maintainer, I want Events stored in UUID order under one lock, so that a processing cursor can never skip an Event.
34. As a SolidTrace maintainer, I want the processing cursor stored in Pebble, not DuckDB, so that DuckDB is only used for analytical queries.
35. As a SolidTrace maintainer, I want startup to fast-forward each processing cursor to the newest Event DuckDB already holds, so that a crash never causes a batch to be appended twice.
36. As a SolidTrace maintainer, I want the integration tests to drive Event processing step by step instead of sleeping, so that they are deterministic.
37. As a SolidTrace maintainer, I want `CONTEXT.md` to describe Event ingest and Event processing as they are, so that the glossary matches the code.
38. As a SolidTrace maintainer, I want the accepted gap in new-Issue notification written down, so that it can be fixed deliberately later.

## Implementation Decisions

### Event ingest

- **Steps.** Event ingest:
  1. authenticates the key and resolves the Project,
  2. extracts the event item from a store body or an envelope,
  3. checks that the payload is valid JSON (`400` if not),
  4. writes it, then replies `200`.

  Event ingest does no classification or Issue work, and doesn't parse the payload beyond checking that it is valid JSON.
- **Envelope handling doesn't change.** The first `event` item is stored. Transactions and other items get a `200` and are not stored.
- **Status codes.**

  | Response | When |
  |---|---|
  | `400` | Invalid envelope, invalid item header or invalid JSON |
  | `401` | Missing or unknown key |
  | `500` | Key lookup error or Pebble write error |
  | `503` | Overload or shutting down |

  A Pebble write is never retried under the lock.

### The Pebble Event writer

- **One writer.** The Pebble storage adapter exposes a write for one Event. Event ingest depends on it through a small interface declared next to **IssueRepository**, and tests can fake it.
- **The lock.** The write takes one process-wide mutex, generates the Event's UUIDv7, and commits one Pebble batch with `NoSync` inside the lock. Because the UUID is generated inside the lock, Event keys are committed in UUID order within each Project. Accepted risk: the clock going backwards across a restart.
- **No batching.** Each request writes its own Event, and Pebble merges concurrent writers itself. The benchmarks in [prod-readiness 011](../prod-readiness/issues/011-pebble-memtable-size.md) showed the mutex costs nothing measurable, and a channel with an ack gave the same throughput with more parts.
- **Durability.** `NoSync` stays. A process crash or OOM kill loses nothing, because the write is in the page cache. A power loss or kernel crash can lose the last few milliseconds.
- **Key and value.**
  - The Event key is unchanged: the 4-byte big-endian Project id, then the 16-byte UUID.
  - The value is the raw payload exactly as received.
  - Reading an Event by UUID doesn't change.
- **Receive time.** A storage helper returns an Event key's receive time: the millisecond timestamp inside its UUIDv7. A comment and a test pin that Event keys hold UUIDv7s whose time is the receive time.
- **Waiting-request limit.** An atomic counter tracks requests waiting for the write. Past the limit, the request returns overloaded, which maps to `503`. The setting is `ingest_max_waiting`, with a default of 10,000: about half a second of waiting at the default memtable's saturation rate. There is no timeout on waiting for the lock.
- **Shutdown.**
  1. Both HTTP apps stop with a 5 s timeout.
  2. The writer is closed under the mutex. Any later write returns shutting-down, which maps to `503`, so no write can reach Pebble after it closes.
  3. Event processing stops. Abandoning a batch in progress is safe.
  4. The stores close.

  Docker's default stop grace period is enough.

### Event processing

- **What it is.** One worker goroutine. It is the only place that classifies Events, finds or creates Issues, reopens resolved Issues, writes DuckDB rows and sends Console messages. It owns the **IssueRepository** seam that Event ingest owns today.
- **Processing cursor.**
  - One per Project, stored in Pebble.
  - Key: the reserved Project id `0`, a `cursor/` marker, then the Project id. Rails never assigns Project id `0`.
  - Value: the UUID of the last Event handled.
  - It is written with `NoSync`. The startup fast-forward corrects a cursor that a power loss rolled back.
- **Reading.** For a Project, Event processing seeks to the Project's key prefix plus its cursor UUID and reads forward. A Project with no cursor starts at the beginning of its key range. Up to 10,000 Events make one batch.
- **Each batch, in order:**
  1. **SQLite.** Parse each payload and extract environment, server name, release, level and tags. The timestamp is the payload's `timestamp` if present, otherwise the key's receive time. Classify the Event, find or create its Issue, and reopen the Issue if it's resolved. The existing find-or-create with its conflict handling is reused.
  2. **DuckDB.** Append the batch's rows in one transaction. Rows are unchanged.
  3. **Console messages.** Enqueue `issue_created` for Issues created in this batch and `issue_received_event` for other Issues that got Events, following today's per-batch grouping rules.
  4. **Pebble.** Move the Project's processing cursor to the batch's last Event.

  Messages are sent before the cursor moves, so each is sent at least once. A crash between steps 3 and 4 resends them.
- **Startup fast-forward.** A crash between the DuckDB commit and the cursor update leaves DuckDB ahead of the cursor. A DuckDB transaction commits all of its rows or none, so the newest UUID in DuckDB's hot table for a Project shows exactly how far that Project's DuckDB writes got. At startup, before the processing loop and the archive job start, each Project's cursor moves to the newest UUID in the hot table that is past it, if there is one. This uses no deletes and creates no duplicates.
- **The archive never runs inside a batch.** The archive job runs whenever the Console sends `archive_events`. It moves rows out of the hot table by the Event's own `timestamp` date, not by UUID, so it can move rows from a batch that has just committed. If it did that between a batch's DuckDB commit and its cursor update, and EventStore then crashed, the fast-forward wouldn't see those rows and the batch would be appended twice.

  So the archive and Event processing share a lock, and the archive only runs between batches, after the cursor has moved.
- **Finding work.**
  - At startup, Event processing finds every Project that has Event keys by skipping from one key prefix to the next.
  - After that, each successful write adds its Project to a "has new Events" set and signals a channel with room for one signal. That signal wakes Event processing, and so does a 1 s tick.
- **Errors.**
  - **Store errors** keep the cursor and are retried with backoff, indefinitely. These are SQLite, DuckDB, message-queue or Pebble errors. The worker logs each one, and no Event is skipped.
  - **Content errors** mean an Event can't be handled because of what it contains, for example a payload that is valid JSON but not an Event object. Event processing retries that Event 3 times, then skips it and logs its key. The raw payload stays in Pebble.

    If one row makes the DuckDB append fail, Event processing retries the batch one Event at a time to isolate that row.
- **Backlog.** Ingest no longer slows down when Event processing is behind. Event processing periodically logs each Project whose oldest unprocessed Event is older than a threshold, along with how far behind it is. There is no cap.

### Removed

- The Pebble channel, the Pebble ingester and its batch writer.
- The DuckDB channel and the DuckDB ingester.
- Settings `pebble_batch_size`, `pebble_flush_timeout`, `pebble_channel_size`, `duckdb_channel_size` and `duckdb_flush_timeout`.
- The request-side Issue work and its tests. The rules move to Event processing.

### Glossary (`CONTEXT.md`)

- **Event ingest** is redefined: the EventStore module that authenticates an incoming payload, stores it as an Event in Pebble and acknowledges it.
- New term **Event processing**: the EventStore module that turns stored Events into Issues, indexes them in DuckDB and tells the Console. It owns the Issue lifecycle: classification, find-or-create and reopen-on-resolved.
- New term **Processing cursor**: a Project's position in Pebble, up to which Event processing has handled Events.
- **IssueRepository** becomes the seam between Event processing and Issue persistence.

## Testing Decisions

- **A good test here** sends requests through the ingest HTTP app and checks what an outside observer sees:
  - status codes,
  - what the query app returns,
  - what reading an Event by UUID returns,
  - what is in the message queue.

  It doesn't assert on channels, internal counters or call order.
- **One seam: the EventStore test environment.** It is extended from the existing integration tests:
  - Real Pebble, DuckDB, SQLite and message queue in temp dirs.
  - The ingest HTTP app and the query app.
  - Event processing driven by an explicit "process until caught up" step instead of sleeps.
- **Restarts and crashes.** A restart is a new Event processing started on the same stores. Crash states are prepared by writing store state directly, then starting Event processing:
  - DuckDB rows past the cursor, to test the fast-forward with no duplicates.
  - Events in Pebble with no cursor, to test catch-up.
  - An Issue created with the cursor still behind, to document the accepted gap.
- **The only substitution point** is the existing **IssueRepository** interface. A fake injects transient errors to test retry without skipping. Poison Events need no fake: a valid JSON payload that isn't an Event object passes ingest and fails processing.
- **Cases to cover:**
  - An Event is readable by UUID straight after its `200`, before any processing step.
  - An Event appears in queries after a processing step.
  - Events stored before a restart are indexed after it.
  - Overload returns `503`.
  - A write after shutdown returns `503`.
  - Invalid JSON returns `400`.
  - The timestamp falls back to the receive time.
  - A resolved Issue reopens.
  - Concurrent first Events for one fingerprint produce one Issue.
  - `issue_created` and `issue_received_event` are enqueued as today.
  - A poison Event is skipped, and later Events of its Project are processed.
  - A store error delays processing but skips nothing.
- **Unit tests stay where they are.** The handler's error-to-status mapping and the envelope parsing tests stay as they are. The receive-time helper gets a small storage test next to the existing key test.
- **Prior art:**
  - the integration tests' environment setup and store and envelope ingestion tests,
  - the handler test showing an Event is readable before DuckDB has it,
  - the storage tests for concurrent find-or-create and for the Event key,
  - the service tests' fake **IssueRepository**.

## Out of Scope

- **Pebble memtable size.** Tracked in [prod-readiness 011](../prod-readiness/issues/011-pebble-memtable-size.md). The default stays until 011 recommends a size.
- **`Sync` writes.** Not adopted: about 18k Events/s with a p50 of about 2 ms.
- **Exactly-once Console messages.** A crash can resend a message. Threshold alerts already de-duplicate per the [integration-notification spec](../integration-notification/spec.md).
- **A backlog cap, and backlog metrics or `/health` fields.**
- **Replacing a client timestamp that is far in the future or past with the receive time.** It is now possible, but not planned.
- **Storing transactions and other non-Event envelope items.**
- **Two defects found while designing this.** They are filed separately:
  - [prod-readiness 013](../prod-readiness/issues/013-archive-overwrites-parquet.md): re-archiving overwrites a day's Parquet file.
  - [prod-readiness 014](../prod-readiness/issues/014-stuck-processing-messages.md): Console messages stuck in `processing` after a crash are never retried.
- **`CountEvents` ignoring the time window.** Covered by the integration-notification spec.
- **Data migration.** The project is not live, and existing dev data can be wiped.

## Further Notes

- **Known gap, accepted for now: a new-Issue notification can be lost after a crash.**
  - Event processing creates the Issue in SQLite before the DuckDB commit, and the two can't commit together. If EventStore crashes between them (a window of about one batch, normally under a second), the replay finds the Issue already there and treats the Event as not new.
  - The Issue and the Event are stored, but the Console gets `issue_received_event` instead of `issue_created`, so no Integration is told about the new Issue.
  - Fixes considered, if this is revisited:
    - **(a) A `first_event_uuid` column on the Console's `issues` table**, written in the same insert that creates the Issue. This is exact, and it also gives the Console a "first seen" Event. It is preferred.
    - **(b)** A Pebble marker keyed by fingerprint, written before the Issue is created.
    - **(c)** Asking DuckDB whether the Issue has any Events yet. This gets slower as the archive grows.
    - **(d)** A second processing cursor in SQLite, committed with the Issue.
- **Why there is no Event log.** A separate log keyspace with metadata values was considered. Per-Project cursors over the existing Event keys were chosen because Event processing derives everything from the raw payload. The only value that can't be derived again after a crash is whether the Event created a new Issue, which is the gap above.
- **Why the cursor isn't in DuckDB.** A cursor committed with the DuckDB rows would make the fast-forward unnecessary, and a fact check showed the duckdb-go Appender can join such a transaction. It was rejected to keep DuckDB for analytical work only.
- **Benchmarks.** The benchmark numbers are in [prod-readiness 011](../prod-readiness/issues/011-pebble-memtable-size.md). The harness source survives only in the 2026-09-26 session transcript.
- **Tickets:**
  - [02 Mutex Event writer](issues/02-mutex-event-writer.md)
  - [03 Event processing](issues/03-event-processing.md)
