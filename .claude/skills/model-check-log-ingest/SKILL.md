---
name: model-check-log-ingest
description: "TLA+ model, checked with TLC, of EventStore's Log path: ingest into Pebble before the 200, Log processing into ClickHouse (record the range, insert with its dedup token, move the cursor, delete the batches), crashes, power loss, and the Match monitor's scan of the Logs ingested after a batch. Use when a change touches event_store/logprocessing/processor.go, storage/pebble_logs.go, storage/logs_clickhouse.go's insert, ScanLogsIngestedAfter, or the Pebble or ClickHouse sync settings, or to find out whether a change can lose a Log, store it twice, or let a Match monitor read it twice."
---

# Model-check the Log ingest path

A test runs the crash points that someone wrote down. This model lets TLC try **every order** of ingest, Log processing, a Match scan, a process crash, a power loss and the OS's writeback, in a small scope. Every action in `specs/LogIngest.tla` points to the Go statement that it stands for. If an action is one step in the model, it is one call in the code.

## When to use it

Use it when a change touches a file or function in the coverage record below. To see what changed since the sync commit, committed or not:

```sh
git diff --stat 0f95dbb5 -- event_store/logprocessing/processor.go event_store/storage/pebble_logs.go event_store/storage/logs_clickhouse.go event_store/storage/logs_clickhouse_read.go event_store/handler/logs.go event_store/ingest/logs.go
```

Changes to normalising, the Log transform, Attributes, the key counts and the reads other than `ScanLogsIngestedAfter` are outside the model.

## Run it

From the repo root:

```sh
.claude/skills/model-check-log-ingest/bin/check      # 4 configs x 6 rules: about 1 minute
.claude/skills/model-check-log-ingest/bin/mutants    # each rule breaks on a broken variant: about 15 s
```

- **`bin/check`:** checks each rule of each `specs/*.cfg` alone, and compares the results with `specs/expected.txt`.
  - Exit 0: every result is as recorded.
  - Exit 1: a result is different.
  - Exit 2: TLC could not run, or the model has an error.
  - TLC's output for each rule is in `~/.cache/solid-trace/model-check-log-ingest/<time>/<config>-<rule>/tlc.out`. `bin/check LogIngest` checks only that config.
- **`bin/mutants`:** for 6 broken variants of the code, it changes a copy of the model in the same way. Each variant must break its rule. Run it after each change to the model or a rule.
- **`COVERAGE=1 bin/check`:** also names each action that TLC never takes in a config. Only an action that the config turns off may be in that list, such as `Expire` where nothing can expire. Any other action in it is a model error, such as an `UNCHANGED` that conflicts with the action's own change. Run it after each change to a model.

The first run installs Java (`java@temurin-25`) and TLC (`tla2tools.jar` 1.7.4, pinned by its sha256) with mise. Only that first run needs the network. The model-check-monitors skill uses the same tools.

**Read every result with the pstack `/statechart` skill.** It shows each rule that breaks as its shortest path in plain words. Give it `specs/LogIngest.tla` and the config of the result. The state count is above its chart budget, so it draws only the paths to the rules that break.

## The rules

| Rule | In plain words | The harm when it breaks |
| --- | --- | --- |
| `NoLogStoredTwice` | Each Log is in ClickHouse at most once. | Counts, charts and Monitors count the Log twice. |
| `NoLoggedLogLost` | Each Log whose batch reached Pebble's disk is in ClickHouse, or still in Pebble for Log processing. | A Log that Pebble kept is lost. This is what "Pebble in front of the 200" promises. |
| `NoAcceptedLogLost` | Each Log answered 200 is in ClickHouse, or still in Pebble. | A sender's Log is lost after its 200. |
| `MatchReadsEachLogOnce` | A Match monitor reads each Log once. | The same Logs are in two `monitor_matched` Notifications. |
| `MatchMissesNoLog` | A Match monitor has read each Log before its `ingest_cursor`, of the batches that reached Pebble's disk. | A match is never sent. |

The 4 configs set the environment that the code can meet:

| Config | Assumption |
| --- | --- |
| `LogIngest` | EventStore's process can die once. A ClickHouse insert can fail before it writes, or write and then answer an error. |
| `LogIngestPowerLoss` | Also, the machine can lose power once (a hard reset, a kernel panic). ClickHouse does not sync its parts. As far as we know, this is its default: the MergeTree settings `fsync_after_insert` and `fsync_part_directory` are 0, and the repo sets neither. |
| `LogIngestPowerLossSynced` | The same, but ClickHouse syncs each part before it answers. |
| `LogIngestWindowPassed` | A range's dedup entry can leave `non_replicated_deduplication_window` (`logsDedupWindow`, 10,000 blocks) before the range goes again. |

## Findings at the sync commit

`specs/expected.txt` records each of these as `breaks`. A line marked L is a finding. A line marked D is a cost that the code accepts by design. A fix makes its lines hold, and `bin/check` then reports `NOT AS EXPECTED`. Change those lines to `holds`, and update this list.

- **L1. A power loss can lose Logs that Pebble already counts as stored. Run: `LogIngestPowerLoss`.**
  - `InsertLogBatches` returns when ClickHouse has written the part to the page cache, not to the disk.
  - `SetLogProcessingCursor` and `DeleteLogBatchesUpTo` are NoSync. But the next range's `RecordLogInsert` (Sync) puts them on disk about a second later, while the kernel can keep ClickHouse's dirty pages for up to about 30 s.
  - Path:
    1. A batch is recorded and inserted.
    2. The cursor moves past it, and that write reaches the disk.
    3. The power fails: ClickHouse loses the part.
    4. `Prepare` deletes the batch at or below the cursor. The Logs are then in neither store (`NoLoggedLogLost`), and a Match monitor misses them (`MatchMissesNoLog`).
  - In `LogIngestPowerLossSynced`, both rules hold. So a ClickHouse that syncs its parts closes this loss. Check the ClickHouse 26.8 settings, and measure the insert cost, before you act on it.
- **D1. A power loss can lose a Log after its 200. Runs: both power-loss configs.** `WriteLogBatch` writes with `pebble.NoSync`, and its comment says: "a process crash loses nothing". A Log that is not on disk yet is lost on a power loss. This is the code's choice, so it is not a finding.
- **D2. If a range goes again after its dedup entry left the window, its Logs are stored twice. Run: `LogIngestWindowPassed`.**
  - This needs more than 10,000 blocks inserted into `logs` between a try and its retry.
  - The `logsDedupWindow` comment gives the reason that this does not happen: "A range is inserted again within seconds, while each Project inserts about once a second." That holds while the retry backoff (at most 30 s) times the busy Projects stays well below 10,000.
  - Keep that true when you change `maxBackoff`, `logsDedupWindow`, or how often each Project inserts.

L1 and D1 are accepted: they are the cost of no fsync, which the code chooses for its insert and ingest speed.

**Fixed: L2, in `0f95dbb5`.** After a power loss, the Log processing cursor's NoSync write can go back after a scan gave it to Console. `ScanLogsIngestedAfter` then answered that older cursor as `ingest_cursor`, and the next run read the batches between again. It now answers `after` when the cursor is behind it, and `MatchReadsEachLogOnce` holds in both power-loss configs. Variant M6 in `bin/mutants` puts the old answer back, and the rule must break. The Go test is `TestAMatchMonitorsCursorNeverMovesBackWhenTheProcessingCursorDoes`.

## When a result is not as expected

1. **Read the path with `/statechart`.** Then do each step of it in the code, at the function that the action's comment names.
2. **It is a real bug if each step is a step that the code can take,** in that order. A power-loss or window config counts, because its assumption is real. Report the bug with the path in the code's words. Do not weaken the rule or the model to make it pass. Do not change product code unless the user asks.
3. **It is a modeling error if a step cannot happen in the code.** For example, two calls that are one in the code, a write that the code syncs, or a guard that is missing. Fix the model, then run `bin/check` and `bin/mutants`.
4. **ERROR (exit 2):** read `tlc.out`. "not completely specified" means that an action leaves a variable out of its `UNCHANGED`. A deadlock means that the `Done` step does not cover a correct end.
5. **A run takes much longer than a minute:** the model has an unbounded loop. Every counter (`made`, `fails`, `scans`, `crashes`, `losses`, `forgotten`) must have a bound, also when a change removes a guard such as the dedup token.

## When a change to the covered code is intended

1. Find the actions in the coverage record that the change touches. Change them to do what the new code does, one action for each call. A sync change is a change to `durable`: only a Sync write moves it in the code's own steps.
2. Run `bin/check`. A new `breaks` is a bug in the change, unless you can show that the model is wrong. A finding that now holds means that the change fixed it: update `specs/expected.txt` and the findings above.
3. Run `bin/mutants`. Update a variant in `MUTANTS` in `bin/mutants` when you change the model text that it replaces. A new rule needs a variant too.
4. Move the sync commit (in this file and in the `git diff` command) only after you confirm that each action describes the code at the new commit.

## Coverage record

**Sync commit:** `0f95dbb5` (2026-11-22). At that commit, each action below describes the code that it names.

| Model action | Code |
| --- | --- |
| `Ingest` | `ingest/logs.go` `storeLogs` → `PebbleWriter.WriteLogBatch` (`pebble.NoSync`), then the 200 |
| `Take` | `logprocessing.Processor.storeLogs`: `LogProcessingCursor`, `RecordedLogInsert`, `LogBatchesAfter`, the `again` / `upTo` logic, `RecordLogInsert` (`pebble.Sync`) |
| `Insert` | `ClickHouseLogs.InsertLogBatches` → `insertLogs`, with `LogInsert.token` as `insert_deduplication_token`; `processProject` / `retryLater` on an error |
| `SetCursor` | `PebbleWriter.SetLogProcessingCursor` (`pebble.NoSync`) |
| `Delete` | `PebbleWriter.DeleteLogBatchesUpTo` (`pebble.NoSync`) |
| `Restart` | `Processor.Prepare` |
| `Scan` | `ClickHouseLogs.ScanLogsIngestedAfter` (it answers `after` when the cursor is behind it), `handler/logs.go` (`ingest_cursor`), and Console's `MonitorEvaluation#run_match`, which keeps it |
| `Forget` | `logsDedupWindow` (`non_replicated_deduplication_window` of `logs`) |
| `PowerLoss`, `WritebackWal`, `WritebackPart` | Pebble's WAL replay up to the first missing record; ClickHouse's parts without `fsync_after_insert` |

## Scope

The checks cover the model in these bounds: one Project, 2 Log batches of one Log each, at most 2 batches in an insert, 3 Match scans, 2 inserts that answer an error, one process crash, one power loss, and one forgotten dedup entry. A pass covers only these bounds.

The model leaves out:
- The Log transform, content errors and `storeApart`.
- Parts of more than one day.
- The `log_key_counts` and Source counts tables, which have their own dedup logs with the same token.
- Retention, Project deletion and the disk limit.
- Other Projects.
- The id floor (`KeepLogBatchIDsAbove`).
- Console's own database on a power loss.

The process-crash rule rests on the code's claim that a NoSync write survives a process crash.
