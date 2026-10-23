# 09: Logs from before the switch, read in place

**What to build:** The ClickHouse release loads a prepared install's state and reads its old Log files where they are, from the bucket or the disk. Every Query over a range that reaches before the upgrade answers as it did. `deploy/install` prepares an install from the previous release's image, and EventStore refuses to start on unprepared DuckDB data. See the [spec](../spec.md)'s "Logs from before the switch" and "Cutover".

**Blocked by:** 05, 08

**Status:** ready-for-agent

- [x] **Starting:** with `logs.duckdb` and no marker, EventStore refuses to start and names the command. With a marker, it loads the prepared files into ClickHouse once: the old Log files, the old promoted sets, the key counts, and the cursors into Pebble. It records the switch time.
- [x] **Reads** whose range reaches before the switch time also read that range's old files:
  - with `s3()` for uploaded files and `file()` for local ones;
  - with each day's promoted set, and `coalesce(column, map[key])` for a promoted key;
  - NULL for a column a file lacks.

  This covers pages, tables, the histogram (old hours go through the bin cache, not the MV), a Log by id, and Fields from the loaded key counts.
- [x] **Old hours' generations** change only when their files are deleted.
- [x] **Retention and Project deletion** delete old files' objects, local copies and rows. The old S3 adapter stays for this.
- [x] **The old path is one module** the rest calls through, so 12 can delete it.
- [x] **`deploy/install`**, finding DuckDB data and no marker, runs `prepare-clickhouse` from the previous release's image with the stack stopped, before starting the new one. It says what it's doing and stops if the command fails.
- [x] **Root tests**, from 08's committed fixtures:
  - Queries over the old hours, and across the switch time, answer as the same Logs would from ClickHouse;
  - a late Log of an old hour inserted after the switch is listed with them;
  - Retention deletes old files past it;
  - unprepared DuckDB data stops the start.
- [x] Targeted tests only, `go test -p 1`.

## Comments

- 2026-10-23, review of 08: the prepared-files contract is in [08](08-prepare-clickhouse-command.md)'s Answer, and the fixtures are in `event_store/testdata/prepared_clickhouse/` (static on this branch; see its README). Two points from the review, now in the spec:
  - **Old files are chosen by their time span, not the switch time.** Prepare files the current hour and Logs up to an hour ahead, so a file's `max_time` can be later than the marker's `prepared_at`. A read takes the old files whose `min_time`/`max_time` overlaps its range (a NULL span counts as its whole hour).
  - **Load per marker, not once.** If the install is rolled back to the DuckDB release, it removes the marker on start, and a later upgrade prepares again with a new marker. EventStore records which marker it loaded and reloads, replacing the old tables, when it finds a different one.

## Answer

Built on `clickhouse/09-logs-from-before-the-switch`, from `feature/clickhouse` at 52124d0 and merged with its tip at c0a2c4d (ticket 07). Notes for 10, 11 and 12 (the start decision, how each read uses the old files, every hook 12 deletes, helpers 10 must keep, the test harness) are in `~/.cache/solid-trace/clickhouse-notes/09-notes.md`.

**The module.** Everything about old Log files is in `storage/logs_old_files.go`. The rest calls it through a few hooks:
- `ClickHouseLogs.old` and `ClickHouseColumns.old`;
- ScanLogs, ScanLogsIngestedAfter, LogByID, AttributeCounts, `tableSource`, LogProjects and the two deletes;
- the histogram handler's split.

`(*ClickHouseLogs).UseOldLogFiles` runs at start, in `main` and the root `TestEnv`. It runs before logs.duckdb is opened, which makes the file, and before Log processing's Prepare, which reads the cursors it loads.

**Starting** (migration 901: `old_log_files`, `old_log_promoted_sets`, `old_log_key_counts`, `old_log_marker`):
- **A marker not yet loaded** is loaded. Loading:
  - truncates the three tables and fills them from the prepared Parquet files;
  - raises each Project's Processing cursor in Pebble to the prepared one;
  - then records the marker, its release and `prepared_at`, the switch time.
- **Where the prepared files go.** ClickHouse can't see `prepared/`, so each file is sent with its query as a base64 parameter to `format(Parquet, …)`. A 17 MB file took under a second.
- **A different marker** (a rollback, then a new prepare) is loaded again, replacing what the last one loaded.
- **logs.duckdb with no marker** stops the start with an error that names `prepare-clickhouse`. It does so whether the files were never prepared, or a loaded marker was removed by the DuckDB release starting again.
- **With no DuckDB data**, the start records that, because this release's own `NewLogStore` still makes an empty logs.duckdb. Without the record, the second start would stop.

**Reads.** A read takes the old files whose span (`min_time`/`max_time`, or the hour when NULL) overlaps its range, never cut at the switch time:
- **One table function per (day, local or bucket).** Local copies are read with `file()` under the Log file store, and the others with `s3()`. The bucket's key goes as query parameters, so it is in no statement's text.
- **Each day's promoted set.** It names the day's columns, all Nullable, with `input_format_parquet_allow_missing_columns`.
  - A promoted key reads its column, or `coalesce(column, map[key])` when the key was promoted partway through the day.
  - A column a file lacks reads NULL.
  - `search` reads the map and the promoted string columns.
- **Paths are a brace glob of keys.** Missing files are then skipped, so a read racing Retention doesn't fail, and S3 is never listed.

Each read uses them as follows:
- **Pages:** each span reads the old files in two steps, as DuckDB did. The first step gets times and ids. The second gets those Logs whole from the files that hold them, where `id IN` becomes a PREWHERE. The old Logs are merged newest first with the span's Logs from `logs`.
- **Tables and Charts:** the source query adds `UNION ALL` of each old source, read with the table's select list.
- **The histogram:** an unfiltered by-minute histogram counts its buckets up to the old files' end through the table path and the bin cache, and the rest from the counts by minute.
- **A Log by id:** with a time, that day's files. Without one, the days around the id's time, then every local copy. The DuckDB release also never read the whole bucket for this.
- **Fields:** `old_log_key_counts` is added to the counts.
- **Match monitors (`after_batch`):** they also read the files whose last batch is after the cursor. This wasn't asked for. It keeps a Monitor that last ran before the switch from missing the Logs stored since.

**Old hours' generations** start at 0 and change only when Retention or deletion deletes their files, or a late Log is inserted into them.

**Retention and Project deletion:**
- `LogProjects` adds the Projects that have old rows.
- A deletion lists the days' files from the table, not from memory.
  1. It stops reads using them, and bumps those days' generations.
  2. It deletes the objects through the old S3 adapter, then the local copies and empty folders.
  3. Then it deletes the rows of those days from the three tables.
- A file that fails to delete keeps every row, so the next pass tries again.

**`deploy/install`.** `prepare_duckdb_logs` runs after `set_version`, and only when the release to run has `deploy/clickhouse`. It looks for logs.duckdb (from `logs_duckdb_path` or `duckdb_path` in the config, mapped to `deploy/data`) with no `prepared/marker.json`. If it finds one:
- It picks the newest earlier tag whose `event_store/main.go` ships `prepare-clickhouse`, or stops and says to upgrade through the last DuckDB release first.
- It says what it is doing, stops the services and pulls that tag's EventStore image.
- It runs `compose run --rm --no-deps event_store ./event_store prepare-clickhouse` with that tag, so the command gets event_store's volumes, config and bucket key.
- It stops the install if the command fails or writes no marker.

How it was checked: a harness (`bash -n`, plus eight scenarios with a fake `docker` and a throwaway repository of release tags) covered:
- preparing, from a release tag and from `--build`;
- the marker present, no DuckDB data, and a DuckDB release to run;
- `logs_duckdb_path` set elsewhere;
- the command failing, and no release that ships it.

Untested: a real previous image, `compose run` against the real compose file and data, and `sudo env` passing `SOLIDTRACE_VERSION`. No tag ships `prepare-clickhouse` yet, so on this branch a `--build` over DuckDB data stops with the "upgrade first" message.

**Root tests** (`logs_old_files_test.go`, about 1.5 min, against the dev ClickHouse, MinIO and 08's fixtures).
- **Same answers as ClickHouse.** These tests run every check on two installs and compare the answers. One has the 12 fixture Logs inserted into ClickHouse with their ids. The other reads the prepared fixture: 3 local-and-bucket days and 2 bucket-only hours. Both get the same Logs posted after the switch, including a late Log of an old hour.
  - pages: 15 Queries over 6 ranges, before, across and after the switch, in pages of 2 and 1000;
  - tables: 14 Queries over the same ranges, each run twice so the second comes from the bin cache;
  - histograms: by minute through the split, filtered, and by seconds;
  - a Log by id, with and without its time.
- **Fields** keep the DuckDB release's counts, and Logs inserted since add theirs.
- **Missing column:** a promoted column no file has reads as missing.
- **Match monitors** read the old batches since their last run, then only the new ones.
- **Retention** deletes the 1st's local copies, objects and rows, and its cached bins miss. All 48 hours were hits before. This holds after a restart and another pass.
- **Project deletion** deletes the deleted Project's files.
- **Starting:** unprepared DuckDB data stops the start; a store with no DuckDB data starts again; a rollback stops the start until prepared again; each marker loads once and replaces the last.

**Targeted runs:** after the merge with c0a2c4d, `go test -p 1 . ./storage ./handler` passes: the root package in 8.3 min, storage and handler in seconds. Before the merge, the only failures were six of the DuckDB SQL mode's tests, which already failed on 52124d0; 07 replaced them.

**For 10:**
- **Keep these helpers when deleting the DuckDB files.** The module uses `logObjectPrefix`, `LogDay`, `dayString`, `AttributeType`, `latestTime` and `earliestTime`, and the S3 adapter (`NewObjectStore`).
- **logs.duckdb's location is still needed.** `main` still needs where logs.duckdb is, and `prepared/` beside it, after `duckdb_path` and `logs_duckdb_path` become ignored keys.
- **The dev stack won't start.** Its `storage/duckdb/development/logs.duckdb` has no marker, so EventStore now refuses to start there. Run `prepare-clickhouse` from `feature/prepare-clickhouse` on it with the stack stopped, or move the file aside, before the Playwright flows.

**Not covered:**
- **The map side of a partway key.** The fixture's maps hold no promoted key's values, so `coalesce(column, map[key])` is never shown taking a value from the map.
- **Cursor durability.** Cursors are written to Pebble without a sync, before the marker is recorded.
