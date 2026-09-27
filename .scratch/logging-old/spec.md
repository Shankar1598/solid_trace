# SolidTrace Logs

Status: wontfix

Companion documents: `architecture.md` (technical design, data model, API contracts, milestones) and `issues/` (tracer-bullet tickets). Decisions are recorded in `docs/adr/0001` to `0005`.

## Problem Statement

Teams that run SolidTrace for error tracking still need a second tool for application logs. Hosted products such as Axiom give them schemaless ingest, instant queries and dashboards with no setup, but are not self-hosted. Self-hosted alternatives either need a fleet of services (Elastic, Loki + Grafana) or make users configure indexes and field extraction before queries are fast (Better Stack style). Meanwhile the Sentry SDKs these teams already use have started sending structured logs to SolidTrace's envelope endpoint by default, and SolidTrace silently drops them.

## Solution

SolidTrace accepts Logs from three sources with no per-field configuration: the Sentry SDKs already pointed at a Project, any HTTP shipper that posts JSON (Vector, Fluent Bit, curl), and OpenTelemetry exporters. Logs are stored by EventStore in DuckDB next to Events, with every attribute kept and typed automatically, and rolled into Parquet with per-Project retention.

In the Console a Logs page lets a user pick a Project and a time range, then filter and aggregate with a visual Builder or a KQL-style Query, see a histogram of matching Logs, open any Log's full detail, tail live, and save or share the Query. Dashboards are collections of Elements, each a Query plus a visualisation. Monitors run a Query on a schedule and alert through the Organization's existing Integrations when a threshold is crossed or a matching Log appears.

## User Stories

1. As a developer, I want the logs my Sentry SDK already emits to appear in SolidTrace with no extra setup, so that I get logging for free next to my errors.
2. As an operator, I want to point Vector or Fluent Bit at one HTTP endpoint with a bearer key and a handful of config lines, so that host and container logs land in the right Project.
3. As an operator, I want to send logs with plain curl or any HTTP client as a JSON array or NDJSON, so that scripts and cron jobs can log without an agent.
4. As an operator, I want to send OTLP/HTTP logs from an OpenTelemetry Collector or SDK, so that my existing telemetry pipeline works unchanged.
5. As a developer, I want nested JSON attributes to be queryable by dotted name (`http.request.method`) without declaring a schema, so that I never configure indexes or extractions.
6. As a developer, I want numeric and boolean attributes to compare as numbers and booleans, so that `duration_ms > 500` works without casts.
7. As a developer, I want a timestamp in my payload to be honoured whatever common key or format it uses, and the receive time used otherwise, so that batched or delayed shipments keep their real time.
8. As an operator, I want the ingest endpoint to reject oversized requests and to ask shippers to retry when it is overloaded, so that a burst never corrupts or silently loses data.
9. As a developer, I want a Logs page where I choose a Project and a time range and immediately see recent Logs and a histogram, so that I can start exploring without writing a query.
10. As a developer, I want to write a KQL-style Query such as `logs | where level == "error" and message has "timeout" | summarize count() by bin_auto(_time), service`, so that I can use a language I already know from Axiom or Azure.
11. As a developer, I want a Builder mode with filter rows, a visualisation and group-by, so that I can build the same Query without typing it.
12. As a developer, I want autocomplete for field names and inline errors in the editor, so that I find mistakes before running a Query.
13. As a developer, I want the histogram above the results to be coloured by level and to support dragging to zoom into a time window, so that I can narrow in on an incident.
14. As a developer, I want a results table that stays fast with thousands of rows and lets me choose columns, so that I can scan wide logs.
15. As a developer, I want to open a Log and see all its fields and the raw JSON, and to add "filter for" or "filter out" from any value, so that I can pivot quickly.
16. As a developer, I want to jump from a Log to the surrounding Logs in time ("view in context"), so that I can read what happened around it.
17. As a developer, I want a field sidebar listing the fields seen in the current Project and time range with top values on click, so that I discover what is in my data.
18. As a developer, I want a live tail view that refreshes automatically, can be paused, and highlights warnings and errors, so that I can watch a deploy.
19. As a developer, I want to save a Query with a name and share a link that carries the Query and either a relative or an absolute time range, so that teammates see the same thing.
20. As a developer, I want to download results as CSV or JSON, so that I can analyse them elsewhere.
21. As a developer, I want a Log that carries a trace id to show it, so that logs and future traces can be correlated.
22. As a team lead, I want to create a Dashboard and add Elements (time series, statistic, table, log stream, note) each backed by a Query, so that the team has one view of service health.
23. As a developer, I want an "Add to dashboard" action from the Logs page that pre-fills an Element with my current Query, so that building a Dashboard is fast.
24. As a team lead, I want a Dashboard-level time range with per-Element override, so that one Dashboard serves both live and historical views.
25. As an on-call engineer, I want a threshold Monitor that runs an aggregating Query every N minutes over the last M minutes and alerts when the value is above or below a threshold, so that I hear about error spikes.
26. As an on-call engineer, I want a match Monitor that notifies me for each Log matching a filter, with sensible caps, so that rare but critical lines page me.
27. As an on-call engineer, I want Monitors to notify through the Slack, PagerDuty and email Integrations the Organization already has, and to notify again when the condition resolves, so that alert routing stays in one place.
28. As an on-call engineer, I want a Monitor to optionally alert when no data arrives, so that a dead pipeline is noticed.
29. As an administrator, I want to set a retention period in days per Project, so that disk use is bounded and predictable.
30. As an administrator, I want ingest to keep working during and after a restart with at most a second of loss, and the health endpoint to show log ingest and disk status, so that I can operate one VM with confidence.
31. As an administrator, I want all log reads to go through the Console's authentication and Organization scoping, so that Logs are never exposed to another tenant.
32. As a developer, I want queries over the last hours to answer in well under a second and multi-day queries to degrade gracefully with a timeout, so that exploring stays interactive.
33. As a developer, I want to run raw DuckDB SQL over my Project's Logs as an advanced mode, so that I can do analysis the Query language does not cover (later milestone).
34. As a contributor, I want a load-test scenario for log ingest, so that throughput regressions are caught.

## Implementation Decisions

- Logs are stored by EventStore in the same DuckDB database as Events, in their own hot table plus a Parquet manifest and a field catalog. Logs bypass Pebble entirely; every Log is fully represented in columns.
- A Log row has fixed columns (id as time-ordered UUID, project, event time, ingest time, level, severity number, service, environment, host, source adapter, trace id, span id, message) and three typed maps for attributes: string, number and boolean. Nested objects are flattened to dotted keys; arrays and very deep objects are kept as JSON strings. A per-Project field catalog records each key with its inferred kind and is what the editor, Builder and sidebar read.
- Three ingest adapters map onto one canonical record: Sentry envelope `log` items (the envelope reader must iterate all items honouring each item's length header), a generic HTTP endpoint that accepts a JSON array, a single object or NDJSON (sniffed from the body, gzip and zstd supported, permissive timestamp/message/level key detection), and OTLP/HTTP logs in protobuf and JSON. Shippers authenticate with the Project's existing public key, also accepted as a bearer token.
- Limits mirror Axiom: 10,000 records per request, 1 MiB per record, 200-byte keys, about 1,024 distinct keys per Project. Over-limit requests get 413; a full ingest queue returns 429 with Retry-After so shippers back off and retry.
- The ingest pipeline batches records for up to one second and appends them to the hot table; the field catalog is updated from each batch. Each closed hour is written to one Parquet file per Project, sorted by time, registered in the manifest, then deleted from the hot table in one transaction; a nightly job compacts small hourly files per day; an hourly sweep deletes files older than the Project's retention.
- Queries never glob the Parquet directory. The planner reads the hot table plus the explicit list of manifest files whose Project and time range overlap the Query.
- The query language is KQL-Lite: a documented subset of Kusto/APL parsed and compiled to DuckDB SQL in Go. Every Query is scoped to one Project and one time range injected by the server, literals are bound as parameters, results are capped, and each execution has a timeout. Unsupported Kusto features are rejected with a positioned diagnostic. The same parser serves editor validation, Dashboards and Monitors.
- KQL string semantics follow the Kusto reference: case-insensitive `=~ contains has startswith endswith in~`, case-sensitive `== != in matches regex` and `_cs` variants, term-based `has`, one `now()` per Query, `bin`/`bin_auto` for time bucketing with the chosen width reported back.
- EventStore exposes internal endpoints for query, validate, fields, facets, recent (live tail cursor on the time-ordered id) and single-record fetch; the Console proxies all of them and the browser never calls EventStore. These endpoints join the read-API authentication group being added separately.
- The Logs page is Organization-level with a Project selector; the URL carries the Project, Query and time range so links are shareable. Query execution uses JSON requests from the page rather than full page visits.
- The editor is CodeMirror 6 with a small KQL tokenizer, completion from the field catalog and diagnostics from EventStore's validate endpoint. Charts use recharts through the shadcn chart component; the results table is virtualised. New shadcn primitives are added with the repository's registry style.
- Saved Queries, Dashboards, Dashboard Elements and Monitors are Console models in SQLite. A Dashboard Element is a kind, a title, a Project, a Query, visualisation options and a 12-column grid position. A Monitor is a kind (threshold or match), a Query, a comparator and threshold, a frequency and range in minutes, a no-data policy and a target Integration.
- Monitors are evaluated by a Console recurring job once a minute: due Monitors run their Query through EventStore, update state, record an evaluation event, and on state change create a Notification that flows through the existing per-Integration delivery job and notifiers. The notification path is generalised so its subject can be an Issue or a Monitor.
- Raw DuckDB SQL mode ships after KQL, behind statement validation, a Project-scoped wrapping CTE, row caps and timeouts, because DuckDB's security settings are instance-wide and cannot sandbox a single connection.
- DuckDB stays at the pinned 1.4 engine for this feature; nothing here requires the 1.5 VARIANT type, and an engine upgrade is a separate decision.

## Testing Decisions

- Good tests exercise external behaviour: an HTTP request in and rows or JSON out, a KQL string in and result rows out, a Monitor state in and a Notification out. Transpiler tests assert on executed results over fixture rows first and on generated SQL only through golden files that document the mapping.
- EventStore: package tests for the log record and adapters (Sentry item, generic JSON, OTLP), the storage writer/manifest/archive/retention using temporary directories (existing DuckDB and Pebble tests are the prior art), the ingester with a fake writer (existing ingest service tests are the prior art), and handler tests driving the Fiber app in-process for ingest and query routes. KQL golden tests live beside the parser.
- Console: controller tests with a signed-in user and the Go boundary stubbed with WebMock (existing event store service tests are the prior art), job tests for Monitor evaluation with ActiveJob helpers, notifier payload tests, serializer tests, and one system smoke test that opens the Logs page.
- Load: a k6 log-ingest scenario alongside the existing event-ingest scenario, reporting rows per second, p95 latency and success rate.
- End to end, manually: send logs via curl, a Sentry SDK, Vector and Fluent Bit; run a summarise Query; add it to a Dashboard; create a threshold Monitor bound to a Slack Integration and watch it fire and resolve.

## Out of Scope

- Everything listed in `.scratch/prod-readiness/review.md` (read-API authentication, Dockerfile, shutdown drain of the events channel, key lookup cache, event retention). The logging work depends on some of these and names them, but does not implement them.
- Traces and metrics. The data model keeps trace and span ids so a later traces feature can correlate, and metrics would be a separate narrow store.
- Syslog listeners (Vector or Fluent Bit front them), OTLP over gRPC, inverted full-text indexes, cross-Project queries, anomaly Monitors, drag-and-drop Dashboard layout, natural-language query generation.
- Per-user or per-Project authorisation beyond Organization membership.

## Further Notes

- Log volume is typically 10 to 100 times error volume. Disk growth is bounded only by retention; the default of 30 days should be visible in Project settings from the first release.
- The Sentry SDKs enable structured logs by default in current major versions, so existing SolidTrace users will start sending log items as soon as they upgrade SDKs. The envelope reader must at least not fail on them before the log adapter ships.
- Field naming follows Axiom where it matters for pasted queries: `_time`, `_sysTime`, bracketed names such as `['http.status']`, and the same operator set.

## Comments

2026-09-27: Closed as wontfix. This plan was superseded by the [logs map](../logs/map.md). The file stays here as reference only.
