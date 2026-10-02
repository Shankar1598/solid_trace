# M1-01: JSON Logs are stored and listed

**What to build:** the tracer bullet through Log ingest, Log processing and a first query. A shipper POSTs Logs to `POST /api/{project_id}/logs` and gets a `200` that means they are stored. Log processing moves them into the Project's hot table, and a minimal "latest N for a Project" operation on the query app returns them newest first. Spec: [Log ingest](../spec.md#log-ingest-eventstore) (Sources, Auth, The write), [Log processing](../spec.md#log-processing-eventstore), [Log storage](../spec.md#log-storage-eventstore) (Engine, Hot tables), [Testing Decisions](../spec.md#testing-decisions) (Seam 1).

Scope limits, so later tickets have room:
- Normalisation is minimal: the built-in fields from their primary key names only (`message`, `time`, `level`, `service`, `environment`, `trace_id`, `span_id`), a missing time becomes the receipt time, and top-level keys go to the typed maps by JSON type. Full normalisation is M1-03.
- No promotion (M1-07), no `logs_late` or export (M1-08). Every Log goes to the hot table.
- Body is identity only, under EventStore's current limits (M1-04).

**Blocked by:** None (can start immediately).

**Status:** ready-for-agent

- [ ] The JSON route accepts a JSON array of objects, a single object, and NDJSON (`application/x-ndjson`); each object is one Log, and an element or line that fails to parse is dropped and counted.
- [ ] Auth accepts the Project's public key in every `sentry_key=` form EventStore accepts today, and as `Authorization: Bearer <key>`; a key whose Project differs from the path's `project_id` gets `401`.
- [ ] The response is `200` with counts of Logs accepted and dropped.
- [ ] Each request's Logs are one Pebble Log batch, written through the same writer and mutex as Events with `NoSync`, in their own keyspace ordered per Project by UUIDv7 (each Log has a UUIDv7 id); Log batch keys never collide with Event keys or processing cursors. Logs share the `ingest_max_waiting` gate with Events.
- [ ] Log processing is one worker separate from Event processing, woken by Log writes and a 1 s tick, reading each Project's batches in order from its Log processing cursor.
- [ ] `logs.duckdb` is attached to the same DuckDB instance as `events.duckdb`. Per batch, one transaction appends to the Project's hot table (created with its first Log: `id`, the seven built-in fields, `attrs_string`, `attrs_number`, `attrs_bool`, `attrs_json`) and moves the cursor, which lives in `logs.duckdb`; then the Pebble batches up to the cursor are deleted.
- [ ] A query-app operation returns the latest N Logs of a Project with every built-in field and Attribute.
- [ ] A controllable clock is the one new substitution point in the EventStore test environment; tests drive Log processing with an explicit "process until caught up" step.
- [ ] Tests: each body form; Bearer and `sentry_key` auth; the Project-mismatch `401`; a `200` followed by a restart before processing still yields the Logs; a crash between the DuckDB commit and the Pebble delete yields no duplicates; Event ingest and processing are unaffected.
- [ ] `CONTEXT.md` gains **Log ingest** and **Log processing**, defined like their Event counterparts, and **Processing cursor** is widened to cover Log processing.
