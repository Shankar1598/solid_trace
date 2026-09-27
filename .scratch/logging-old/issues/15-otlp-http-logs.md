# 15: OTLP/HTTP log ingest

**What to build:** OpenTelemetry Collectors and SDKs export logs to SolidTrace over OTLP/HTTP in protobuf or JSON with the Project key as a bearer token. Resource attributes are kept under `resource.*` with service, environment and host promoted to columns; responses follow the OTLP partial-success contract.

**Blocked by:** 02

**Status:** wontfix

- [ ] `POST /api/:project_id/otlp/v1/logs` and `POST /v1/logs` accept `application/x-protobuf` (via the official OTLP proto module) and `application/json` (hand-written decoder handling hex trace/span ids and int64-as-string)
- [ ] Severity number and text, body, attributes and resource attributes map to the canonical record
- [ ] `200` with `partialSuccess`, `400` on undecodable bodies, `429`/`503` with `Retry-After` on backpressure
- [ ] Docs include a Collector `otlphttp` exporter example; adapter and handler tests

## Comments

2026-09-27: Closed as wontfix. This plan was superseded by the [logs map](../../logs/map.md). The file stays here as reference only.
