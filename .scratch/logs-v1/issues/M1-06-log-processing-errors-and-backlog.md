# M1-06: Log processing errors and backlog

**What to build:** Log processing survives bad Logs and store failures as Event processing does, and the operator can see when it is behind. Spec: [Log processing](../spec.md#log-processing-eventstore) (Errors); user stories 35, 38.

**Blocked by:** M1-01.

**Status:** ready-for-agent

- [x] A store error (DuckDB unavailable, disk full) retries with backoff and skips nothing; the cursor doesn't move.
- [x] A batch that fails for its content is retried, then isolated Log by Log; a Log that can never be appended is skipped and logged, and the rest of the batch is stored. Classify errors the way Event processing does (ingest-write-path 04).
- [x] A periodic log line names Projects whose oldest unprocessed Log batch is older than a threshold.
- [x] A stuck or failing Log Project doesn't stall other Projects' Logs or Event processing.
- [x] Tests: a bad Log in a batch is skipped and its neighbours are queryable; a store failure followed by recovery loses nothing; the backlog line appears for a lagging Project.

## Comments

### 2026-10-02 built (logs-v1/m1-06)

What was built (`event_store/logprocessing/processor.go` unless named):
- **Retry per Project, with backoff.** A Project whose Logs fail to store stays pending with a retry time: 100 ms, doubling to 30 s, in real time. `ProcessUntilCaughtUp` processes the other Projects meanwhile. When only waiting Projects are left, it sleeps until the earliest retry or a wake. It now returns only `ErrStopped`, as Event processing's does; M1-01's returned the first failure. So `env.ProcessLogs` blocks while a store stays broken; the new tests use timeouts.
- **Classification** (`classify`). A failure is a content error if logs.duckdb still answers (`LogStore.Ping` reads a logs table) and DuckDB didn't type the error as IO, out of memory, fatal or a transaction conflict. Anything else is a store error. Store errors are retried until they clear and skip nothing. Nothing commits, so the cursor stays where it was.
- **Content errors.** After 3 content retries (about 0.7 s), `storeApart` takes over:
  1. It finds the Logs that fail on their own by trying halves, down to single Logs. Each try is `LogStore.TryLogBatches`: the whole commit (promotion, append, counts, cursor), then a rollback.
  2. It commits the rest with the cursor in one transaction. A batch that the skipped Logs leave empty still moves the cursor.
  3. It logs each skipped Log: `Log processing skipped a Log` with `project_id`, `batch_id`, `log_id` and `error`.
- **Backlog line.** Every minute by the injected clock, Log processing logs `Log processing is behind` with `project_id` and `behind` for each Project whose oldest Log batch in Pebble was written more than a minute before now. It reads Pebble alone (`PebbleWriter.OldestLogBatches`, `storage.LogBatchWrittenAt`). So it reports while DuckDB is down, and it covers every Project with batches, not only those the worker has taken. That avoids Event processing's gap (ingest-write-path 05).
- **Seam.** `logprocessing.Store` (`LogProcessingCursor`, `AppendLogBatches`, `TryLogBatches`, `Ping`), with `*storage.LogStore` as the adapter. In the test env, `env.FlakyLogs` wraps the real LogStore as `flakyIssueRepository` wraps SQLite. `fail(err, projects...)` fails commits as a full disk does, while tries still work. `goDown()` fails every call, Ping included. `recover()` ends both.
- **Tests** are in `logs_processing_test.go`:
  - bad Logs are skipped and their neighbours stored, also across a restart;
  - a lone bad Log doesn't hold up later Logs;
  - DuckDB unavailable and a full disk each lose nothing and are reported as store errors;
  - a failing Project holds up neither other Projects nor Event processing;
  - the backlog line names the lagging Project and not the caught-up one.

Choices the ticket left open:
- **Typed store errors, beyond Event processing's `Ping` alone.** The ticket names "disk full" as a store error. With `Ping` alone, a full disk reads as content, because `SELECT 1` still works. Event processing is unchanged; it could reuse `storeFailure`.
- **Halving, not one try per Log.** With k bad Logs among n, the search costs about 2k·log2(n) rolled-back tries. A commit in which every Log is bad costs about 2n tries, all in one worker turn, and other Projects wait for it. That should be rare: normalisation already stops what DuckDB rejects. The tests plant a bad Log, with a time in the year 300000, straight into Pebble with `WriteLogBatch`.
- **No retries per Log.** The commit has already failed 4 times over about 0.7 s, so a Log that fails alone in a try is skipped. If the search finds no Log at fault (the Logs fail only together), the commit keeps retrying with backoff and nothing is skipped.
- **A skipped Log is gone.** Its batch is deleted from Pebble with the others, so only the log line is left. Event processing keeps a skipped Event's payload in Pebble. The Log spec deletes batches up to the cursor, so the Log path doesn't.
- **An unreadable Log batch** (a Pebble read or decode error) counts as a store error. The Project retries forever: isolated, logged, and named by the backlog line. It isn't skipped, because deleting it would lose Logs a later version might read.
- **Noise.** Every failed try logs one line per Project, so up to 2 a minute per Project at the 30 s cap. A DuckDB outage with many Projects is loud.
- **Backlog false positive.** If deleting a batch after its commit fails (already logged as a warning), the batch counts as waiting. That lasts until the Project's next commit, or a restart, deletes it.

Deviations: the typed store errors above, and the `logprocessing.Store` seam. The spec names the clock as the one new substitution point, but the ticket's store-failure test needs a way to break the store.

Follow-ups for later tickets:
- **M1-08:**
  - Take the export lock inside `storeLogs` and `storeApart`, around the commits and tries. Never hold it across `waitUntil`, or the export waits out a backoff.
  - `TryLogBatches` runs the same `appendLogBatches`, so routing to `logs_late` applies to tries too. Keep it that way, so a try fails exactly as the commit would.
  - Add any new store method the processor calls to `logprocessing.Store` and to `flakyLogStore`.
- **M1-09:** deleting a Project's Log batches is enough for Log processing. If the Project was waiting to be retried, its next try finds no batches and drops the failure.

