# Move DuckDB from the preview to the 2.0.0 release

Status: needs-triage

Blocked by: 01

When DuckDB 2.0.0 ships (planned for 2026-10-21) and duckdb-go tags `v2.20000.0`, bump the driver from `v2.20000.0-6.preview`.
New database files created by 2.0 default to storage format v2.0.0, which 1.x engines cannot open. Nothing is deployed, so dev data can be wiped.
Check the 2.0 breaking-change list in the release announcement. Known changes: the old lambda syntax `x -> …` becomes an error, and ICU is replaced by a native implementation.

## Comments
