# 03: The hourly export's COPY outside the write lock

**Status:** resolved

**What to do:** Idea 1b of [performance-ideas.md](../research/performance-ideas.md), taken in [ticket 01](01-review-performance-findings.md). Write the export's files without holding `writeMu`, so that Log processing no longer stops for the whole export every hour. Keep the files' sort (`ORDER BY time, id`), which Log read speed 02's newest page relies on.

## What production shows (2026-10-21)

- Every hour, `Log processing is behind` fires for both Projects at hh:01 to hh:04, `behind` 1m53s to 4m13s. It fires the moment the export releases `writeMu`, just as the first file's upload, which waited on the same lock, logs `Uploaded a Log file to the bucket`.
- The tagalys Project's hour files are 550 to 640 MB each (about 12M Logs an hour).

## Design

`ExportLogs` takes a new `exportMu`, so exports never overlap. Per Project:

1. **Stage, outside `writeMu`.** One DuckDB transaction that only reads: it reads the Project's Log processing cursor first, which fixes its snapshot, then COPYs each hour to its staged file as before, syncs it and reads its stats. Then it rolls back.
2. **Commit, under `writeMu`.** A second transaction inserts the files' manifest rows, drops demoted columns (re-reading the hot table's columns), and deletes the Logs before the cutoff whose `batch` is at or below the staged cursor (or NULL, from before the batch column). It commits under `publishMu` and renames the files, as before.

Why the batch condition is exact:
- Within a Project, Log processing stores batches in id order and commits the cursor with them (as `ScanLogsIngestedAfter` already relies on).
- So every Log in the snapshot has a batch at or below the cursor, and every Log stored after it has a higher one.
- Log processing never stores an hour before the hot window in the hot table, so what it stores meanwhile below the cutoff is late Logs. They stay in `logs_late` for the next export's late file, and the same goes for hot Logs after a clock step back.

The commit checks that it deleted exactly as many Logs as the files hold. Only a deletion can change that meanwhile, such as the Project's deletion or Retention. On a mismatch, it rolls back and removes the staged files, and the next export tries again.

The CHECKPOINT after the export stays under `writeMu`. DuckDB 1.5 can't take appends during a checkpoint, and production's full row groups make it cheap.

Each Project's export now logs `Exported a Project's Logs` (`logs`, `files`, `copy`, `commit`), and the checkpoint logs `Checkpointed logs DuckDB after the hourly export` (`took`), so production shows where the time goes.

No ADR changes. ADR 0007's commit marker still holds: the manifest row is inserted in the transaction that deletes the file's Logs.

## Acceptance

- [x] Log processing's longest wait during an export is a fraction of the export, in a probe (`event_store/bench/logexport/`, under a minute, 2 CPUs), before and after
- [x] No Log lost or kept twice when Log processing stores late and hot Logs while the files are written (storage test)
- [x] A Project deleted while its files are written gets no files, and leaves no staged file behind (storage test)
- [x] The root export tests pass: late files, demoted columns at 01:00, a crash between staging and rename, lists the same before and after

## Comments

### Built (2026-10-21, `feature/export-outside-write-lock`)

**Probe figures.**
- The probe is `event_store/bench/logexport/`: an hour of 300,000 Logs to export, and 600,000 in the hour after.
- A writer appends batches of 200 Logs every 20 ms, as Log processing does, while the export runs. One in ten batches is late, in the exported hour.
- Each run was pinned to 2 CPUs (`taskset -c 0,1`), twice per setting.
- Before is `main`'s export (`e434fe4`), in a worktree.

| DuckDB threads | | Export | Longest append wait | Median wait | Appends during the run |
|---|---|---|---|---|---|
| 2 | before | 3.6–3.7 s | 3.6–3.8 s | 28–29 ms | 38–39 |
| 2 | after | 4.8–5.2 s (COPY 4.5–4.8 s, commit 0.2 s) | 0.38–0.46 s | 36–39 ms | 89–93 |
| 8 | before | 4.8–5.5 s | 4.8–5.5 s | 29–34 ms | 35–37 |
| 8 | after | 5.6–5.8 s (COPY 5.3–5.5 s, commit 25 ms) | 0.49–0.52 s | 52–53 ms | 75–76 |

- Before, an append waits for the whole export. After, it waits at most half a second, about as long as one of Log processing's own checkpoints.
- The export itself takes about 1.3 s longer, as its COPY now shares the 2 CPUs with the appends it no longer blocks.
- Every run kept every Log: the files, the hot table and `logs_late` hold all of them between them.
- Production's hours hold about 40 times the probe's Logs. That fits its stall of 2 to 2.5 minutes. After this change, the stall should be the commit and the checkpoint.

**The checkpoint after the export.**
- The probe first built its hot table with large INSERTs, which leave part-filled row groups.
- After the delete, DuckDB's vacuum merged up to four of those row groups, rewriting 300,000–350,000 rows: the checkpoint took 4–4.7 s.
- With full row groups, as Log processing's small commits make them, the same checkpoint took 15 ms. So the probe now rewrites its table sorted, which fills its row groups.
- Production's row groups should be full, so its checkpoint stays cheap under `writeMu`. If production's `Checkpointed logs DuckDB after the hourly export` line shows seconds, the vacuum is the first suspect.

**Tests.**
- New storage tests:
  - `TestLogsStoredWhileAnExportWritesItsFilesStayForTheNextExport`;
  - `TestAProjectDeletedWhileAnExportWritesItsFilesGetsNone`.
- Both fail with the batch condition and the count check taken out (checked by mutation).
- They change Logs through an unexported hook, `LogStore.staged`, which runs while the export's read transaction is still open.
- The storage test helper `appendLogs` and the object-store probe now make batch ids with `uuid.NewV7`. Random ids broke the order that Pebble guarantees, which the delete relies on.
- Passed:
  - the targeted storage tests;
  - 63 root tests: export, manifest, late files, demoted columns, crash between staging and rename, uploads, the bin cache, Retention, deletion, ingested-after reads, newest pages across a clock step back, queries and the Log transform;
  - `go vet`.
- The full Go suite is for the user.

**What production will show.** After release, `Exported a Project's Logs` (`copy`, `commit`) and `Checkpointed logs DuckDB after the hourly export` (`took`) give the real split. `Log processing is behind` should stop firing at hh:01 to hh:04.
