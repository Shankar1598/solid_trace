# M1-01: JSON Logs are stored and listed

**What to build:** the tracer bullet through Log ingest, Log processing and a first query. A shipper POSTs Logs to `POST /api/{project_id}/logs` and gets a `200` that means they are stored. Log processing moves them into the Project's hot table, and a minimal "latest N for a Project" operation on the query app returns them newest first. Spec: [Log ingest](../spec.md#log-ingest-eventstore) (Sources, Auth, The write), [Log processing](../spec.md#log-processing-eventstore), [Log storage](../spec.md#log-storage-eventstore) (Engine, Hot tables), [Testing Decisions](../spec.md#testing-decisions) (Seam 1).

Scope limits, so later tickets have room:
- Normalisation is minimal: the built-in fields from their primary key names only (`message`, `time`, `level`, `service`, `environment`, `trace_id`, `span_id`), a missing time becomes the receipt time, and top-level keys go to the typed maps by JSON type. Full normalisation is M1-03.
- No promotion (M1-07), no `logs_late` or export (M1-08). Every Log goes to the hot table.
- Body is identity only, under EventStore's current limits (M1-04).

**Blocked by:** None (can start immediately).

**Status:** resolved

- [x] The JSON route accepts a JSON array of objects, a single object, and NDJSON (`application/x-ndjson`); each object is one Log, and an element or line that fails to parse is dropped and counted.
- [x] Auth accepts the Project's public key in every `sentry_key=` form EventStore accepts today, and as `Authorization: Bearer <key>`; a key whose Project differs from the path's `project_id` gets `401`.
- [x] The response is `200` with counts of Logs accepted and dropped.
- [x] Each request's Logs are one Pebble Log batch, written through the same writer and mutex as Events with `NoSync`, in their own keyspace ordered per Project by UUIDv7 (each Log has a UUIDv7 id); Log batch keys never collide with Event keys or processing cursors. Logs share the `ingest_max_waiting` gate with Events.
- [x] Log processing is one worker separate from Event processing, woken by Log writes and a 1 s tick, reading each Project's batches in order from its Log processing cursor.
- [x] `logs.duckdb` is attached to the same DuckDB instance as `events.duckdb`. Per batch, one transaction appends to the Project's hot table (created with its first Log: `id`, the seven built-in fields, `attrs_string`, `attrs_number`, `attrs_bool`, `attrs_json`) and moves the cursor, which lives in `logs.duckdb`; then the Pebble batches up to the cursor are deleted.
- [x] A query-app operation returns the latest N Logs of a Project with every built-in field and Attribute.
- [x] A controllable clock is the one new substitution point in the EventStore test environment; tests drive Log processing with an explicit "process until caught up" step.
- [x] Tests: each body form; Bearer and `sentry_key` auth; the Project-mismatch `401`; a `200` followed by a restart before processing still yields the Logs; a crash between the DuckDB commit and the Pebble delete yields no duplicates; Event ingest and processing are unaffected.
- [x] `GLOSSARY.md` gains **Log ingest** and **Log processing**, defined like their Event counterparts, and **Processing cursor** is widened to cover Log processing.

## Comments

### 2026-10-02 built (logs-v1/m1-01)

What was built:
- **Clock:** `pkg/clock`. `clock.Clock` has one method, `Now()`. `clock.System{}` is the real clock. `clock.NewManual(t)` returns a test clock with `Set` and `Advance`. Log ingest takes each request's receipt time from it. Log processing doesn't use it yet. UUIDv7 ids still come from the real clock (google/uuid's clock is unexported).
- **Log ingest:**
  - `POST /api/:project_id/logs` is `IngestHandler.Logs`. The key comes from any `sentry_key=` form, or else `Authorization: Bearer`. A key for another Project, or a non-numeric `project_id`, gets `401`.
  - The handler calls `ingest.Service.IngestLogs(projectID, body, ndjson)`, which returns `ingest.LogCounts{Accepted, Dropped}`. The route answers that as JSON: `{"accepted": n, "dropped": m}`.
  - Normalisation is in `ingest/normalise.go` (`normaliseLog`). Each Log gets a UUIDv7 id in `IngestLogs`.
  - The new seam to storage is `ingest.LogWriter` (`WriteLogBatch(projectID, []models.Log)`). `PebbleWriter` implements it.
- **`ingest.NewService` now takes an `ingest.Config`:** Events, Logs, NotifyEvents, NotifyLogs, Clock and MaxWaiting. One Service handles both Events and Logs, so they share the `ingest_max_waiting` gate (`enterGate`/`leaveGate`). M1-05's envelope Logs can use the same Service.
- **The Log model:** `models.Log`, with `Attributes{String, Number, Bool, JSON}`, one map per hot-table map. The JSON maps hold JSON text. `Attributes.MarshalJSON` merges the four maps into one object with each value's own type.
- **Log batches in Pebble** (`storage/pebble_logs.go`):
  - Keys are `00000000` + `"logs/"` + Project id (4 bytes) + batch UUIDv7. The batch id is generated under the Event writer's mutex.
  - The value is a format byte (`1`) followed by msgpack of `[]models.Log`. The msgpack field tags on `models.Log` are the stored format, so don't rename them.
  - Methods: `WriteLogBatch`, `LogBatchesAfter(project, after, maxLogs)`, `DeleteLogBatchesUpTo` (a `DeleteRange`, NoSync) and `ProjectsWithLogBatches`.
- **logs.duckdb** (`storage/logs_duckdb.go`):
  - `storage.NewLogStore(duckdbWriter, path)` runs `ATTACH IF NOT EXISTS … AS logs` on the events DuckDB instance.
  - The cursor table is `logs.log_processing_cursors(project_id UINTEGER, batch_id UUID)`. It is updated with UPDATE-then-INSERT, not an upsert, with no key or index.
  - Hot tables are `logs.logs_hot_<project id>` with columns `id UUID, time TIMESTAMP, level, message, service, environment, trace_id, span_id VARCHAR, attrs_string MAP(VARCHAR,VARCHAR), attrs_number MAP(VARCHAR,DOUBLE), attrs_bool MAP(VARCHAR,BOOLEAN), attrs_json MAP(VARCHAR,VARCHAR)`. An absent built-in field is NULL; `message` is always a string.
  - `AppendLogBatches(project, batches)` runs in one transaction: `CREATE TABLE IF NOT EXISTS`, the append, and the cursor move.
  - `LatestLogs(project, limit)` orders by `time DESC, id DESC`.
  - Config: `logs_duckdb_path` sets the file. It defaults to `logs.duckdb` next to `duckdb_path`.
- **Log processing:** `logprocessing.Processor`, with `New`, `Prepare`, `Notify`, `Start`, `Stop` and `ProcessUntilCaughtUp`. It mirrors Event processing: a pending set, a wake channel and a 1 s tick.
  - Per Project, it reads the cursor, then the batches after it up to about 10,000 Logs (whole batches, at least one).
  - It commits them with `AppendLogBatches`, then deletes them from Pebble.
  - `Prepare` deletes the batches at or below each cursor, which a crash between commit and delete leaves behind. The cursor skips them anyway.
  - `main.go` wires it in, and stops it after `StopWrites`.
- **Query app:** `GET /api/:project_id/logs/latest?limit=N` returns `{"logs": [...]}`, newest first. N defaults to 100 and is capped at 10,000; an invalid N gets `400`. Each Log has `id`, `time` (RFC 3339, UTC), the built-in fields (absent ones omitted, `message` always present) and `attributes` (one object).
- **Test environment** (`integration_test.go`):
  - `env.Clock` is a `clock.Manual` starting at `testNow` (2026-10-02 12:00 UTC).
  - `env.ProcessLogs(t)` runs Log processing until it is caught up.
  - `env.Restart` also restarts Log processing.
  - New `env.Reopen(t)` closes every store and reopens it on the same files, with new processors and apps, as a real restart. `Cleanup` is now a method, so `defer env.Cleanup()` closes whatever is open at the end.
  - The Log tests are in `logs_test.go`.

Choices the ticket left open:
- **Minimal normalisation.** A built-in key whose value isn't a string (or, for `time`, isn't an RFC 3339 string) leaves the field empty and keeps the value as `original.<field>`, typed by its JSON value. A JSON `null` is treated as absent: a null Attribute is not stored. A number out of DOUBLE range is stored as a string of its text. Nested objects are not flattened; they go to `attrs_json` as compact JSON text, as arrays do. Level values are stored as sent. M1-03 replaces all of this.
- **Body parsing.** `application/x-ndjson` (parameters ignored) is always read as NDJSON. Any other body that is one valid JSON value is read as an object or an array. A body that isn't one valid JSON value is read as NDJSON whatever its content type, so NDJSON labelled `application/json` still works. Blank lines are skipped. Elements and lines that aren't JSON objects (including `null`, scalars and arrays) are dropped and counted. An empty body answers `{"accepted":0,"dropped":0}`, and nothing is written.
- **Compressed bodies get `415`.** A `Content-Encoding` other than identity is refused with `415`. The route reads `c.Request().Body()`, not `c.Body()`, so Fiber never inflates it without a cap. M1-04 replaces the `415` with capped gzip/zstd decoding.
- **A mismatched or non-numeric `project_id`** gets `401` after the key is validated. An unknown key is `401` and a lookup failure is `500`, as on the Event routes.
- **Processing batches combine Log batches.** One DuckDB transaction appends a Project's Log batches up to about 10,000 Logs and moves the cursor to the last of them, as Event processing works in batches of 10,000 Events. So "per batch, one transaction" holds for whole Log batches, several at a time.
- **One appender per transaction, not a long-lived one.** `AppendLogBatches` opens the hot table's appender inside the transaction and closes it before COMMIT, as Event processing does. That satisfies "reopened after any ALTER TABLE" trivially. M1-07 can open it after its ALTERs in the same transaction. If M1-10 shows the per-commit open cost matters, a long-lived appender per table can replace it.
- **Minimal error handling.** If a Project's commit fails, the error is logged, the Project stays pending with its cursor unmoved, the other Projects carry on, and the worker retries on the next wake or 1 s tick. `ProcessUntilCaughtUp` returns the first failure. There is no backoff, no content/store classification and no skipping: a poison Log retries every second forever. M1-06 replaces this.
- **No lock with the export yet.** Log processing has no lock. M1-08 needs one, shared with the hourly export, as `batchLock` is for Event processing and the archive.
- **Glossary.** `GLOSSARY.md` also gained **Log batch** (the spec uses the term) and two Relationships lines. The EventStore entry is unchanged.

Deviations: none from the spec.

Follow-ups for later tickets:
- **M1-03:** replace `normaliseLog`. `IngestLogs` passes it the request's receipt time in UTC, and `setAttribute` is the typed-map helper. Retention is `storage.ConsoleSettings.LogRetentionDays` (M1-02), not wired into `main.go` yet. Add `Truncated` to `ingest.LogCounts`.
- **M1-04:** `IngestHandler.Logs` reads the raw body and refuses non-identity encodings with `415`. Decode there with caps instead. `renderIngestError` maps `ErrOverloaded`/`ErrShuttingDown` to `503` for both Events and Logs, so `Retry-After` goes there.
- **M1-05:** `ingest.Service` already holds the LogWriter and the shared gate. The envelope path can normalise `log` items and call `s.logs.WriteLogBatch` and `s.notifyLogs` the same way.
- **M1-06:** `logprocessing.Processor.processProject` and `ProcessUntilCaughtUp` are the places for retries, isolation, skipping and the backlog line. The backlog line can use the injected clock (pass it to `logprocessing.New`). To skip one bad Log, the transaction must still move the cursor past its batch.
- **M1-07:** the promoted-set changes and ALTERs go in `appendLogBatches`, before the appender opens. The table must be created first, which `CREATE TABLE IF NOT EXISTS` already does there. `LatestLogs` must merge promoted columns back into the maps.
- **M1-08:** `LatestLogs` reads only the hot table today. Add the lock shared with the export (see above). Pass the clock to Log processing for the hot window.
- **Test speed:** each Seam 1 test pays a Rails `db:reset` (about 2.5 s). A Log-only env on a temp Console-shaped SQLite would be much faster as the Log suites grow.
- **UUIDv7 and clock skew:** a batch id generated after a restart with the system clock set back could sort at or below the cursor and never be processed. Event processing has the same exposure today.

### 2026-10-03 code review

- **Log batch ids** are now UUIDv7s of the receipt time from the injected clock. They are kept above every stored batch and every cursor, so a clock step back can't put a batch below a cursor. `WriteLogBatch` takes the receipt time. See the spec's 2026-10-03 code review entry, item 5.
