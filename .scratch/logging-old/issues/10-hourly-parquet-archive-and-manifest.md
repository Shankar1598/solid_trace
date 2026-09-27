# 10: Hourly Parquet archive with manifest and planner integration

**What to build:** Each closed hour of Logs is written to one Parquet file per Project (sorted by time, zstd, fixed column list), registered in the `log_files` manifest and removed from the hot table in one transaction. Queries read the hot table plus only the manifest files overlapping the Project and time range; nothing globs the directory. Startup reconciles unregistered files.

**Blocked by:** 04

**Status:** wontfix

- [ ] Archiver goroutine follows the existing Run/Stop pattern; file layout `project_id=<id>/day=YYYY-MM-DD/hour=HH-<uuid>.parquet`; temp-write then rename, then manifest insert and hot delete in one DuckDB transaction
- [ ] Planner builds `read_parquet([files])` from the manifest and omits the Parquet leg when no file overlaps; Parquet metadata cache enabled
- [ ] Startup deletes files with no manifest row; a test proves rows are still in the hot table in that case
- [ ] An `EXPLAIN` test shows time predicates prune row groups on the archived leg
- [ ] Config key for the Parquet path and the hot window documented

## Comments

2026-09-27: Closed as wontfix. This plan was superseded by the [logs map](../../logs/map.md). The file stays here as reference only.
