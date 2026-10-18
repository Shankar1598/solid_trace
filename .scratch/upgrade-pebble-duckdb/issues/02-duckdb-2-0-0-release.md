# Move EventStore back to DuckDB 1.5.5

Status: resolved

Blocked by: 01

Re-scoped 2026-10-02 by [How are logs stored and retained?](../../logs/issues/10-storage-architecture-and-retention.md): the logs storage design needs only DuckDB 1.5 features, so EventStore leaves the 2.0 alpha now and runs the released 1.5.5. DuckDB 2.0 becomes a later upgrade (logs map, v2 fog). The logs storage benchmark ([logs 14](../../logs/issues/14-benchmark-storage-design-on-1-5-5.md)) waits on this.

- Swap the driver from `v2.20000.0-6.preview` to `v2.10505.0` (engine 1.5.5, bundled static library). Check for a newer 1.5.x tag first.
- Remove the alpha wiring from 01: `event_store/scripts/fetch-duckdb`, the `[env]` block in `mise.toml`, the fetch in `bin/go-dev`, the library steps in the Dockerfile, the `lib/` ignore and the `-tags=duckdb_use_lib` build tag.
- Keep `TestDuckDBEngineVersion` and change its prefix check to `v1.5`.
- `duckdb.OrderedMap` (`event_store/storage/duckdb.go`) came with the preview driver; check that `v2.10505.0` has it, or switch back to `duckdb.Map`.
- Wipe dev data: 1.5 cannot open files written by the 2.0 alpha.

## Comments

### 2026-10-02 hold until 2026-10-21

Don't raise this ticket until the due date (2026-10-21) has passed. See the rules in [STATUS.md](../../STATUS.md).

### 2026-10-02 clarified

The first text said the move could not start before the release. That was wrong: 2.0 is already in use through the alpha shared library. Only the driver swap and the removal of the alpha wiring wait on the `v2.20000.0` tag. The [alpha announcement](https://duckdb.org/2026/09/02/try-duckdb-20-alpha) lists only the CLI and Python as alpha clients and says other clients will be added gradually. It says the alpha is "explicitly not production-ready".

### 2026-10-02 re-scoped to 1.5.5

The ticket was "Move DuckDB from the preview to the 2.0.0 release". Logs ticket 10 decided to go back to the released 1.5.5 instead, so the hold until 2026-10-21 and the comments above no longer apply.

### 2026-10-02 resolved

Done on branch `feature/logging` (not yet on `main`).

- The driver is `v2.10506.0` (DuckDB **1.5.6**, tagged 2026-09-30), the newest 1.5.x tag. The bindings had to be pinned to `v0.10506.0` by hand: Go's version selection would not downgrade them from the preview.
- The alpha wiring is gone: `scripts/fetch-duckdb`, the `[env]` block in `mise.toml`, the fetch in `bin/go-dev`, the library steps in the Dockerfile and the `lib/` ignore (`.dockerignore` keeps `bin/`).
- `TestDuckDBEngineVersion` now expects `v1.5`.
- `duckdb.OrderedMap` is still in the 1.5 driver, so `storage/duckdb.go` is unchanged.
- Dev data: `storage/duckdb/development` (written by the alpha) was moved aside, not deleted. Pebble's processing cursors are past those Events, so dev DuckDB starts empty.
- `logsbench` (`event_store/bench/logslayouts`): the VARIANT layouts need 2.0 (`variant_extract_string` is missing on 1.5, and VARIANT storage needs `storage_compatibility_version` v1.5.0, after which a checkpoint ran out of memory). `Layouts()` now runs JSON and MAP only; the VARIANT code stays for the 2.0 re-check, and `TestRunReportsVariantShredding` is skipped.
- A shell started while `mise.toml` had the `[env]` block keeps `GOFLAGS=-tags=duckdb_use_lib` exported; open a new shell (or `unset GOFLAGS CGO_LDFLAGS`), or the build looks for the removed library.
- `go vet ./...` and `go test -p 1 ./...` pass. The Docker image was not rebuilt.
