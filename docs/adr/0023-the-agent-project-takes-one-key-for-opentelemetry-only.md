# The Agent Project takes one key, for Claude Code's OpenTelemetry only

Replaces a rule that was only a comment in `ProjectKeyPolicy`, not an ADR: the **Agent Project** had no key, because "a key would let any sender write Logs into it". Amends [ADR 0020](0020-agent-transcripts-raw-chunks-in-clickhouse-and-logs-for-reading.md): the Agent Project also holds Claude Code's OpenTelemetry events and metrics.

Each engineer's Claude Code sends its OpenTelemetry events and metrics to the Agent Project of the **Organization**. An Admin makes the key, and the Agent Project can have only one. Each member of the Organization can read the key, as they can read the keys of other **Projects**, so the Capture setup page fills it in for each engineer. Rotate and delete follow the rules of the keys of other Projects. The key has no DSN. These limits keep the key from writing anything else:

- **Two routes.** EventStore accepts the key on `POST /v1/logs` and `POST /v1/metrics` only. Every other route that takes a key (`/api/:project_id/store`, `/api/:project_id/envelope`, `/api/:project_id/logs` and `/api/logs`) answers 403 for it. So the key makes no **Event**, **Issue** or **Span**.
- **Only Captured repositories.** At ingest, before the **Log batch** is stored, EventStore keeps a record only when its `vcs.repository.url.full` matches a **Captured repository**. It reads the value from the log record's own attributes, or the metric data point's, never from the resource or the scope, because a sender can put the value on the resource. A record with no value, or with another repository, is dropped. EventStore reads the list from Console's tables with the key ([ADR 0001](0001-eventstore-writes-console-tables.md)). An empty list drops all records.
- **A dropped record is filtered.** The sender gets a success, with no `partial_success`, so Claude Code shows no warning. The **Ingest counts** count the record as filtered on the Project's day, so an Admin can see why nothing arrives.
- **The file `otel`.** The Agent Project's reads find **Runs**, the **Dispatcher**'s Logs and **Agent session** steps by `file`. So EventStore stores each record of the key with `file = otel`, and keeps a sender's `file` as `original.file`, by [ADR 0002](0002-logs-scoped-by-project-flat-attributes.md)'s rule for a built-in field. No Agent Project read uses `otel`. `otel` is a stored name: a change needs a data migration.

We chose this because the Admin keeps one list of Captured repositories, for Agent sessions and for telemetry, and the telemetry is next to the Agent sessions and the Runs. Run events and the Dispatcher's Logs come in with **API tokens** and do not go through these limits.

## Considered options

- **An ordinary Project for the telemetry, with a Log transform that drops other repositories.** No new rule for the Agent Project, but the Admin keeps a second list, in VRL, that can differ from the first.
- **The filter in Log processing.** Run events and the Dispatcher's Logs go through Log processing too, so the filter would drop them.

## Consequences

- Claude Code sends the records of every session on the machine, so the records of other repositories leave the machine. The server drops them; they are not stored, not even in a Log batch.
- A key holder can still send records that name a Captured repository. They are stored as the team's telemetry, limited to two routes and to Logs in the file `otel`.
- The starter Factory **Dashboard**'s Queries read only the files `runs/<run id>`, so the Logs of the key do not change them. A data migration gave the existing Dashboards the new Queries.

Source: [.scratch/claude-code-otel/plan.md](../../.scratch/claude-code-otel/plan.md)
