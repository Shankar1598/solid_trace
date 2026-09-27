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
  - Not needed after all: `SET TimeZone='UTC'`. A bound `time.Time` is sent as TIMESTAMPTZ only when the parameter's type is left open, as in `SELECT typeof(?)`. Compared with the `TIMESTAMP` column in the `NewerThan`/`OlderThan` filters, the parameter is inferred as `TIMESTAMP` and bound as UTC. This was probed under Asia/Kolkata, America/New_York and UTC, with both UTC and IST Go times. `TestDuckDBWriter` now covers both filters.
- **Dockerfile:** use `golang:1.25`, set `CGO_ENABLED=1`, and keep a `bookworm-slim` runtime. The build is already broken on `main` (prod-readiness review §3).
- **Commits:** one each for Pebble, DuckDB and the Dockerfile.

## Done when

- [x] `go test ./...` passes in `event_store`, including the integration tests
- [x] `bin/go-dev` boots, and one event goes in and comes back out through a query
- [x] The Docker image builds and starts
- [x] Logs map and logs 09 note that the "duckdb-go has a 2.0 build" risk is retired

## Out of scope

A Go CI job, memtable tuning ([prod-readiness 011](../../prod-readiness/issues/011-pebble-memtable-size.md)), tiering beyond the first tier.

## Comments

**2026-09-27:** all done-when checks pass on the branch. `go test ./...` passes, including the integration tests, and the storage tests also pass under `TZ=Asia/Kolkata`. `bin/go-dev` ingested one event: it came back from Pebble by UUID, and from DuckDB with its tags. The Docker image builds, starts healthy and ingests. The ticket stays `claimed` until the branch merges to `main`.

**2026-09-27, later: moved to the real 2.0 alpha engine.**
- The `-6.preview` tag reports `version()` = v1.5.4, so it gives none of the 2.0 features (async I/O, storage v2.0, VARIANT shredding).
- We first looked at running 2.0 as a separate Quack server and connecting from the embedded preview driver. It does not work:
  - The Quack protocol changed incompatibly at 2.0. Every 1.5.x client, including GizmoData's pure-Go one, fails against the 2.0 server with `Failed to deserialize`.
  - The preview driver crashes on `LOAD quack`.
  - The 2.0 alpha does not accept bound parameters over Quack.
- **Decision:** EventStore links the alpha in-process.
  - The driver stays at `v2.20000.0-6.preview`, built with `-tags=duckdb_use_lib`.
  - `event_store/scripts/fetch-duckdb` downloads one pinned build, `ca15f79c32/v2.0.0-alpha43385`, from duckdb-staging. It checks a SHA-256 per platform (linux amd64/arm64, macOS universal) and writes to `event_store/lib/duckdb/`, which is gitignored.
  - `mise.toml` `[env]` sets `GOFLAGS` and `CGO_LDFLAGS` with an rpath, so no `LD_LIBRARY_PATH` is needed.
  - `bin/go-dev` runs the fetch.
  - The Dockerfile fetches the library, links it, and copies it into `/usr/local/lib` on the runtime image.
  - `TestDuckDBEngineVersion` fails unless the engine is v2.0, so a shell without the mise env fails loudly.
- **Risks:**
  - The staging URLs may not be kept forever. If they disappear, re-pin to a newer alpha.
  - New `.duckdb` files use the dev storage header, which neither 1.x nor the final 2.0 may open. That is fine, because the data is dev-only.
  - macOS is wired up but untested, because there is no Mac.
- **Verified on the alpha:**
  - `go test ./...` passes, including integration and with `TZ=Asia/Kolkata`. The engine guard also passes through `mise exec`.
  - `bin/go-dev` links `lib/duckdb/libduckdb.so` and ingests and queries correctly. It also opened the dev `.duckdb` file written earlier by the 1.5.4 preview.
  - The Docker image links `/usr/local/lib/libduckdb.so`, starts healthy and ingests.
  - The worktree's `mise.toml` needed a one-time `mise trust` before its `[env]` applied.
