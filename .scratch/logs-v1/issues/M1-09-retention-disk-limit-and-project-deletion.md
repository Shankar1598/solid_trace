# M1-09: Retention, disk limit and Project deletion

**What to build:** Logs never fill the disk and never outlive their Project. Each Project's Logs are deleted past its Retention, an instance-wide disk limit deletes the oldest Logs first, and a deleted Project's Logs go with it. Spec: [Log storage](../spec.md#log-storage-eventstore) (Retention, Disk limit, Project deletion); [Further Notes](../spec.md#further-notes); user stories 18, 29–33.

**Blocked by:** M1-02, M1-03, M1-08.

**Status:** resolved

- [x] A daily pass inside EventStore deletes each Project's `day=` folders past its effective Retention (read from Console's SQLite), with those days' promoted-set and popularity rows.
- [x] Normalisation's "older than Retention" time rule uses the Project's effective Retention instead of the fixed default.
- [x] One instance-wide disk limit for Log files; when exceeded, the oldest hour files are deleted first, from any Project. Where the limit is configured and its default (for example a share of free disk) are the implementer's call; record the choice in this ticket's Comments.
- [x] **Project deletion is detected when a Project's row disappears from Console's `projects` table** (not when its keys vanish: keys are hard-deleted and a Project can briefly have none). EventStore then drops its hot table, its `logs_late` rows, its folders and its promoted-set and count rows. Console has no Project destroy route today, so this path is exercised by tests only.
- [x] Tests (controllable clock): Retention deletes the right days for the default and an override; the disk limit deletes the oldest hours across Projects; a removed `projects` row deletes that Project's Logs and leaves others; deleting a Project's last key deletes nothing.

## Comments

### 2026-10-03 built (logs-v1/m1-09)

What was built:
- **Retention at ingest:** `storage.ConsoleSettings` is now `ingest.Config.Retention` in `main.go` and the test env, in place of `FixedRetention(30)`. It has no cache. Every Log request reads SQLite, as `ValidateKey` already does. A cache is still deferred with the `ValidateKey` cache.
- **`ConsoleSettings`:**
  - `ProjectLogRetention() (map[uint32]int, error)` gives every row of `projects` with its effective Retention. A Project missing from the map is deleted.
  - A Retention below 1 day (only Rails validations prevent one) is now an error from `LogRetentionDays`, so ingest answers 500 for that Project. In the map it shows as `0`, so the pass skips that Project and doesn't take it for deleted.
- **`LogStore`** (`storage/logs_retention.go`, `storage/logs_disk_limit.go`):
  - `LogProjects()` lists the Projects with any Log data: a hot table, `logs_late` rows, sets, counts or a `project=` folder.
  - `DeleteLogDaysBefore(project, day)` deletes the days before `day`: the `day=` folders, set and count rows, `logs_late` rows, and hot-table rows, which only a day of failing exports can leave behind.
  - `DeleteProjectLogs(project)` drops the hot table and deletes the `logs_late`, set and count rows and the `project=` folder, then runs `CHECKPOINT logs`.
  - `EnforceDiskLimit(LogDiskLimit{Bytes})` deletes whole hour folders, oldest hour first, with ties broken by Project id.
  - All of them hold `writeMu`. They commit and delete files under `publishMu`, as the export's commit and renames do, and evict `promotedSetCache` there too (`deleteBefore`, `deleteProject`).
- **`logprocessing.Cleanup`** (`logprocessing/cleanup.go`):
  - `DeleteExpiredLogs(now)` is the daily pass. A Project with no `projects` row gets `DeleteProjectLogs`; any other gets `DeleteLogDaysBefore(LogDay(now − Retention days))`.
  - The timer runs at start, then 5 minutes past each hour start, so after the hourly export. Expired Logs are deleted once per UTC day, and the disk limit is enforced every hour. A failed run is retried a minute later. `main.go` starts and stops it.
- **Test env:** `env.LogCleanup` and `env.DeleteExpiredLogs(t)`. Tests are in `logs_retention_test.go`. They seed `projects` rows explicitly with `seedProject` and edit Console with `consoleExec`.

Choices the ticket left open:
- **A day goes only once it lies wholly past Retention.** That means days before `LogDay(now − N days)`. At noon on 2 Oct with 30 days, 1 Sep goes and 2 Sep stays, because ingest still accepts Logs from 2 Sep 12:00. So up to N+1 days' folders exist at once.
- **Disk limit.** It is set in EventStore config, `logs_disk_limit_gb` (GB = 10⁹ bytes), not in Console: it is about EventStore's disk. The default (unset) lets Log files grow until the disk holding them has 10% of its space left: limit = Log files + free − 10% of disk, measured each hour with `statfs`. If other data fills the disk, Logs make way.
  - Only published `*.parquet` files count. Logs not yet exported are never deleted.
  - Whole hour folders go (`data.parquet` and its late files together). A day folder goes when its last hour does.
  - The disk limit leaves set and count rows; Retention deletes them later.
- **The Log processing cursor survives Project deletion.** That way a batch the Project already stored, still in Pebble, is never stored again. A batch processed after the pass recreates the hot table, and the next pass deletes it again. Project ids aren't reused (SQLite AUTOINCREMENT).
- **A Project with an invalid Retention is skipped and logged**, never treated as deleted.

Deviations: none from the spec.

Follow-ups:
- **M2-01:** as M1-08 said, take `publishMu` for reading around file reads. Retention, Project deletion and the disk limit delete files under its write lock. After the disk limit, a day can have a promoted set but no files. Readers must expect `Hour()` to return nothing.
- **M1-10:** measure `DeleteProjectLogs`' `DROP TABLE` and `CHECKPOINT` on a large hot table, and the hour-folder walk (`hourDirs` stats every file each hour: about Projects × 24 × Retention files).
- **Console:** a Project destroy route, when one is added, needs nothing from EventStore: the next daily pass deletes the Logs. It runs once a UTC day and at startup.
