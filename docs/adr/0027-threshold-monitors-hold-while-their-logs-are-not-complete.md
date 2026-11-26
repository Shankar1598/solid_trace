# Threshold monitors hold while their Logs are not complete

A Threshold monitor compares a range of Logs with its threshold ([ADR 0006](0006-monitors-evaluated-by-console-through-the-query-api.md)). The range ends 60 seconds before the run. This covers normal Log processing. After a restart, it does not. After a restart of EventStore's host, ClickHouse can take minutes to load. Log processing is then some minutes behind. The senders have also not sent their kept Logs again yet. A Monitor with "alert on no data" or a "too few" threshold then fires, and it resolves a minute later. Missing Logs look like no data.

So a Threshold run is now **held** while the Project's Logs are not complete:

- **Each read of a Threshold run sends `hold_if_incomplete: true`.** This is so for a Query, for SQL and for the Baseline's read. EventStore checks two facts before it runs the read:
  - **Log processing is behind.** The Project's oldest Log batch that waits for Log processing is more than 60 seconds old. This is the same 60 seconds as Console's `LogMonitor::PROCESSING_LAG`.
  - **Ingest gap.** After a gap of more than 15 seconds, EventStore holds for the gap's length. The hold is at least 1 minute and at most 10 minutes.

  When either fact is true, EventStore does not run the read. It answers `{"incomplete": {"reason", "behind_seconds" | "until"}}`.
- **A held run compares nothing.** Console records a `monitor_runs` row with status `held` and the reason. It changes no group's state and writes no Notification. It runs the Monitor again at the next tick, as after an error. The Monitor's `held_since` is the time of its first held run. Any run that is not held clears it. Turning the Monitor off or on clears it too.
- **A hold never compares on partial data, however long it lasts.** A Monitor that is held for a long time is a "not running" Monitor. A later change reports it to the Organization.
- **EventStore owns the facts, and Console owns the policy.** EventStore says that the Logs are not complete. It also says why, and until when, if it knows. Console decides to hold, and what to tell a member.

These do not hold:

- **Match monitors.** A Match monitor reads in Log batch order from its cursor. It reads each Log in the first run after Log ingest stores the Log's batch.
- **The form's preview, Dashboards, the Logs explorer and MCP reads.** They do not send the flag. A member who previews a Monitor sees what the Logs give now.

## Considered options

- **Console reads the backlog first, then the range.** This needs a second call for each run. Also, the backlog can change between the check and the read. With the check in the read's own request, a held run puts no load on ClickHouse while Log processing drains the backlog. A backlog only gets smaller, so the check and the read cannot race.
- **Compare on partial data when it proves the result.** An example is a count that is already over a "more than" threshold. This is correct only for some operators. It is not correct for a change from a Baseline. It is left for later.
- **A longer range lag for every run.** It would delay every Notification by the longest backlog. A restart's backlog has no upper limit.

## Consequences

- **A held run sends nothing, also when the Monitor would fire on real data.** A real problem that starts during a hold is reported when the Logs are complete. The hold after an ingest gap is at most 10 minutes. A Log processing backlog holds as long as it lasts.
- **The Monitor's page shows each held run with its reason.** While the Monitor is on and held, its status line says "Held since". Its status is `held` when no group is firing. A firing group stays firing.
- **SQL Monitors hold on the Log facts only.** A SQL Monitor that reads `events` or `spans` does not wait for Event processing or Span processing.
