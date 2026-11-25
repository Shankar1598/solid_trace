---
name: model-check-monitors
description: "TLA+ models, checked with TLC, of Console's Monitor runs (MonitorEvaluation: the run lease, the version check, the Match cursor) and of Notification delivery (the outbox, the per-Integration tick, the event threshold check, and their Solid Queue limits_concurrency). Use when a change touches monitor_evaluation.rb, integration_notification.rb, notification.rb, log_monitor.rb's lease or cursor, or the Monitor and Notification jobs, or to find out whether a change can send a Notification twice, out of order, or never."
---

# Model-check the Monitors and their Notifications

Tests check the orders of steps that someone wrote down. These models let TLC try **every order** of the steps of 2 Monitor runs, or of the ticks of an Integration, in a small scope. Every action in a model points to the Ruby statement that it stands for. If an action is one step in the model, it is one statement or one transaction in the code.

## When to use it

Use it when a change touches a file or function in the coverage record below. To see what changed since the sync commit, committed or not:

```sh
git diff --stat 3c96a93e -- console/app/services/monitor_evaluation.rb console/app/services/integration_notification.rb console/app/models/notification.rb console/app/models/log_monitor.rb console/app/jobs/monitor_evaluation_job.rb console/app/jobs/monitor_scheduler_job.rb console/app/jobs/integration_notification_processor_job.rb console/app/jobs/event_threshold_job.rb console/app/jobs/integration_notification_job.rb console/app/controllers/monitors_controller.rb console/app/mcp/mcp_tools/run_monitor.rb console/Gemfile.lock
```

Changes to messages, payloads, Query reading, groups and thresholds are outside the models. In `Gemfile.lock`, only a change of `solid_queue` matters.

## Run it

From the repo root:

```sh
.claude/skills/model-check-monitors/bin/check      # every config, every rule: about 20 s
.claude/skills/model-check-monitors/bin/mutants    # each rule breaks on a broken variant: about 10 s
```

- **`bin/check`:** checks each rule of each `specs/*.cfg` alone, and compares the results with `specs/expected.txt`.
  - Exit 0: every result is as recorded.
  - Exit 1: a result is different.
  - Exit 2: TLC could not run, or the model has an error.
  - TLC's output for each rule is in `~/.cache/solid-trace/model-check-monitors/<time>/<config>-<rule>/tlc.out`. The last line of the run gives that folder.
  - `bin/check DeliveryLong` checks only that config.
- **`bin/mutants`:** for 7 broken variants of the code, it changes a copy of the model in the same way. Each variant must break its rule, or the rule checks nothing. Run it after each change to a model or a rule.
- **`COVERAGE=1 bin/check`:** also names each action that TLC never takes in a config. Only an action that the config turns off may be in that list, such as `Expire` where nothing can expire. Any other action in it is a model error, such as an `UNCHANGED` that conflicts with the action's own change. Run it after each change to a model.

The first run installs Java (`java@temurin-25`) and TLC (`tla2tools.jar` 1.7.4, pinned by its sha256) with mise. Only that first run needs the network.

**Read every result with the pstack `/statechart` skill.** It shows each rule that breaks as its shortest path in plain words. When the rules hold, it draws how the model moves between states. Give it the `.tla` file and the config of the result.

## The rules

Each rule is an INVARIANT. The harm follows each one.

| Model | Rule | In plain words | The harm when it breaks |
| --- | --- | --- | --- |
| MonitorRuns | `NoLogMatchedTwice` | Each matching Log is in one `monitor_matched` Notification. | The team gets the same Logs twice. |
| MonitorRuns | `NoLogMissed` | Each Log before the cursor was in a `monitor_matched` Notification. | A match is never sent. |
| MonitorRuns | `NoStaleWrite` | No run writes what it found over an older range than the run before it. | A Threshold group goes back in time: it resolves, then fires again, and pages again. |
| MonitorRuns | `NoWriteOverASave` | A run that loaded the Monitor before a save writes nothing that it found. | The run undoes `resolve_groups` after a person changed or turned off the Monitor. |
| MonitorRuns | `OneRunAtATime` | At most one run reads EventStore for a Monitor at a time. | Two runs do the work of one, and their writes can cross. |
| Delivery | `SentAtMostOnce` | The provider gets each Notification once. | A duplicate Slack message, email or PagerDuty event. |
| Delivery | `ResolveAfterTrigger` | A resolve never goes before the trigger that it closes, while that trigger can still go. | PagerDuty gets a resolve, and then a trigger that stays open. |
| Delivery | `NoRowLeftBehind` | When no run and no tick has work left, each row has its outcome: sent, skipped, or failed for the last time. | A page waits for ever. |
| EventThreshold | `OneThresholdRow` | An Integration gets one threshold reached Notification for an Issue in a window. | One message for each Event that arrived together. |

Each config sets the environment that the code can meet:

| Config | Assumption |
| --- | --- |
| `MonitorRuns` | Runs end within `RUN_LEASE` (5 minutes), as the code assumes. |
| `MonitorRunsSlow` | A run can take longer than `RUN_LEASE`, and another run then takes the Monitor. |
| `Delivery` | Each tick ends within 2 minutes of its dispatch, and no process dies. |
| `DeliveryLong` | A tick can wait or run longer than 2 minutes, which `limits_concurrency duration:` allows. |
| `DeliveryTickDies` | A tick's process can die while it delivers. |
| `DeliveryWriterDies` | A Monitor run's process can die after its commit, before `schedule_ticks`. |
| `EventThreshold` | Each job ends within 2 minutes of its dispatch. |
| `EventThresholdLong` | A job can wait in the queue, or run, for longer than 2 minutes. |

## Findings at the sync commit

`specs/expected.txt` records each of these as `breaks`. A fix makes its lines hold, and `bin/check` then reports `NOT AS EXPECTED`. Change those lines to `holds`, and remove the finding here.

- **F1. A run can write over a newer run. Runs: `MonitorRuns`, `MonitorRunsSlow`.**
  - `MonitorEvaluation` uses the Monitor that it loaded before the take (`MonitorEvaluationJob#perform`, Run now's `set_monitor`, `McpTools::RunMonitor`).
  - `#record` checks only `updated_at`, which `update_columns` does not change. It does not check that the run still holds the lease.
  - Path:
    1. A run A loads the Monitor.
    2. Another run B commits and releases the lease.
    3. Run A takes the lease. It reads the Logs after the old `ingest_cursor` again (`NoLogMatchedTwice`), or it writes Threshold states from its older range (`NoStaleWrite`).
  - When runs end within `RUN_LEASE`, the window is the gap between run A's load and its take. That gap is short, but it can grow to the SQLite busy timeout (5 s) while another write holds the lock.
  - When a run takes longer than `RUN_LEASE`, the window is wide (`MonitorRunsSlow`).
  - In a scratch copy of the model, these 2 changes made all `MonitorRuns` rules hold, and left only `OneRunAtATime` broken in `MonitorRunsSlow`. With that fix, a late run writes nothing.
    - Load the Monitor again after the take.
    - In `#record`, write only when `run_started_at` is still this run's started time.
- **F2. Two ticks can deliver at the same time. Run: `DeliveryLong`.**
  - Solid Queue deletes the tick's semaphore 2 minutes after the dispatch, also while the tick still waits or runs. Then the next tick runs too.
  - `deliver_monitor_rows` marks each row `processing` only just before it sends it, and `mark_rows` does not check the status.
  - So both ticks send a row that both plucked (`SentAtMostOnce`).
  - Or tick 1 is still sending the firing row when tick 2 plucks only the resolved row and sends it first (`ResolveAfterTrigger`).
  - A tick takes this long when a Monitor that notifies by group fires many groups at once: one message for each row, and up to 5 s for each provider call.
- **F3. A tick that dies leaves its row in `processing` for ever. Run: `DeliveryTickDies`.**
  - No code sets a `processing` row back, and `Notification.deliverable` does not include `processing`.
  - So the firing page never goes (`NoRowLeftBehind`), and the next tick sends the resolve without it (`ResolveAfterTrigger`).
  - A deploy that stops a job does the same: Solid Queue runs the job again, but the row is no longer deliverable.
- **F4. A run that dies after its commit leaves its rows with no tick. Run: `DeliveryWriterDies`.**
  - `MonitorEvaluation#record` commits the rows, and then `schedule_ticks` enqueues the tick in the queue database.
  - If an error or a dead process stops the run between the two, the rows wait for the next tick of that Integration, from any other Notification. A group that stays firing writes no new row.
- **F5. Two threshold reached Notifications for one Issue. Run: `EventThresholdLong`.**
  - `event_threshold_reached?` checks for a row, counts in EventStore, and then `Notification.enqueue!` writes the row. These are 3 statements, with no transaction around them.
  - Only the semaphore keeps the jobs apart, and Solid Queue deletes it 2 minutes after the dispatch.
  - A burst of Events gives many jobs. One of them can wait in the queue for longer than that, and then the next job runs at the same time.

## When a result is not as expected

1. **Read the path with `/statechart`.** Then do each step of it in the code, at the file and function that the action's comment names.
2. **It is a real bug if each step is a step that the code can take,** in that order. A "Long" or "Dies" config also counts, because its assumption is real. Report the bug with the path in the code's words. Do not weaken the rule or the model to make it pass. Do not change product code unless the user asks.
3. **It is a modeling error if a step cannot happen in the code.** For example, two steps that are one transaction in the code are two actions in the model, or a guard is missing. Fix the model, then run `bin/check` and `bin/mutants`.
4. **ERROR (exit 2):** read `tlc.out`. A parse error, a deadlock or `TypeOK` is a model error. A `Done` step stops a correct end from being a deadlock.

## When a change to the covered code is intended

1. Find the actions in the coverage record that the change touches. Change them to do what the new code does, one action for each statement or transaction.
2. Run `bin/check`. A new `breaks` is a bug in the change, unless you can show that the model is wrong (see above). A finding that now holds means that the change fixed it: update `specs/expected.txt` and the findings above.
3. Run `bin/mutants`. When you change the model text that a variant replaces, update that variant in `MUTANTS` in `bin/mutants`, so that each rule that holds still has a variant that breaks it. A new rule needs a variant too.
4. Move the sync commit (in this file and in the `git diff` command) only after you confirm that each action describes the code at the new commit.

## Coverage record

**Sync commit:** `3c96a93e` (main, 2026-11-22). At that commit, each action below describes the code that it names.

| Model action | Code |
| --- | --- |
| MonitorRuns `Load` | `MonitorEvaluationJob#perform` (`LogMonitor.find_by`, `now = Time.current`); `MonitorsController#set_monitor` and `#run`; `McpTools::RunMonitor#run` |
| MonitorRuns `Take` | `MonitorEvaluation.take` |
| MonitorRuns `Read` | `MonitorEvaluation#run_match` (`EventStore.logs_ingested_after`), `#read_threshold` |
| MonitorRuns `Commit` | `MonitorEvaluation#record`, `#notify_matched`, the `update_columns` of `#run_match`, `#move_states`, `#finish` |
| MonitorRuns `Release` | the `ensure` in `MonitorEvaluation.run` |
| MonitorRuns `Expire` | `MonitorEvaluation::RUN_LEASE` |
| MonitorRuns `Save` | `MonitorsController#update`, and `LogMonitor`'s `resolve_groups` callbacks |
| Delivery `Write`, `ScheduleAfterWrite`, `WriterDies` | `MonitorEvaluation#record`: `IntegrationNotification.monitor_changes`, then `.schedule_ticks` |
| Delivery `Dispatch`, `Expire`, `TickDies` | `IntegrationNotificationProcessorJob` `limits_concurrency` (to: 1, duration: 2 minutes, on_conflict: :discard); Solid Queue 1.3.1 `Semaphore`, `Dispatcher::ConcurrencyMaintenance`, `ClaimedExecution.fail_all_with` |
| Delivery `Pluck` | `IntegrationNotification.deliver_pending`: `Notification.pending_for(...).pluck(:id)`; `Notification.deliverable` |
| Delivery `Mark`, `Send` | `IntegrationNotification#deliver_monitor_rows`, `#deliver_monitor_message`; `Notification.mark_rows` |
| Delivery `Follow` | the end of `deliver_pending`: `deliverable.exists?`, then `schedule_tick(wait: RATE_LIMIT_WINDOW)` |
| EventThreshold `Dispatch`, `Unblock`, `Expire` | `EventThresholdJob` `limits_concurrency` (key: the Issue, duration: 2 minutes, on_conflict: block); Solid Queue 1.3.1 `BlockedExecution` |
| EventThreshold `Check`, `Count`, `Insert` | `IntegrationNotification.check_event_threshold`, `#apply_rules`, `#event_threshold_reached?`, `#threshold_reached_since?`, `#record`; `EventThresholdJob`'s `retry_on` |

## Scope

The checks cover the models in these bounds:
- **MonitorRuns:** 2 runs (a scheduler job and Run now), 2 Log batches, 1 save.
- **Delivery:** one group's chain of 2 rows (firing, then resolved), 4 ticks, 2 tries.
- **EventThreshold:** 2 Events.

A pass covers only these bounds, not every size.

The models leave out:
- Match timeouts and the skip after `TIMEOUTS_BEFORE_SKIPPING`.
- Arming.
- EventStore errors in a run.
- Deletion of a Monitor or an Integration.
- Issue rows and `BATCHED_KINDS`.
- The rate limit window, which only delays a tick.
- `NotConfigured`.
- The values and groups that a Threshold run reads.
- More than one group or Integration.

`MaxTries` stands for `MONITOR_RETRY_FOR` (6 hours). In Delivery, `Follow` is one step, because nothing slow runs between the `exists?` check and the end of the job, and a new row's tick waits `INITIAL_DELAY`.
