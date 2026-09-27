# Upgrade Pebble to v2.1.7 and DuckDB to the 2.0 preview

Status: claimed

**Source:** grilling session, 2026-09-27
**Branch:** `upgrade-pebble-duckdb` (worktree `worktree/upgrade-pebble-duckdb`)
**Area:** `event_store/go.mod`, `event_store/storage/pebble.go`, `event_store/storage/duckdb.go`, `event_store/Dockerfile`

## Why

Nothing is deployed, so we take the newest versions and keep no data. This is also the prerequisite for the logs effort's DuckDB 2.0 target ([logs map](../../logs/map.md), [logs 09](../../logs/issues/09-benchmark-storage-layouts.md)).

## Decisions

- **Data:** dev only, and none exists on disk. No migration and no backups.
- **Pebble v2.1.4 → v2.1.7.** This is the latest tag. There is no `/v3`, and there are no API or format changes. Set `FormatMajorVersion: pebble.FormatValueSeparation` (24), pinned by name, so the format moves only when the code changes. Value separation stays off: `Experimental.ValueSeparationPolicy` defaults to `Enabled: false`. Stores now write sstable format Pebblev7 (columnar) instead of Pebblev4.
- **DuckDB driver v2.5.4 (engine 1.4.3) → `v2.20000.0-6.preview`.** This is engine 1.5.4 plus the 2.0 backports from DuckDB's internal "v1.5-stable" branch. It is a pre-release of `v2.20000.0`, which will ship engine 2.0.0. There is no Go build of the real 2.0 alpha without linking a shared library. The preview has no release notes.
  - Replace the deprecated `duckdb.Map` with `OrderedMap`.
  - Set `SET TimeZone='UTC'` when the database opens. Since v2.5.5, a bound `time.Time` is sent as TIMESTAMPTZ, and the `NewerThan`/`OlderThan` filters compare it with a `TIMESTAMP` column.
- **Dockerfile:** use `golang:1.25`, set `CGO_ENABLED=1`, and keep a `bookworm-slim` runtime. The build is already broken on `main` (prod-readiness review §3).
- **Commits:** one each for Pebble, DuckDB and the Dockerfile.

## Done when

- [ ] `go test ./...` passes in `event_store`, including the integration tests
- [ ] `bin/go-dev` boots, and one event goes in and comes back out through a query
- [ ] The Docker image builds and starts
- [ ] Logs map and logs 09 note that the "duckdb-go has a 2.0 build" risk is retired

## Out of scope

A Go CI job, memtable tuning ([prod-readiness 011](../../prod-readiness/issues/011-pebble-memtable-size.md)), tiering beyond the first tier.

## Comments
