# 02: Generic HTTP log ingest endpoint

**What to build:** `POST /api/:project_id/logs` accepts a JSON array, a single JSON object or NDJSON (sniffed from the body), gzip/deflate/br/zstd encodings and the Project's public key as `Authorization: Bearer` or `X-Sentry-Auth`. Records flow through a bounded channel and a batching ingester into `logs_hot`; timestamp, message and level are detected from common keys and normalised; nested objects flatten to dotted keys. A curl, Vector `http` sink and Fluent Bit `http` output all work with only endpoint and key configured.

**Blocked by:** 01

**Status:** wontfix

- [ ] Arrays, single objects and NDJSON bodies all ingest; response is `{ingested, failed, failures:[{index,error}]}` with per-record errors not failing the batch
- [ ] Timestamp detection order `_time > timestamp > time > ts > dt > @timestamp > date`, RFC3339 strings and epoch numbers by magnitude, receive time otherwise; message keys `message > msg > body > log > event`; level keys normalised to the six levels plus severity number
- [ ] Limits enforced: 10,000 records per request, 1 MiB per record, 200-byte keys, request body 16 MiB (Fiber body limit and the Angie `:4000` `client_max_body_size` raised in deploy config)
- [ ] `401` on a bad key, `413` over limits, `429` with `Retry-After` when the ingest channel is full; zstd bodies decode
- [ ] Ingester batches for up to one second, retries a failed write three times, drains its channel on shutdown; new config keys documented in `deploy/event_store.yml`
- [ ] Handler and adapter tests drive the Fiber app in-process; a k6 `log_ingestion` scenario runs via `bin/load-test --kind logs`

## Comments

2026-09-27: Closed as wontfix. This plan was superseded by the [logs map](../../logs/map.md). The file stays here as reference only.
