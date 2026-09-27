# Move DuckDB from the preview to the 2.0.0 release

Status: needs-triage

Blocked by: 01

When DuckDB 2.0.0 ships (planned for 2026-10-21) and duckdb-go tags `v2.20000.0`, bump the driver from `v2.20000.0-6.preview`.
Then remove the alpha wiring from 01: `event_store/scripts/fetch-duckdb`, the `[env]` block in `mise.toml`, the fetch in `bin/go-dev`, the library steps in the Dockerfile, and the `lib/` ignore. Go back to the bundled static library. Keep `TestDuckDBEngineVersion`, and change its prefix check if needed.
New database files created by 2.0 default to storage format v2.0.0, which 1.x engines cannot open. The alpha writes a dev storage header, so wipe dev data when switching.
Check the 2.0 breaking-change list in the release announcement. Known changes: the old lambda syntax `x -> …` becomes an error, and ICU is replaced by a native implementation.

## Comments
