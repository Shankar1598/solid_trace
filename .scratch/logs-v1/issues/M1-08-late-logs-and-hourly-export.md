# M1-08: Late Logs and the hourly export

**What to build:** Logs move from the hot tables to hourly Parquet files on local disk, late Logs have a home, and queries return the same rows before and after an hour is exported. Spec: [Log processing](../spec.md#log-processing-eventstore) step 3; [Log storage](../spec.md#log-storage-eventstore) (`logs_late`, Promotion: demoted columns, Hourly export); user stories 36, 37.

**Blocked by:** M1-07.

**Status:** resolved

- [x] A Log whose `time` is outside the hot window (start of the previous hour to start of the next, by the controllable clock) goes to the shared `logs_late` table (same fixed columns, maps only, never promoted).
- [x] A timer inside EventStore runs at each hour start for hour h-2: per Project, one zstd Parquet file under `project=<id>/day=<d>/hour=<h>/data.parquet`, 30,720-row row groups, sorted by time, with that day's promoted columns; `logs_late` rows for that hour and Project join it.
- [x] `logs_late` rows for hours already exported become `late-<n>.parquet` in that hour's folder, with the day's promoted columns (values pulled out of the maps).
- [x] The exported rows are deleted from the hot table and `logs_late`, then a checkpoint runs. Files are staged under a temporary name and renamed after the delete commits; startup recovers or removes staged files, as the Event archive does.
- [x] Demoted columns are dropped at the export that deletes the previous day's last hour (01:00 UTC), in the same transaction.
- [x] The latest-N query reads the hot table, the Project's Parquet files and its `logs_late` rows, so its result doesn't change across an export.
- [x] Tests: late and future Logs are queryable; a query returns the same rows before and after the hourly export and after a late file is written; a crash between staging and rename leaves no half-written file and loses no Log; demoted columns are dropped at 01:00 and their values still read back.

## Comments

### 2026-10-03 built (logs-v1/m1-08)

What was built:
- **`logs_late`** (`storage/logs_duckdb.go`): one table, `project_id` plus a hot table's fixed columns. `AppendLogBatches` (and so `TryLogBatches`) splits the Logs by `storage.HotWindow(now)`: the hot Logs are promoted, appended and counted as before; the rest go to `logs_late`, maps only.
- **Hourly export** (`storage/logs_export.go`): `LogStore.ExportLogs(now)`. Per Project, one transaction: stage each hour's file with `COPY … ORDER BY time, id` (zstd, `ROW_GROUP_SIZE 30720`), `fsync` it, drop demoted columns, delete the rows, then commit and rename. Then `CHECKPOINT logs`. A failing Project is logged and skipped; its Logs wait for the next run.
- **Timer** (`logprocessing/export.go`): `HourlyExport` runs `ExportLogs` once at start (catch-up after downtime), then at each hour start by its clock, or a minute after a failure. `main.go` starts it, and stops it after Log processing.
- **Recovery**: `NewLogStore` and every export first settle staged files, as the Event archive does. A file whose first Log is still in the hot table or `logs_late` is deleted, otherwise renamed. An unreadable file is left and logged.
- **Latest-N** reads the hot table, `logs_late` and the Project's files (`read_parquet(…, union_by_name, hive_partitioning = false)`), `UNION ALL BY NAME`, so promoted columns are merged back by name whichever source a Log comes from.
- **Config**: `logs_parquet_path`, default `logs_parquet` next to `duckdb_path` (local disk, not `parquet_storage_path`'s cold storage). `NewLogStore(events, path, filesDir)`.
- **Test env**: `env.ExportLogs(t)` runs the export at the clock's time. Tests are in `logs_export_test.go`.

For later tickets (layout and lookup):
- **`storage.LogFiles`** (`storage/logs_files.go`, `LogStore.Files()`) owns the layout: `<dir>/project=<id>/day=<YYYY-MM-DD>/hour=<HH>/data.parquet`, then `late-1.parquet`, `late-2.parquet` …; staged names end `.tmp`.
  - `ProjectDir`, `DayDir`, `HourDir` give the folders (M1-09 deletes `DayDir`s and hour files).
  - `Hour(project, t)` answers "which files hold Project P's hour H": `data.parquet`, then late files in order, never staged files. `Project(project)` lists all, oldest hour first.
  - Every Log in an hour folder's files has a time in that hour, so a reader can pick hours by folder alone.
- **Columns of a file** are exactly its day's promoted set (`PromotedSets`), every value of a set key in its column for Logs that came late, and as the hot table left them for the rest (so `Partway` keys still need the coalesce). No footer reads needed.

Choices the ticket left open:
- **The export takes every hour before the hot window, not only h-2.** Hours h-2 and earlier, from the hot table and `logs_late`. An hour without `data.parquet` gets it, one with it gets the next late file. This covers hours missed while EventStore was down, late Logs of hours the Project never had a file for, and a Log processing commit whose `now` was read just before the export ran.
- **Late Logs don't count towards popularity.** Their days' sets are already made (or the day hasn't started), and a backfill of old Logs shouldn't pick today's columns.
- **The lock is inside `LogStore`**, not in the processor: `writeMu` is held by each commit, each try and each Project's export, so never across Log processing's waits, and `logprocessing.Store`/`flakyLogStore` didn't change. A second lock, `publishMu`, is a read lock for readers (latest-N) and is held by the export only around its commit and renames. Without it, a reader could see an hour twice or not at all. M2-01's reads should take it too (add a method on `LogStore` rather than exporting the mutex).
- **Demotion rule.** At each export, a promoted column is dropped if no set of the days the hot table keeps after the cutoff has it, nor the sets of the hot window's days. In practice that's only at 01:00. Only then does the export scan the table's days.
  - **DuckDB 1.5.6 can't commit a `DELETE` followed by an `ALTER TABLE` of the same table** in one transaction ("another transaction has altered this table"). So the drops come before the delete, still in the same transaction.
  - A Project with no Logs in the previous day's last hour gets its drop at its next export with Logs.
- **Staged files are fsynced before the commit**, so a committed export's files survive a power loss before their rename. If `COMMIT` itself fails, the staged files are left for recovery, since the commit may have happened.
- **Late file numbers** are one more than the highest published `late-<n>.parquet` in the folder.
- **The crash test** simulates both crashes on disk, with no seam in production code. Before the commit: it restores a copy of `logs.duckdb` from before the export and renames the files back to `.tmp`. After the commit: it renames the files back to `.tmp`.
- **Demotion test**: tests can't see columns, so it checks values read back across the 01:00 export, later Logs and a late Log of the previous day. A debug log line (`Dropping demoted Log column`) confirmed, while it was built, that k090…k099 were dropped once, at 01:00.

Deviations: none from the spec.

Follow-ups:
- **M1-09:** Retention and Project deletion delete `LogFiles.DayDir`/`ProjectDir`, `logs_late` rows (`project_id`), and the hot table. Hold `writeMu` (go through a `LogStore` method) so they don't race an export. Recovery parses the Project from the `project=<id>` folder. The disk limit can rank hour files across Projects by their `day=`/`hour=` folders.
- **M2-01:** read hours newest first through `LogFiles.Hour`; take `publishMu` for reading. Latest-N today unions all of a Project's files in one query (DuckDB's top-N prunes row groups by time stats); the hour-by-hour scan is M2-01's.
- **M1-10:** the export holds `writeMu`, so Log processing waits, for each Project's COPY, delete and commit. Then comes a `CHECKPOINT logs`, outside the per-Project transactions but still under the lock. Measure both at scale.

