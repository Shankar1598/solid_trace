# Monitors are evaluated by Console through EventStore's query API, and the outbox carries Monitor rows

A Monitor runs a saved Query on a schedule and tells its Integrations what it finds ([monitors.md](../logs/monitors.md)). Console runs it. Every minute `MonitorSchedulerJob` (a Solid Queue recurring task) enqueues a `MonitorEvaluationJob` for each Monitor that is on and due. `MonitorEvaluation.run`, the one entry point, which Run now also calls, asks EventStore's query API:

- **A Threshold monitor** sends `logs/query` with its Query, `from` and `to`, over its range ending 60 seconds before the run.
- **A Match monitor** sends `after_batch` with its cursor. EventStore answers with the matching Logs of the Log batches after it, up to the processing cursor, and the new cursor.

The run then moves `monitor_states`, writes the Notifications each change owes and records a `monitor_runs` row, in one transaction.

The Notifications go through the Integration notification outbox that Issues already use. A `notifications` row is about an Issue or about one group of a Monitor (`monitor_id`, `group_key`), never both. A check constraint holds that, and `issue_id` became nullable. Delivery splits the rows by subject. Issue rows are delivered as before, and each Monitor group's rows go as an ordered chain that stops at its first failure.

We chose this for three reasons:

- Console already owns what a Monitor needs: its settings, the Organization's Integrations, the message formats, and the outbox with its rate limit, retries and cleanup.
- EventStore's query API already runs any Query with a timeout.
- EventStore stays free of Console's Monitor tables. That is unlike Issues ([ADR 0001](0001-eventstore-writes-console-tables.md)), whose rows come straight out of Event processing.

## Considered options

- **EventStore evaluates Monitors itself:** a Go scheduler beside the data, reading Monitors from Console's database as Event processing writes Issues. It would need Console's Monitor and Integration tables, message formats and outbox in Go, a second notifier for Slack, PagerDuty and email, and a second rate limit per Integration.
- **Match monitors read Logs after a Log id:** Log ids are UUIDv7s made at ingest, before the batch commits, so they don't follow commit order and a cursor on them misses Logs. Batch ids do follow commit order within a Project, so EventStore added `after_batch`.
- **Match monitors evaluated at ingest:** every ingest request would wait on every Match monitor's Query, and an EventStore restart would have to replay them.
- **A separate outbox for Monitors:** it would duplicate the ticks, retries and cleanup, and give one Integration two rate limits that don't know about each other.

## Consequences

- **Each run is a query over HTTP.** It has a 30-second timeout, set by Console and enforced by EventStore. A Threshold run reads its whole range every time; nothing is aggregated as Logs arrive. The runs take Console's Solid Queue workers, and Run now takes a Puma thread for up to 30 seconds.
- **Runs see processed Logs only.** The 60-second lag covers normal Log processing. During a longer processing backlog a count reads low, and alert on no data can fire while Logs are still arriving.
- **Match monitors need batch ids in EventStore's storage.** The `batch` column is in the hot tables and `logs_late`, and the manifest records `batch_max` per Log file ([ADR 0007](0007-log-files-listed-in-a-manifest.md)). After 3 timeouts in a row a Match monitor starts again from the newest batch and says it skipped Logs.
- **One run at a time per Monitor.** Run now runs in the request, outside the job queue, so the lock is a lease on the Monitor row (`run_started_at`), taken with one conditional UPDATE, that both paths take. A run whose Monitor was saved while it ran writes nothing it found, because the change may have resolved the Monitor's groups.
- **De-duplication comes from state changes.** Only a group's move between ok and firing writes rows, so a group that stays firing sends nothing more. Turning a Monitor off, deleting it or changing it to a Match monitor resolves its firing groups.
- **Rows outlive their Monitor.** `notifications.monitor_id` has no foreign key, as with `issue_id`. Each Monitor row keeps what its message says about the Monitor, so a deleted Monitor's resolve still closes its PagerDuty incident.
- **Monitor rows share the Integration's rate limit:** one message a minute, so a resolve can trail its firing message by about a minute. A failed Monitor row is retried for 6 hours, not 5 attempts, because a trigger lost in an outage would never be sent again.
- **PagerDuty dedup keys** are `solid-trace-monitor-<id>`, with `-<group>` for a Monitor with groups, so a resolve closes what its trigger opened. Match messages use `solid-trace-monitor-<id>-matched`, which no group shares, so a Threshold firing never joins a Match incident left open.
- **`monitor_runs`** keeps each Monitor's last 100 runs; the cleanup job deletes older ones.
