# M1-08: Late Logs and the hourly export

**What to build:** Logs move from the hot tables to hourly Parquet files on local disk, late Logs have a home, and queries return the same rows before and after an hour is exported. Spec: [Log processing](../spec.md#log-processing-eventstore) step 3; [Log storage](../spec.md#log-storage-eventstore) (`logs_late`, Promotion: demoted columns, Hourly export); user stories 36, 37.

**Blocked by:** M1-07.

**Status:** ready-for-agent

- [ ] A Log whose `time` is outside the hot window (start of the previous hour to start of the next, by the controllable clock) goes to the shared `logs_late` table (same fixed columns, maps only, never promoted).
- [ ] A timer inside EventStore runs at each hour start for hour h-2: per Project, one zstd Parquet file under `project=<id>/day=<d>/hour=<h>/data.parquet`, 30,720-row row groups, sorted by time, with that day's promoted columns; `logs_late` rows for that hour and Project join it.
- [ ] `logs_late` rows for hours already exported become `late-<n>.parquet` in that hour's folder, with the day's promoted columns (values pulled out of the maps).
- [ ] The exported rows are deleted from the hot table and `logs_late`, then a checkpoint runs. Files are staged under a temporary name and renamed after the delete commits; startup recovers or removes staged files, as the Event archive does.
- [ ] Demoted columns are dropped at the export that deletes the previous day's last hour (01:00 UTC), in the same transaction.
- [ ] The latest-N query reads the hot table, the Project's Parquet files and its `logs_late` rows, so its result doesn't change across an export.
- [ ] Tests: late and future Logs are queryable; a query returns the same rows before and after the hourly export and after a late file is written; a crash between staging and rename leaves no half-written file and loses no Log; demoted columns are dropped at 01:00 and their values still read back.
