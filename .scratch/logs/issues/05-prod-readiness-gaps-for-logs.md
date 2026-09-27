# 05: Which production-readiness gaps must be fixed before or alongside logs?

Type: grilling
Status: open
Blocked by: none
Map: [SolidTrace Logs](../map.md)

## Question

`.scratch/prod-readiness/review.md` lists gaps in today's SolidTrace (for example read-API authentication, ingest key lookup cost, shutdown drain, retention). Which of them does the logs work depend on or make worse, which come into this effort's scope, and in what order relative to logs v1?

Input from research (ingest protocols): the envelope parser loses whole Events today when an Event is followed by another item (for example sentry-ruby's `client_report`), returning 400. That is an existing error-tracking bug, not only a logs gap. Research also found the pinned Fiber mishandles zstd bodies and has no decompressed-size cap. See [Which ingest protocols and shippers should logs support?](02-ingest-protocols-and-shippers.md).

Already fixed outside this map (2026-09-26, on `main`): review §2, read-API authentication. The query routes moved to a loopback-only listener (`query_port`, default 4100) with no token, Pebble event keys are prefixed with the project id, and a failed ingest key lookup now returns 500 instead of 401. Detail: [Query API has no authentication](../../prod-readiness/issues/002-query-api-authentication.md). Leave §2 out of this triage.
