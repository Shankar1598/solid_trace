# Traces

Status: ready-for-agent

Planned with the user on 2026-10-27. Terms are in [GLOSSARY.md](../../GLOSSARY.md); the new ones are listed under Further Notes. Tickets are in [issues/](issues/).

## Problem Statement

A member can see that a request failed and what it logged, but not why it was slow.

Sentry's SDKs send performance data, Transactions and their Spans, whenever tracing is on. EventStore receives them in the same envelopes as Events and throws them away. Tagalys's Rails API traces 1% of its requests and go-api 5%, but those Transactions go only to sentry.io. Its Issues go to sentry.io, and its Logs go to Better Stack and, for some apps, to SolidTrace. Nothing ties a slow request to its Logs or to the Issue it raised.

So finding where an endpoint spends its time, whether it got slower after a release, or which customer it's slow for means searching three tools by hand. An agent working over MCP can't do it at all.

## Solution

SolidTrace stores the Transactions that Sentry SDKs send and links each one to the Logs and Issues that share its trace id.

- **Sampling stays in the SDK.** Apps sample 1–5% of requests, and SolidTrace keeps everything it receives. Throughput is scaled back up by each Transaction's sample rate and labelled as an estimate.
- **A trace page** shows one trace across the Organization's Projects:
  - a waterfall of its Spans, with self time, repeats collapsed and the critical path marked;
  - the trace's Logs;
  - the trace's Issues.

  It still opens when the request wasn't sampled, showing the Logs and Issues alone, so a link from any Issue or Log works. Issues, Logs and the command palette link to it.
- **Performance**, per Project, lists Transactions by the time they consume. Each row has throughput, p50, p95 and p99, error rate, and the change from the previous period.
- **A Transaction's page** shows:
  - its p95 by release;
  - where its time goes by Span op and description;
  - Spans that repeat within one request (N+1s);
  - its slowest traces.
- **MCP** gains `list_transactions`, `get_transaction`, `get_trace` (summarised so that a large trace fits an agent's context) and a `spans` table in SQL queries. With these, an agent can go from "what got slower" to "which Span, since which release, for whom".
- **Retention:** trace Retention defaults to 14 days and can be changed for the instance and per Project. Spans move to the bucket after the same local cache hours as Logs, so local disk holds only recent hours.
- **The Overview** counts Spans and adds a Datadog APM price estimate.
- **Docs:** a Traces page shows how to send to sentry.io and SolidTrace at once while moving over, and how to put trace ids on Log lines.
- **Dogfooding:** SolidTrace's own Console traces itself into the Meta Project.

## User Stories

### Traces

1. As a member, I want to open a trace from an Issue's Event, so that I can see the request that raised it.
2. As a member, I want to open a trace from a Log's trace id, so that I can see the request a line came from.
3. As a member, I want to open a trace by pasting its id into the command palette, so that I can follow an id from anywhere.
4. As a member, I want the trace page to show Spans from every Project in my Organization, so that a request that crosses a frontend and an API is one picture.
5. As a member, I want each Span labelled with its Project, so that I know which service did the work.
6. As a member, I want a waterfall of the trace's Spans nested by parent, with each one's op, description, start, duration and status, so that I can see where the time went.
7. As a member, I want each Span's self time, its duration minus its children's, so that a slow parent isn't blamed for its children.
8. As a member, I want identical repeated Spans within a Transaction collapsed into one row with a count and total time, so that an N+1 stands out and the page stays short.
9. As a member, I want the critical path marked, so that I know which Spans made the request as long as it was.
10. As a member, I want the trace's Logs beside the waterfall in time order, each linked to the Log, so that I can read what the request said.
11. As a member, I want the trace's Issues listed and linked, so that I can see what the request broke.
12. As a member, I want a trace with no sampled Spans to still show its Logs and Issues, with a note that it wasn't sampled, so that a link from an unsampled Issue isn't a dead end.
13. As a member, I want a trace id from another Organization to show nothing of it, so that traces stay private to their Organization.
14. As a member, I want a trace older than the local cache to still open, a little slower, so that reading from the bucket doesn't lose a trace.

### Performance

15. As a member, I want a Performance page under each Project listing its Transactions, one row per name, so that I see every endpoint and job that was traced.
16. As a member, I want throughput scaled up by each Transaction's sample rate and labelled as an estimate, so that 1% sampling doesn't read as 1% of the traffic.
17. As a member, I want p50, p95 and p99 durations, so that I know how slow each Transaction usually is and how slow it gets.
18. As a member, I want the error rate, so that I know which Transactions fail.
19. As a member, I want time consumed (throughput × average duration), and the list sorted by it by default, so that the Transaction worth fixing comes first.
20. As a member, I want to sort by any column, so that I can find the slowest or the busiest.
21. As a member, I want each number's change from the previous period of the same length, so that I can see what got slower.
22. As a member, I want Transactions named by raw URL folded into one "Unparameterised" row, so that `/orders/123` doesn't make a row per request.
23. As a member, I want range and environment pickers that work as they do on Logs, kept in the URL, so that I can share a view of production over the last day.
24. As a member of a Project with no Transactions yet, I want the page to say so and link to the Traces docs, so that I know how to turn tracing on.

### A Transaction

25. As a member, I want a Transaction's p50 and p95 over the range, split by release, so that I can see which release made it slower.
26. As a member, I want where its time goes by Span op, as a share of its duration, so that I know whether it's the database, an HTTP call or the view.
27. As a member, I want where its time goes by Span description, with literals replaced so that the same query groups together, so that I find the one slow query.
28. As a member, I want Spans that repeat within one Transaction flagged with their median and maximum repeats, so that I can find N+1s.
29. As a member, I want its slowest traces, each linking to the trace page, so that I can open an example.

### Settings

30. As an Instance admin, I want a default trace Retention on Settings → Instance, 14 days to start, so that Spans don't fill the disk.
31. As an admin, I want a Project's own trace Retention, overriding the default, so that a busy Project can keep fewer days.
32. As an Instance admin, I want Spans older than the local cache hours moved to the bucket, as Logs are, so that local disk holds only recent hours and the cheaper bucket holds the rest.
33. As an Instance admin, I want Spans to count toward the disk limit, so that a burst of traces can't fill the disk.
34. As an admin deleting a Project, I want its Spans deleted too, so that nothing of it is left behind.

### Overview

35. As a member, I want Spans received and stored on the Overview, per Project and in total, so that I know what tracing costs to keep.
36. As a member, I want a Datadog APM list-price estimate for the Spans, so that I know what tracing would cost elsewhere.

### MCP

37. As an agent, I want `list_transactions` with throughput, percentiles, error rate, time consumed and the change from the previous period, so that I can find what's slow and what got slower in one call.
38. As an agent, I want `get_transaction` with its p95 by release, its time by op and description, its repeated Spans and its slowest trace ids, so that I can see where its time goes across many requests.
39. As an agent, I want `get_trace` summarised (repeats collapsed, self time, the critical path, the top Spans by self time first, trimmed to a budget with a count of what was left out), so that a trace of 2,000 Spans fits my context.
40. As an agent, I want `get_trace` to give the trace's Issues and the `query_logs` arguments for its Logs, so that I can read what the request said and broke.
41. As an agent, I want a `spans` table in `run_sql`, so that I can ask what the tools don't, such as p95 by a customer tag.
42. As an agent, I want each answer to end with a link to its page, so that the user can open what I found.
43. As an agent, I want trace Retention in `get_instance_settings`, `update_instance_settings`, `list_projects` and `update_project`, so that I can change it as a person can.

### Operator

44. As an operator, I want to send to sentry.io and SolidTrace at once from one SDK, documented for Ruby and Go, so that I can move over one app at a time and compare them.
45. As an operator, I want a documented way to put the trace id on my apps' Log lines, for requests and Sidekiq jobs, without changing their text format, so that Logs link to traces without breaking my shippers.
46. As an operator, I want a Log transform that moves a `trace=<id>` tag into the Log's trace id, documented, so that SolidTrace extracts it and my shippers don't change.
47. As an operator, I want SolidTrace to trace its own Console into the Meta Project, so that I can see the feature working on my install.

## Implementation Decisions

### Ingest and Span processing

- EventStore's envelope ingest reads `transaction` items as well as the event and log items it reads today. It writes each one to Pebble as received, with the envelope's sample rate from its dynamic sampling context; a missing or unreadable rate counts as 1.
- **Span processing** moves received Transactions to ClickHouse in ranges with a dedup token, on the same at-least-once path as Event processing, so a retry can't store a range twice. A Transaction never becomes an Issue.
- A Transaction becomes one row for itself (the **segment**) and one for each of its Spans.
- Worked out per Transaction during processing, because all its Spans arrive together:
  - each Span's self time;
  - a **group** hash of its op and normalised description (literals such as numbers, quoted strings, `IN` lists, UUIDs and hex ids become `?`);
  - each group's repeat count within the Transaction.
- A Transaction whose name source is a raw URL, or is missing, is stored under the name `<unparameterised>`. Its raw name is kept as an attribute.
- Span ingest is counted for the Overview, the way Log ingest is, without the Overview reading `spans`.

### Schema

- **`spans`** (ClickHouse MergeTree), one row per Span or segment, with:
  - ids and links: Project, trace id, span id, parent span id, segment id, whether it's the segment, Transaction name;
  - what it did: op, description (ZSTD), group, repeats, status;
  - timing: start, duration and self time in microseconds;
  - where it ran: sample rate, environment, release, server name;
  - attributes: the SDK's tags and data, as a map.
  
  It's partitioned by Project and day, ordered by Project, Transaction and start, and has the `logs` table's storage policy, bucket TTL and dedup window.
- **`trace_index`**: one row per Transaction, with trace id, Project, start, end and Transaction name. It's ordered by trace id and partitioned by Project and day, so a trace is a primary-key lookup across the Organization's Projects rather than a scan of `spans`.
- **`logs`** gains a bloom-filter skip index on trace id. It applies to new parts only, so existing parts aren't rewritten.
- **`events`** gains a trace id column, filled by Event processing from the Event's trace context, with a bloom filter. Events stored before it have none.
- **Console** gains `default_trace_retention_days` on instance settings (default 14) and `trace_retention_days` on Projects (empty means the default). EventStore reads them as it reads Log Retention. Per [ADR 0001](../../docs/adr/0001-eventstore-writes-console-tables.md), the columns and their reader change in the same commit.

### Retention and the bucket

- Retention, Project deletion and the disk limit drop whole Project-day partitions of `spans` and `trace_index` with the code that drops them for `logs`. Spans count toward the disk limit.
- With a bucket, Span parts move to it after the instance's local cache hours, through ClickHouse's bucket disk, as Logs do. Spans have no Parquet export.

### EventStore Query API

Three new routes on the loopback Query API. As with the Overview, Console sends Project ids and EventStore never reads Organizations.

- **Trace**: takes Project ids, a trace id and an optional time hint. It answers the trace's Spans, its Logs (its Projects within its time ± 5 minutes, or around the hint when nothing was sampled) and its Events grouped by Issue.
- **Transactions**: takes a Project, a range and an environment. For each Transaction name, it answers:
  - throughput, the sum of `1 / sample_rate` over segments divided by the range's seconds;
  - p50, p95 and p99 over sampled segments, unweighted;
  - error rate, the weighted share of segments whose status isn't `ok`;
  - time consumed, the weighted sum of durations;
  - the same numbers for the previous period of the same length.
- **Transaction**: takes a Project, a name, a range and an environment. It answers p50 and p95 over time by release, self time by op and by group, repeated groups with their median and maximum repeats, and the slowest trace ids.
- Reads carry the Overview's read tags, with new kinds `trace`, `transactions` and `transaction`, so they show in the Overview's Health.
- Budget: under 1 s over 7 days on production's data.

### Console

- The trace page belongs to the Organization. Performance and the Transaction page belong to the Project. The sidebar gains Performance under each Project.
- **One answer builder per view** (trace, Transactions, Transaction) feeds both its page and its MCP tool, as the Overview's builder does. Each builder's interface is "this view, for this user, over this range". The EventStore call, Organization scoping, links and summarising are inside it.
- When EventStore fails, a page renders whatever Console holds and says what's unavailable, as the Overview does.
- Links into the trace page come from:
  - the Issue page's Event, when it has a trace id;
  - the Log detail's trace id;
  - the command palette, for a 32-hex string.
- Trace Retention sits beside Log Retention on Settings → Instance and on the Project's settings.

### MCP

- `list_transactions`, `get_transaction` and `get_trace` answer their builders' views and end with a link to the page.
- `get_trace` keeps at most about 200 rows:
  - repeats are collapsed, with self time and the critical path;
  - the top Spans by self time come first;
  - it says how many Spans it left out;
  - it includes the trace's Issues and the `query_logs` arguments for its Logs, as `get_issue` does.
- SQL queries gain a read-only `spans` table for the Project, behind the same guard as `logs` and `events`.
- The instance settings and Project tools take and answer trace Retention.

### Docs and dogfood

- A Traces docs page covers:
  - turning tracing on with a sample rate;
  - sending to sentry.io and SolidTrace at once through a custom SDK transport, for Ruby and Go. The same envelope works for both, because EventStore authenticates only by the key in the auth header.
  - putting trace ids on Log lines with request and Sidekiq middleware that tag lines `trace=<id>`, plus lograge's custom options;
  - a Log transform that moves the tag into the trace id.
- Console sends its own Transactions and Events to the Meta Project at a sample rate set at install (default 5%). Its Log lines carry the trace tag, and the Meta Project's seeded Log transform extracts it.

## Testing Decisions

A good test drives the feature from outside, through the same interface a sender, a page or an agent uses, and checks what comes back: what's stored and answered, not how. Tests don't reach into Span processing's internals, ClickHouse's tables or the builders' private steps. Each ticket runs the targeted tests it touches; the full suites are for the user.

Three seams, all existing:

1. **EventStore's HTTP surface.** Real envelopes go into ingest, and answers come out of the Query API, against the test ClickHouse. This covers:
   - ingest and Span processing: self time, groups, repeats, the unparameterised name, sample rate, retries not counting twice, and Transactions never making Issues while the envelope's event and log items are still stored;
   - each route's answers: a trace across two Projects with its Logs and Events, an unsampled trace around its hint, weighted Transaction numbers at two sample rates with the previous period, and a Transaction's breakdown, releases and slowest traces;
   - Retention, Project deletion and the disk limit dropping Span days;
   - the `spans` table in SQL reading its own Project only.
   
   The envelopes are captured from sentry-ruby 5.28 and 6.x and from sentry-go. Prior art: the Sentry Logs envelope tests, Event cleanup tests, Log Retention tests, SQL scope tests and Overview route tests in EventStore's root package.
2. **Console's answer builders, through their controllers' page props and their MCP tools,** with EventStore's answers stood in at its URL. This covers Organization scoping (another Organization's trace refused), the unsampled trace, pages rendering when EventStore fails, `get_trace`'s budget on a 2,000-Span trace, and settings validation and admin gating. Prior art: the Overview controller tests (which stand in for `/overview`), the Overview and Issue MCP tool tests, and the instance settings tests.
3. **Playwright flows on the dev stack:**
   - open a trace from an Issue, a Log and the palette, including an unsampled one;
   - open Performance, sort it, and open a Transaction and one of its slowest traces;
   - change trace Retention;
   - see Spans on the Overview.
   
   Prior art: the Overview, Issues, palette and instance-settings flows.

Timed on production's data before release, under a minute per run: every route over 7 days, and opening a trace older than the local cache hours.

## Out of Scope

- OTLP, and SDKs other than Sentry's.
- Web vitals, profiling, session replay, and Sentry's automatic suspect-span detection. Repeated-Span flags are as far as this goes.
- Server-side sampling, or keeping every trace that had an error.
- Monitors, Charts and Dashboards on Spans (later).
- Exporting Spans as Parquet. They reach the bucket through ClickHouse's bucket disk.
- Forwarding envelopes from SolidTrace to sentry.io. Sending to both happens in the SDK.
- The changes to Tagalys's apps, which are tracked in the Tagalys workspace (see Further Notes).

## Further Notes

### New terms

GLOSSARY.md gains these with ticket 02:

- **Trace**: every Span, Log and Event that shares one trace id, across an Organization's Projects.
- **Span**: one timed operation in a Trace, with an op, description, start, duration and parent.
- **Transaction**: the root Span of one request or job in one Project, as a Sentry SDK sends it. It is named by route or job, and holds its child Spans.
- **Span processing**: what moves received Transactions from Pebble to ClickHouse.
- **Trace Retention**: how many days a Project keeps its Spans.

### Capacity (measured 2026-10-27)

- The production VM has 2 vCPUs, 15 GB and a 74 GB data disk. CPU load is 4.5–5 and CPU pressure is ~18%. ClickHouse's local data is 36 GB against a 39 GB disk limit, projected to 24 GB at steady state.
- Tagalys sends ~330 M Logs a day, 80% of them the indexer's verbose Logs. Logs reads are already slow: the verbose Project's p95 is 15 s, and seven reads in a day hit the 30 s timeout.
- Spans are expected to add roughly 5–20% of today's Log rows. The API's and go-api's Logs will also start arriving, at a volume not yet measured.
- Roll out one app at a time and watch CPU. Move to 4 vCPUs if reads get worse (ticket 07).

### Tagalys rollout

Decided with the user on 2026-10-27, and tracked in the Tagalys workspace:

1. Merge the branch that adds Elasticsearch Spans to the Rails API's Transactions.
2. Add the dual-send transport to the shared Sentry initializer and to go-api's, behind a SolidTrace DSN. Keep sentry.io until SolidTrace has proved itself.
3. Add trace ids to Log lines:
   - request and Sidekiq middleware in the shared engine;
   - lograge custom options on the API;
   - go-api's existing Sentry trace id field renamed in its Vector program.
   
   Text formats stay as they are.
4. Add a SolidTrace sink to the API's and go-api's Vector configs, keeping Better Stack.
5. Switch one app at a time: the dashboard first, then the indexer, go-api and the API.
