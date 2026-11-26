# Threshold monitors hold while their Logs are not complete

A Threshold monitor compares a range of Logs with its threshold ([ADR 0006](0006-monitors-evaluated-by-console-through-the-query-api.md)). The range ends 60 seconds before the run, which covers normal Log processing. After a restart it does not. On 2026-11-26 the production VM stopped and started. Log processing was some minutes behind when ClickHouse came back, and the senders had not sent their kept Logs again yet. Four Monitors with "alert on no data" or a "too few" threshold fired at 09:51 and resolved at 09:52. Missing Logs looked like no data.

So a Threshold run is now **held** while the Project's Logs are not complete:

- **Each read of a Threshold run sends `hold_if_incomplete: true`**, for a Query and for SQL, and for the Baseline's read too. EventStore checks two facts before it runs the read. (a) **Log processing is behind**: the Project's oldest Log batch that waits for Log processing is more than 60 seconds old, the same 60 seconds as Console's `LogMonitor::PROCESSING_LAG`. (b) **Ingest gap**: after a gap of more than 15 seconds, EventStore holds for the gap's length, at least 1 and at most 10 minutes. When either is true, it does not run the read and answers `{"incomplete": {"reason", "behind_seconds" | "until"}}`.
- **A held run compares nothing.** Console records a `monitor_runs` row with status `held` and the reason, changes no group's state, writes no Notification, and runs the Monitor again at the next tick, as after an error. The Monitor's `held_since` is the time of its first held run, and any run that is not held clears it.
- **A hold never compares on partial data, however long it lasts.** A Monitor that is held for a long time is a "not running" Monitor, which a later change reports to the Organization.
- **EventStore owns the facts, Console owns the policy.** EventStore says that the Logs are not complete, why, and until when it knows. Console decides to hold and what to tell a member.

These do not hold:

- **Match monitors.** They read in Log batch order from their cursor, so a late Log is read when it comes.
- **The form's preview, Dashboards, the Logs explorer and MCP reads.** They do not send the flag. A member who previews a Monitor sees what the Logs give now.

## Considered options

- **Console reads the backlog first, then the range.** It would need a second call for each run, and the backlog could change between the check and the read. With the check in the read's own request, a held run puts no load on ClickHouse while it drains the backlog, and a backlog only gets smaller, so the check and the read cannot race.
- **Compare on partial data when it proves the result**, such as a count already over a "more than" threshold. It is correct only for some operators and not for a change from a Baseline. Left for later.
- **A longer range lag for every run.** It would delay every Notification by the longest backlog, and a restart's backlog has no upper limit.

## Consequences

- **A held run sends nothing, also when the Monitor would have fired on real data.** A real problem that starts during a hold is reported once the Logs are complete. The hold after an ingest gap is at most 10 minutes; a Log processing backlog holds as long as it lasts.
- **The Monitor's page shows each held run with its reason**, and its status line says "Held since" while it is held. Its status is `held` when no group is firing; a firing group stays firing.
- **SQL Monitors hold on the Log facts only.** A SQL Monitor that reads `events` or `spans` does not wait for Event processing or Span processing.
