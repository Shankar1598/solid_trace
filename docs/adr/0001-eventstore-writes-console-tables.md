# EventStore writes Console's issue tables directly

Status: accepted (2026-10-01)

EventStore (Go) opens Console's primary SQLite file and writes `issues`, `issue_fingerprints`, `project_issue_counters` and `project_seen_cursors` itself (`event_store/storage/sqlite.go`). Console (Rails) owns the schema and the migrations. We accept this coupling instead of making the outbox two-way, so EventStore never writes Console tables.

## Why

- Event processing owns the Issue lifecycle (see the ingest write path spec). Doing that through a message round trip to Rails would add latency and a second place for the logic to live.
- The system is in the 0.1 prototype stage. A direct write is the simplest thing that works, and we want to see the end result before hardening it.

## Consequences

- Rails' enums are duplicated in Go as integers: Issue status (0/1) and kind (0/1/2). `assign_number` is reimplemented too. A Rails-side change to any of these silently desyncs EventStore. Anyone changing a Console enum or those tables must change `storage/sqlite.go` in the same commit.
- Ingest shares SQLite's single write lock with Rails. That lock, not Pebble, is the ingest ceiling.
- `event_store/integration_test.go` shells out to `bin/rails db:reset`, so the Go suite needs a Ruby toolchain.

## Not done now

Both are cheap guards that we may add later: generate the Go constants from the Rails enums, and check a schema version at EventStore startup.

## Reopen if

Ingest throughput is limited by the shared SQLite lock, the Console schema starts changing often, or EventStore needs to run apart from Console's disk.
