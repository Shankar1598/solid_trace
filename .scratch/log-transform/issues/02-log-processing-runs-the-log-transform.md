# 02: Log processing runs a Project's Log transform

**What to build:** When a Project has a current Log transform, Log processing runs it on each of the Project's Logs between the stored Log batch and DuckDB, so the hot table, `logs_late`, promotion and Match monitors all see the transformed Logs. A Log the program fails on is stored as it arrived, with the error in `solidtrace.transform_error`. See the spec's "What the program sees" and "Where it runs, and what a failure does".

**Blocked by:** 01

**Status:** claimed

- [ ] Console's `log_transform_versions` table exists (migration), and EventStore's root tests can seed rows in it as `seedProject` seeds Retention. The newest row is current; no row or an empty program means none.
- [ ] At the start of each pass over a Project, Log processing reads the current version read-only and uncached, and compiles it once per version id; a newer version replaces and frees the old one.
- [ ] `.` is as the spec says: built-in fields at the top level (`.time` a timestamp, `.level` absent when the Log has none), Attributes nested by their dots with their types, the `a`/`a.b` collision as the quoted key `."a.b"`, `solidtrace.*` visible, the Log's id never shown or changed.
- [ ] What the program returns is flattened back and normalised by the same code Log ingest uses, through a shared entry point for a decoded object: `original.<field>`, Levels, `original.solidtrace.*`, the limits and `solidtrace.truncated`. A `.` that isn't an object is a failure.
- [ ] `abort` drops the Log; a batch left empty is still passed by the Processing cursor.
- [ ] A Log the program fails on is stored as it arrived, with `solidtrace.transform_error` (cut to 1 KB) set after normalisation so it isn't renamed.
- [ ] A saved version that doesn't compile stores every Log of the Project as it arrived, with `solidtrace.transform_error` naming the compile error.
- [ ] A new version applies only to batches processed after it.
- [ ] Root tests cover each case in the spec's Testing Decisions list that doesn't go through the routes.
