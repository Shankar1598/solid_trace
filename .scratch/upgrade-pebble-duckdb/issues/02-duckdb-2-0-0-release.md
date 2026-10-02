# Move DuckDB from the preview to the 2.0.0 release

Status: needs-triage

Blocked by: 01

EventStore already runs the DuckDB 2.0 alpha engine (`v2.0.0-alpha43385`). Ticket 01 links it as a shared library with `-tags=duckdb_use_lib`. This ticket is a cleanup, not a blocker: it waits for a duckdb-go driver that bundles the 2.0 engine.

When DuckDB 2.0.0 ships (second half of October, planned for 2026-10-21) and duckdb-go tags `v2.20000.0`, bump the driver from `v2.20000.0-6.preview`. That preview is engine 1.5.4 plus 2.0 backports, not 2.0.
Then remove the alpha wiring from 01: `event_store/scripts/fetch-duckdb`, the `[env]` block in `mise.toml`, the fetch in `bin/go-dev`, the library steps in the Dockerfile, and the `lib/` ignore. Go back to the bundled static library. Keep `TestDuckDBEngineVersion`, and change its prefix check if needed.
New database files created by 2.0 default to storage format v2.0.0, which 1.x engines cannot open. The alpha writes a dev storage header, so wipe dev data when switching.
Check the 2.0 breaking-change list in the release announcement. Known changes: the old lambda syntax `x -> …` becomes an error, and ICU is replaced by a native implementation.

## Comments

### 2026-10-02 hold until 2026-10-21

Don't raise this ticket until the due date (2026-10-21) has passed. See the rules in [STATUS.md](../../STATUS.md).

### 2026-10-02 clarified

The first text said the move could not start before the release. That was wrong: 2.0 is already in use through the alpha shared library. Only the driver swap and the removal of the alpha wiring wait on the `v2.20000.0` tag. The [alpha announcement](https://duckdb.org/2026/09/02/try-duckdb-20-alpha) lists only the CLI and Python as alpha clients and says other clients will be added gradually. It says the alpha is "explicitly not production-ready".
