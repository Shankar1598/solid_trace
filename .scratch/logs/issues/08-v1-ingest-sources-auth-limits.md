# 08: How does JSON-over-HTTP log ingest work?

Type: grilling
Status: open
Blocked by: 02, 07
Map: [SolidTrace Logs](../map.md)

## Question

The MVP has one ingest source: JSON over HTTP (decided 2026-09-27; Sentry SDK logs, OTLP, and Vector- or Axiom-compatible endpoints are on the map's Future list). What is the endpoint and payload format (NDJSON, a JSON array, or both), how does a sender authenticate with a Project key, what request and field limits apply, and how is overload signalled to senders?

Input (2026-09-26): the event store now has two listeners. Ingest routes register in `RegisterIngestRoutes` on the public listener; query routes live on a loopback-only listener. Key lookup returns 401 only for an unknown key and 500 when the lookup fails, so SDKs retry.

The ingest channel now answers 503, not 429, when full (commit `bbfbaf1`): Sentry SDKs pause for 60 s on a 429. Senders of JSON over HTTP are not Sentry SDKs, so check whether the same choice suits them (Vector and Fluent Bit retry on both).

## Input: production-readiness gaps (decided 2026-09-27)

From [Which production-readiness gaps must be fixed before or alongside logs?](05-prod-readiness-gaps-for-logs.md):

- **Constraint:** log ingest never writes Console's SQLite, and reads it only through a cached Project key lookup (review §8).
- Decide here: the Project key cache (`ValidateKey` hits SQLite on every request today), the request body limit (Fiber's default is 4 MB, not overridden), and a cap on decompressed body size, including Fiber's zstd handling (review §10; research on ingest protocols).
- Rate limits and per-Project quotas are on the map's Future list.
