# 05: Which production-readiness gaps must be fixed before or alongside logs?

Type: grilling
Status: claimed
Blocked by: none
Map: [SolidTrace Logs](../map.md)

## Question

`.scratch/prod-readiness/review.md` lists gaps in today's SolidTrace (for example read-API authentication, ingest key lookup cost, shutdown drain, retention). Which of them does the logs MVP depend on or make worse, which come into this effort's scope, and which must land before MVP code starts? (Narrowed to the MVP on 2026-09-27.)

Input from research (ingest protocols): the envelope-parser bug it found is fixed (review §6, commit `3ec0486`). Research also found the pinned Fiber mishandles zstd bodies and has no decompressed-size cap. See [Which ingest protocols and shippers should logs support?](02-ingest-protocols-and-shippers.md).

Already fixed outside this map (2026-09-26, on `main`): review §2, read-API authentication. The query routes moved to a loopback-only listener (`query_port`, default 4100) with no token, Pebble event keys are prefixed with the project id, and a failed ingest key lookup now returns 500 instead of 401. Detail: [Query API has no authentication](../../prod-readiness/issues/002-query-api-authentication.md). Leave §2 out of this triage.

Also fixed or in progress outside this map (2026-09-27): §3, the Docker build, is fixed on the unmerged `upgrade-pebble-duckdb` branch, which also moves to the DuckDB 2.0 preview driver. §5, concurrent first Events, has an uncommitted fix on `main`. §4, acknowledged Events that vanish, is being redesigned: Event ingest will write each Event to Pebble before replying `200`.
