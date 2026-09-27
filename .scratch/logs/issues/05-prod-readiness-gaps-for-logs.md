# 05: Which production-readiness gaps must be fixed before or alongside logs?

Type: grilling
Status: resolved
Blocked by: none
Map: [SolidTrace Logs](../map.md)

## Question

`.scratch/prod-readiness/review.md` lists gaps in today's SolidTrace (for example read-API authentication, ingest key lookup cost, shutdown drain, retention). Which of them does the logs MVP depend on or make worse, which come into this effort's scope, and which must land before MVP code starts? (Narrowed to the MVP on 2026-09-27.)

Input from research (ingest protocols): the envelope-parser bug it found is fixed (review §6, commit `3ec0486`). Research also found the pinned Fiber mishandles zstd bodies and has no decompressed-size cap. See [Which ingest protocols and shippers should logs support?](02-ingest-protocols-and-shippers.md).

Already fixed outside this map (2026-09-26, on `main`): review §2, read-API authentication. The query routes moved to a loopback-only listener (`query_port`, default 4100) with no token, Pebble event keys are prefixed with the project id, and a failed ingest key lookup now returns 500 instead of 401. Detail: [Query API has no authentication](../../prod-readiness/issues/002-query-api-authentication.md). Leave §2 out of this triage.

Also fixed or in progress outside this map (2026-09-27): §3, the Docker build, is fixed on the unmerged `upgrade-pebble-duckdb` branch, which also moves to the DuckDB 2.0 preview driver. §5, concurrent first Events, has an uncommitted fix on `main`. §4, acknowledged Events that vanish, is being redesigned: Event ingest will write each Event to Pebble before replying `200`.

## Answer

Decided with the user on 2026-09-27, against the MVP scope. Each gap has one bucket: **prerequisite** (lands before MVP code starts; stays prod-readiness work; the MVP spec states it), **input** (a named logs ticket decides it for logs), or **independent** (prod-readiness work this map drops).

| Gap (`review.md`) | Bucket | Where |
|---|---|---|
| §1 Tiered storage | Future | the map's Future list |
| §2 Query API auth | fixed | before this ticket |
| §3 Docker build, plus the DuckDB 2.0 driver | fixed | merged to `main` (`91f0f8f`); EventStore links the 2.0 alpha |
| §4 Acknowledged Events that vanish | **prerequisite** | The user's change: Event ingest writes each Event to Pebble before replying `200`. It lands before MVP code starts, because log ingest shares the process, the shutdown sequence and probably the write path. The benchmark does not wait for it. Durability for logs waits for it on [How are logs stored and retained?](10-storage-architecture-and-retention.md). |
| §5 Concurrent first Events | fixed | `7de90dd` |
| §6 Sentry protocol correctness | fixed | `3ec0486`; envelope log items are Future |
| §7 Issues list N+1 | independent | |
| §8 EventStore writes Console's SQLite | **input** (logs), independent (Events) | Constraint: log ingest never writes Console's SQLite and reads it only through a cached Project key lookup. Input to [How does JSON-over-HTTP log ingest work?](08-v1-ingest-sources-auth-limits.md) and [How are logs stored and retained?](10-storage-architecture-and-retention.md). |
| §9 No retention | decided for logs; independent for Events | one global window for logs (MVP scope); Event retention stays prod-readiness work |
| §10 `ValidateKey` has no cache; no `BodyLimit` override; no decompressed-size cap (and Fiber mishandles zstd) | **input** | [How does JSON-over-HTTP log ingest work?](08-v1-ingest-sources-auth-limits.md) |
| §10 No rate limit or per-Project quota | Future | the map's Future list |
| §10 No metrics endpoint | Future | the map's Future list |
| §10 `mode=ro` DSN, compose host networking, hardcoded DSN, `ArchiveEventsForDate` | independent | `ArchiveEventsForDate` is also a failure mode noted on ticket 10 |
| §10 CI has no Go job; integration test needs Rails | independent | The user runs tests locally; no CI before MVP code. |
| Prod-readiness 011, Pebble memtable size | independent | |
