# 08: Which ingest sources ship in v1, with what auth and limits?

Type: grilling
Status: resolved
Blocked by: 02, 07
Map: [SolidTrace Logs](../map.md)

## Question

Which ingest endpoints ship in v1, how shippers authenticate (existing project keys, bearer tokens, a new ingest token), what request and field limits apply, and how overload is signalled to shippers?

Input (2026-09-26): the event store now has two listeners. Ingest routes register in `RegisterIngestRoutes` on the public listener; query routes live on a loopback-only listener. Key lookup returns 401 only for an unknown key and 500 when the lookup fails, so SDKs retry.

Input (2026-10-01, from [prod-readiness gaps for logs](05-prod-readiness-gaps-for-logs.md)): the envelope parser walks every item but drops `log` items as non-Event items. Decide whether and how Sentry `log` items route to log ingest. Rate limits and per-Project quotas are deferred past v1; only the basic overload signal is in scope here.

Input (2026-10-02, from [What is a Log?](07-log-domain-model.md)): each source must map onto the seven built-in fields. Decide, per source, which incoming keys fill `message`, `time`, `level`, `service` and `environment` (for plain JSON: `msg`/`message`/`log`, `ts`/`timestamp`/`@timestamp` …). Logs use the Project's existing keys; Axiom-style ingest maps its dataset name to a Project.

## Answer

Resolved 2026-10-02 (grilling). Glossary: the `solidtrace.` reserved prefix added to **Field** in `CONTEXT.md`. No ADR: dropping OTLP from v1 is additive to reverse.

**Sources (v1)**
- **Sentry `log` envelope items** on the existing `/api/{project_id}/envelope` route. They are no longer dropped.
- **Plain JSON:** `POST /api/{project_id}/logs`. The body is a JSON array of objects, a single object, or NDJSON (`application/x-ndjson`), and each object is one Log. A line or element that fails to parse is dropped and counted; it does not fail the batch. Vector uses its `http` sink and Fluent Bit its `http` output (`format json`).
- **Not in v1:** OTLP/HTTP and Axiom-compatible ingest (v2 fog). Loki push and Elasticsearch bulk are out of scope. OTel Collector users therefore have no v1 path.

**Auth**
- Logs use only the Project's existing keys, with no new token type. The public key is already a write-only credential.
- The existing `sentry_key=` forms (`X-Sentry-Auth`, `Authorization`, query string) work on every route. The JSON route also accepts `Authorization: Bearer <public key>`.
- The JSON log route rejects a key whose Project differs from the path's `project_id`. Event routes keep their current behaviour, which does not check this.

**Body**
- Log routes accept `gzip`, `zstd` or identity. EventStore decompresses the body itself; it does not rely on Fiber's pass-through.
- The compressed body is capped at 10 MB and the decompressed body at 50 MB. Over either cap the response is 413 and nothing from the request is stored. Angie's and Fiber's body limits are raised for log routes only.

**Overload**
- Every ingest 503, events included, gains `Retry-After` with a few seconds.
- Logs share the existing `ingest_max_waiting` gate with Events in v1; isolation stays in v2 fog. There is no 429 until quotas exist, which is deferred past v1.

**Per-Log limits: truncate and keep; one bad Log never fails a batch**
- `message` and any string value are truncated at 64 KB.
- An Attribute whose name is longer than 256 B is dropped.
- Beyond 1,000 Attributes, the excess is dropped in a stable order.
- Nesting deeper than 10 levels is stored as a JSON string from that depth.
- A Log still over 1 MB after the rules above is dropped.
- A truncated Log carries the boolean Attribute `solidtrace.truncated`.
- The JSON route's response reports dropped and truncated counts. An envelope always returns 200, even when some of its Logs were dropped, as Sentry does.

**Mapping onto built-in fields**
- **Plain JSON:** the first alias present wins and is *consumed*, so it is not also kept as an Attribute. Lower-ranked aliases that are also present stay Attributes. A chosen value that cannot fill its field becomes `original.<field>`.
  - `message`: `message`, `msg`, `log`, `body`
  - `time`: `time`, `timestamp`, `ts`, `@timestamp`, `_time`. Values are RFC 3339 / ISO 8601, or a numeric epoch whose unit (s, ms, µs, ns) is inferred from its magnitude.
  - `level`: `level`, `severity`, `lvl`, `log.level`
  - `service`: `service`, `service.name`
  - `environment`: `environment`, `env`, `deployment.environment`
  - `trace_id`: `trace_id`, `traceId`. `span_id`: `span_id`, `spanId`
- **Sentry log items:**
  - `body` fills `message`, `timestamp` (float seconds) fills `time`, and `level` and `trace_id` fill their fields.
  - Typed `{value, type}` attributes are unwrapped to native values.
  - `sentry.environment` fills `environment` and is consumed; other `sentry.*` keys stay Attributes. `service` comes from a `service.name` attribute, otherwise it is absent.
- **Reserved prefix:** SolidTrace-set Attributes use `solidtrace.`. A sender's `solidtrace.*` key is kept as `original.solidtrace.*`.
