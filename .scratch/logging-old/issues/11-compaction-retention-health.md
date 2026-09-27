# 11: Compaction, per-Project retention and health

**What to build:** Administrators set a retention period in days on each Project. EventStore deletes Log Parquet files older than that period every hour and merges a day's small hourly files into one file nightly. The health endpoint reports Log ingest lag and disk usage.

**Blocked by:** 10

**Status:** wontfix

- [ ] `projects.logs_retention_days` (default 30) editable in Project settings; EventStore reads it through its existing read-only SQLite access
- [ ] Retention sweep deletes files whose max time is past retention and removes their manifest rows; compaction merges per (Project, day) when the total is under 512 MB and updates the manifest atomically
- [ ] `/health` includes logs channel depth, last flush time, manifest file count and bytes on disk
- [ ] Tests for sweep and compaction with temporary directories, including a crash between file write and manifest update

## Comments

2026-09-27: Closed as wontfix. This plan was superseded by the [logs map](../../logs/map.md). The file stays here as reference only.
