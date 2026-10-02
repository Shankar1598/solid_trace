# 05: Which production-readiness gaps must be fixed before or alongside logs?

Type: grilling
Status: resolved
Blocked by: none
Map: [SolidTrace Logs](../map.md)

## Question

`.scratch/prod-readiness/review.md` lists gaps in today's SolidTrace (for example read-API authentication, ingest key lookup cost, shutdown drain, retention). Which of them does the logs work depend on or make worse, which come into this effort's scope, and in what order relative to logs v1?

Input from research (ingest protocols): the envelope parser loses whole Events today when an Event is followed by another item (for example sentry-ruby's `client_report`), returning 400. That is an existing error-tracking bug, not only a logs gap. Research also found the pinned Fiber mishandles zstd bodies and has no decompressed-size cap. See [Which ingest protocols and shippers should logs support?](02-ingest-protocols-and-shippers.md).

Already fixed outside this map (2026-09-26, on `main`): review §2, read-API authentication. The query routes moved to a loopback-only listener (`query_port`, default 4100) with no token, Pebble event keys are prefixed with the project id, and a failed ingest key lookup now returns 500 instead of 401. Detail: [Query API has no authentication](../../prod-readiness/issues/002-query-api-authentication.md). Leave §2 out of this triage.

## Answer

Resolved 2026-10-01 (grilling). Gaps were checked against the code on that date. §2, §3, §5, §6 and prod-readiness 013 are already fixed.

**Before any logs v1 code**
- Ingest write path 04–06, which finish the §4 durability fixes. The log storage design may reuse the Event pattern (store in Pebble before the `200`, then catch up from a processing cursor).

**In this effort**
- Log retention (§9) is designed in [How are logs stored and retained?](10-storage-architecture-and-retention.md). That ticket must say whether Events can reuse the mechanism. Event retention (Pebble, Parquet) stays a prod-readiness gap.

**Noted for later, not in logs v1**
- Logs must not starve Events: a log flood or a full disk making Event ingest fail. Moved to the map's v2 fog.
- Observability: the `/metrics` endpoint, health reporting and disk alarms. Health reporting and disk alarms moved to the map's v2 fog. Ingest backpressure and a log ingest load test stay in v1.
- Ingest limits: a body-size cap, a zstd decompressed-size cap, rate limits and per-Project quotas. Noted in the prod-readiness review for later, with no ticket.

**Stays in prod-readiness, outside this effort**
- §7, the Issues list N+1 and pagination.
- §8, Go writing Rails tables. Constraint for logs: **logs add no new Go writes to Rails tables**; they only read Project keys.
- The `ValidateKey` cache. Shippers batch, so 1–6k logs/s is about 10–60 requests/s.
- `mode=ro`, compose host networking, the DSN.
- §1 tiering. The logs map's S3/GCS fog covers the log side.
- 011 Pebble memtable size (relevant only if logs go into Pebble).
- Outbox table cleanup.
- Go CI. Tests run in dev; the Rails-only `ci.yml` is not in use, so CI is no prerequisite.
- 012 Sentry compatibility tests, with a note to capture Sentry `log` envelope items in its fixtures.

**Map correction:** the envelope parser already walks every item (§6 fix). What remains is that `log` items are dropped as non-Event items; routing them moves to [Which ingest sources ship in v1, with what auth and limits?](08-v1-ingest-sources-auth-limits.md).
