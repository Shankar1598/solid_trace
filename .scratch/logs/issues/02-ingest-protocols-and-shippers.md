# 02: Which ingest protocols and shippers should logs support?

Type: research
Status: resolved
Blocked by: none
Map: [SolidTrace Logs](../map.md)

## Question

What protocols and shipper configurations would get logs into SolidTrace with the least user setup? Cover: Sentry SDK structured logs (envelope `log` item format, which SDK versions send them by default, how the current EventStore envelope parser treats them), OTLP/HTTP logs (protobuf and JSON), the HTTP sinks of Vector, Fluent Bit and the OpenTelemetry Collector, and whether speaking an existing API (Axiom, Loki push, Elasticsearch bulk) would let unmodified shipper configs work. For each: payload shape, auth, batching and compression, and rough implementation cost in Go.

Starting points: `.scratch/logging-old/research/research-protocols.md`, `explore-go-internals.md` (unverified).

## Answer

Full findings: `.scratch/logs/research/02-ingest-protocols.md` on branch `research/ingest-protocols` (commit `4e3ecce`); read with `git show research/ingest-protocols:.scratch/logs/research/02-ingest-protocols.md`.

- **Sentry SDKs already send logs, and SolidTrace drops them.** A log-only envelope gets HTTP 200 and is discarded (`event_store/ingest/service.go`). Logs are on by default in sentry-ruby 7.0.0 (2026-09-01, which also turns on Rails structured logging with sentry-rails), sentry-go 0.47+ and JS 10.71+; Python 2.68+ needs no opt-in.
- **The same parser loses whole events.** `IngestEnvelope` splits the envelope into at most three pieces, so an event followed by any other item (for example sentry-ruby's `client_report`) fails JSON parsing, returns 400, and the event is lost. Confirmed by reading the code.
- **OTLP/HTTP protobuf** is the one format every shipper sends without custom code: the OTel Collector (proto + gzip default), Fluent Bit's `opentelemetry` output, and all OTel SDKs. JSON is optional in the spec.
- **Vector is the exception.** Its OTLP encoder needs OTLP-shaped events, so its least-setup path is the plain `http` sink with a JSON array body.
- **Sentry's own OTLP logs endpoint** is `/api/{project_id}/integration/otlp/v1/logs` with `x-sentry-auth`. Sentry's published Collector, Fluent Bit and Vector recipes would work against SolidTrace with only a host change if it mirrored that path and header.
- **Go cost of OTLP protobuf is small:** `go.opentelemetry.io/proto/slim/otlp` v1.11.0 (Apache-2.0), protobuf only. **OTLP JSON trap:** `protojson` silently corrupts hex trace ids; the correct pdata library needs Go 1.26 from v1.66.0 (v1.65.0 is the last on Go 1.25).
- **Axiom-compatible ingest** works for unmodified configs given Axiom paths, Bearer tokens, a dataset→project mapping, NDJSON and zstd. **Loki and Elasticsearch-bulk compatibility** cost more (snappy protobuf and labels; bulk action/source pairs plus `GET /` and `/_cluster/health`).
- **Cross-cutting HTTP gaps:**
  - The pinned Fiber passes zstd bodies through still compressed, puts error text in place of the body when decompression fails, and has no cap on decompressed size.
  - Body limits are 4 MB in Fiber and 1 MB in Angie, but Vector batches up to 10 MB.
  - Auth recognises only `sentry_key=`, not Bearer or Basic.
- **Senders treat 429 differently:** Sentry SDKs drop data until the limit expires; Vector, Fluent Bit and OTLP retry on differing status codes.

Open questions it hands to decisions: which protocols ship in v1; whether to mirror Sentry's OTLP path and auth; whether to accept Bearer/Basic; body and decompression limits; backpressure semantics per sender; OTLP JSON support vs the Go version.
