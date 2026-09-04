# SolidTrace: production-readiness review

**Date:** 2026-09-04
**Scope:** whole repo, reviewed against the objective stated in `README.md`
**Method:** read both services end to end; ran `go vet`, `go test ./...`, and the Dockerfile's build command; verified library behaviour against the pinned source in the module cache.

This document is the source of truth for the tickets in `issues/`. Tickets are derived
from it; the reasoning stays here.

## Verdict

The thesis is sound. "Two services + embedded databases instead of Sentry's fleet" is a
good product bet, and the shape delivers on it: Pebble for raw payloads (UUIDv7 keys give
time-locality for free), DuckDB for analytical columns, SQLite for relational. The ingest
pipeline (`chan -> batch -> Pebble -> chan -> DuckDB -> outbox`) is the right pattern, and
the outbox-over-SQLite for Go<->Rails messaging is a good "no Redis" move.

This is not a rearchitect. It is that the README describes a more finished system than the
repo contains, and a handful of things would bite on first deploy.

---

## 1. Tiered storage: README describes behaviour the code does not implement

`event_store/storage/pebble.go:59-71`. Only the **first** configured tier is ever used:

```go
if mainLocator == "" {
    mainLocator = loc
    if tier.Level > 0 { strategy = remote.CreateOnSharedLower } else { strategy = remote.CreateOnSharedAll }
}
opts.Experimental.CreateOnSharedLocator = mainLocator
```

Pebble has exactly one `CreateOnSharedLocator` and one global `CreateOnShared` enum. There
is no per-level routing. Consequences:

- The README's "SSD -> HDD -> S3/GCS" example does not work. The second tier is registered
  in the factory map (so it could *read*) but never receives a write.
- `level:` is not a level. It is a boolean: `>0` means "bottom levels only". `level: 3` and
  `level: 6` behave identically.
- "automatically move older, colder data" is not what happens. Pebble *creates* objects on
  shared storage during compaction; nothing migrates. Enabling this on an existing DB does
  nothing until compaction rewrites those levels.
- Undocumented cost: a read that misses local cache becomes an S3 range GET. "Open an old
  issue" is a normal action, so this is a user-visible latency change.

**Resolution:** rewrite the docs to describe one optional cold locator for the bottom LSM
levels. Do not build per-level routing — Pebble cannot do it, and the Parquet archive
already covers the real need.

## 2. Query API has no authentication, and the deploy exposes it

`event_store/main.go:99-104` — `/api/events/:uuid`, `/api/:project_id/events`,
`/events/count`, `/events/context`. No auth middleware. `project_id` is read straight from
the path.

`deploy/angie/angie.conf` publishes `listen 4000` -> the event_store socket. So on a real
deploy `curl http://host:4000/api/1/events` dumps another org's event index, and
`/api/events/<uuid>` returns the full raw payload: stack traces, request headers, whatever
the SDK captured.

Ingest endpoints check the key. Read endpoints check nothing.

Related: `event_store/handler/ingest.go` collapses `err != nil || projectID == 0` into a
`401`. A transient SQLite error becomes "Invalid project key", and Sentry SDKs treat 401 as
fatal — they drop the event rather than retry. Should be 500.

## 3. Production Docker image does not build

`event_store/Dockerfile:1` is `golang:1.24-bookworm`; `go.mod` requires `go 1.25.5`.
Line 16 is `CGO_ENABLED=0`. Verified:

```
# github.com/duckdb/duckdb-go/mapping
mapping_linux_amd64.go:11:22: undefined: bindings.Type
```

DuckDB's bindings are cgo-only; `mattn/go-sqlite3` is too (it would compile to a stub that
errors at `sql.Open`). The `Getting rid of cgo dependencies` commit landed for Pebble
(replacing RocksDB), but DuckDB and SQLite still need cgo. Needs `CGO_ENABLED=1`, a builder
with a C toolchain, and a glibc runtime base or a static musl build.

## 4. Events acknowledged with 200 can vanish silently

Four paths, none documented as a tradeoff:

- **Shutdown drops everything buffered.** `event_store/main.go:110-118` calls
  `app.Shutdown()` and returns. `pebbleChan` is never closed, so `PebbleIngester.Run()`
  never reaches its drain-and-flush path. With `PebbleChannelSize: 50000` and
  `DuckDBChannelSize: 100000`, a routine restart discards up to 150k acknowledged events.
- **`pebble.NoSync`** at `storage/pebble.go:106`. Defensible as a choice, but combined with
  the above the durability story is "best effort until the OS flushes". Say so in the README.
- **DuckDB channel overflow diverges the two stores.** `pipeline/pebble_ingester.go:53-56`
  logs `"DuckDB channel full, dropping event"` and continues. The event is in Pebble forever
  but never appears in any query. Pebble already committed, so this should block or spill,
  not drop.
- **`DuckDBIngester.flush()`** discards the whole batch on write error
  (`batch = batch[:0]; return`) with no retry.

## 5. Concurrent first-events for a new fingerprint return 500

`event_store/ingest/service.go:75-87` is find-then-create with no uniqueness handling,
running under fasthttp's goroutine-per-connection. Two events for the same *new* fingerprint
arriving concurrently both miss the lookup, both INSERT, and the second violates
`index_issue_fingerprints_on_project_id_and_fingerprint`. Transaction rolls back,
`ingestEvent` returns an error, handler returns 500, SDK drops the event.

The `single_issue` load scenario at 100 VUs triggers this at t=0.

Needs `ON CONFLICT DO NOTHING` + re-select, or a retry loop.

## 6. Sentry protocol correctness

- **Multi-item envelopes break.** `ingest/service.go:48`:
  `strings.SplitN(envelope, "\n", 3)` leaves `lines[2]` as
  `payload1\nitemheader2\npayload2...`. `json.Unmarshal` fails -> 400. Event+attachment and
  event+profile envelopes are routine SDK traffic. Needs a real item loop honouring the
  `length` field in each item header.
- **Transactions become fake issues.** Line 59 accepts `type == "transaction"` and routes it
  through `classifyIssue`. A transaction has no `exception`, so it becomes
  `title: "Unknown Error"`, `kind: default` — a garbage issue per transaction. Drop them
  until performance monitoring is a real feature.
- **String timestamps fall through to `time.Now()`.** `ingest/service.go:123-129` handles
  `payload["dt"]` (not a Sentry field) and `payload["timestamp"]` as `float64`. The Sentry
  spec allows `timestamp` as an ISO-8601 *string*, which the Ruby and Python SDKs send.
  Neither branch matches, so offline/batched events are timestamped wrong.
- **`truncate` splits UTF-8.** `ingest/issue_classifier.go:267` slices bytes, corrupting any
  non-ASCII error message at the 250-byte boundary.

Worth noting: `classifyIssue` itself is good. The culprit-extraction port is careful and the
888-line test file is the best-tested part of the repo.

## 7. The UX claim is where the architecture hurts

`console/app/serializers/issue_serializer.rb:18,21`:

```ruby
events_count: @issue.events.count,
last_seen_at: @issue.events.first&.created_at&.iso8601 || ...
```

Each is a synchronous HTTP round-trip to Go, each running a DuckDB query over the `events`
view (hot table `UNION ALL` every Parquet file). And
`console/app/controllers/issues_controller.rb:8` has **no pagination** —
`scoped_resources.order(created_at: :desc)` with no `limit`. So the issues list is `2 x N`
HTTP calls and `2 x N` full scans, N being every issue the org has ever had. At 500 issues
the page never renders.

Root cause is a schema gap: `issues` has no `times_seen`, `first_seen_at`, or `last_seen_at`.
Those are the three columns every issue list in every error tracker sorts and displays on,
and they are being recomputed from the event store on every render.

The outbox already sends `issue_created` / `issue_received_event`, so
`EventStoreMessageProcessorJob` could carry counts and max-timestamps and bump the columns in
one UPDATE. That kills the N+1, makes the list paginable, and unlocks:

`console/app/frontend/pages/Issues/Index.tsx:93,115` renders a time-range picker
(24H/14D/30D) and a sort dropdown (Last Seen / First Seen / Priority). Neither is wired to
anything — the controller reads only `status`, `query`, `project_id`. `filters.environment`
is in the TypeScript interface and the URL builder but the controller never sends it, so it
serialises as the string `"undefined"`. Dead controls read worse than absent ones.

Also `title LIKE '%query%'` is a full scan. SQLite ships FTS5.

## 8. Architectural coupling (decision needed, not a ticket)

The Go service opens Rails' primary SQLite and writes `issues`, `issue_fingerprints`, and
`project_issue_counters` directly (`event_store/storage/sqlite.go`), reimplementing
`assign_number`, the status enum (0/1) and the kind enum (0/1/2) as magic integers in Go.
Any Rails-side enum change silently desyncs.

It also means every ingested event takes at least one read (and new issues a write
transaction) on the same SQLite file Rails writes to, under SQLite's single-writer lock.
That is the real ingest ceiling — not Pebble.

Options: (a) make the outbox bidirectional so Go never writes Rails tables, or (b) accept the
coupling and encode it explicitly — generate the Go constants from the Rails enums, add a
schema-version check at startup. Currently it is implicit, which is the worst of both.

Smaller version of the same problem: `event_store/integration_test.go` shells out to
`bin/rails db:reset`, so the Go suite cannot run without a working Ruby toolchain. It fails
on a clean checkout. (Unit tests pass: `ingest`, `storage`, `msgpacker` are green.)

## 9. No retention policy anywhere (decision needed, not a ticket)

Grepped: no delete, prune, TTL, or `DeleteRange` in the Go service. Pebble grows forever with
~8KB raw payloads per event. DuckDB's hot table archives to Parquet, but Parquet also grows
forever, and `event_store_messages` / `console_messages` are never cleaned up (~170k rows/day
at the 1s flush cadence; only `notifications` has a cleanup job).

For a product pitched as "one VM, periodic snapshots", unbounded disk growth is the primary
way an install dies. A per-project retention window matters more than tiered storage does —
and is the thing tiered storage is currently standing in for.

## 10. Smaller items

- `deploy/docker-compose.yml` uses `network_mode: host` on every service, but `angie.conf`
  does `proxy_pass http://console:80`. Docker DNS does not resolve service names under host
  networking. That path cannot work as written.
- `console/app/models/project_key.rb:7`: DSN hardcoded to `localhost:3000` (dev Rails is on
  4001, ingest on 4000) with a literal project id `1`. Every user's first copy-paste fails.
- `event_store/auth/project.go:14`: `sqlitePath + "?mode=ro"`. mattn/go-sqlite3 truncates the
  DSN at `?` unless it starts with `file:` (sqlite3.go:1451), so the read-only guard is
  silently not applied. Use `file:` + `mode=ro`. (Busy timeout is fine — the driver defaults
  to 5000ms.)
- `ValidateKey` hits SQLite on every event with no cache. An LRU on public_key -> project_id
  removes a DB round-trip from the hottest path.
- `ArchiveEventsForDate`: `COPY TO` writes a file a transaction rollback cannot undo. If
  `COPY` succeeds and `Commit` fails, the Parquet file survives *and* the rows stay in
  `events_hot` — and since `recreateEventsView` globs on every boot, the view then returns
  those events **twice**. (Already flagged in-code as needing a rewrite; this is the specific
  failure.)
- `fiber.New` sets no `BodyLimit` (defaults to 4MB) and there is no rate limiting or
  per-project quota. One misbehaving client can fill the disk. Gzip request bodies *do* work
  — Fiber's `Body()` handles `Content-Encoding` (verified against v2.52.10).
- No metrics endpoint. Pebble exposes rich `Metrics()` and `prometheus/client_golang` is
  already a transitive dep. Channel depths, batch sizes and drop counts are what a
  self-hoster needs.
- README links `test/load/k6/README.md`; the directory is `load_testing/`. The benchmark
  table is empty placeholders.
- CI has `scan_js` commented out and no Go job at all.

---

## Recommended order

1. Drain channels on shutdown — smallest fix, largest data-loss win.
2. Fix the Dockerfile — nothing else matters if the image cannot build.
3. Auth (or de-expose) the query API — this is a data breach on a public deploy.
4. Denormalise `times_seen` / `first_seen_at` / `last_seen_at`, paginate the index.
5. Retention policy.
6. Rewrite the tiered-storage README section.
7. Protocol correctness (envelopes, transactions, timestamps) and the `ON CONFLICT` fix.
