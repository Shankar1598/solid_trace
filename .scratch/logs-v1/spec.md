# SolidTrace Logs v1: ingest, store and explore Logs

Status: ready-for-agent

**Origin:** the [SolidTrace Logs map](../logs/map.md). Every decision here traces to a resolved ticket on that map; the ticket is linked where the decision is stated. Hard-to-reverse choices are ADRs [0002](../../docs/adr/0002-logs-scoped-by-project-flat-attributes.md), [0003](../../docs/adr/0003-unrestricted-sql-queries-behind-an-instance-setting.md) and [0005](../../docs/adr/0005-logs-per-project-hot-tables-promote-at-ingest.md). Written 2026-10-02 for [Write the v1 spec and first-milestone tickets](../logs/issues/13-write-v1-spec-and-first-milestone-tickets.md). A few choices the tickets left open are made here; they are listed in Further Notes so they can be challenged.

## Problem Statement

SolidTrace tracks errors, but an error is only half of what a developer needs when something breaks. The other half is the Logs around it: what the application did before and after. Today a team running SolidTrace has to run a second system for Logs (Axiom, Better Stack, Loki), with its own keys, retention and query language.

Worse, Sentry SDKs already send Logs to SolidTrace by default (sentry-ruby 7, sentry-go 0.47+, JS 10.71+, Python 2.68+), and SolidTrace answers `200` and throws them away. Shippers such as Vector and Fluent Bit have nowhere in SolidTrace to send to.

A self-hosted operator wants Logs on the same VM as error tracking, with zero setup: no schema, no indexes, every JSON field queryable at once, as in Axiom. One VM must take 3–5 TB of uncompressed Logs a month (1.2–1.9 MB/s, roughly 1–6k Logs/s) and survive bursts of 5× for 10 minutes.

## Solution

Logs live inside the two existing services. EventStore ingests, stores and queries them; Console shows them in a Logs explorer.

- **Ingest:** EventStore accepts Sentry `log` envelope items on the existing envelope route and plain JSON or NDJSON on a new `POST /api/{project_id}/logs`, with the Project's existing keys. A `200` means the Logs are stored.
- **Storage:** Logs go to Pebble first, then to a DuckDB hot table per Project holding the current and previous hour, then to hourly Parquet files on local disk. Common Attributes become columns of their own automatically, so filters on them stay fast. Retention is in whole days, per Project, with an instance-wide disk limit.
- **Query:** one KQL-style query language, compiled by EventStore to DuckDB SQL. Operators can also turn on raw DuckDB SQL for every member.
- **Explorer:** a Console page per Project with a stream of terminal-style lines, filter chips, a search box, a slim histogram and an inspector. A `Builder | Query | SQL` switch moves from clicking to writing.

## User Stories

**Sending Logs**

1. As a developer using a Sentry SDK with logs turned on, I want my Logs stored instead of dropped, so that I get Logs without changing my setup.
2. As a developer, I want Sentry `log` items in the same envelope as an Event to be stored alongside that Event, so that one SDK request carries both.
3. As a developer shipping with Vector, I want to send Logs with its `http` sink and a JSON body, so that I need no plugin.
4. As a developer shipping with Fluent Bit, I want to send Logs with its `http` output in `format json`, so that I need no plugin.
5. As a developer, I want to POST a JSON array, a single JSON object, or NDJSON, so that any shipper's batching works.
6. As a developer, I want to authenticate with my Project's existing public key, in any form Sentry SDKs use or as `Authorization: Bearer <key>`, so that I manage no new tokens.
7. As a developer, I want a key for another Project to be rejected on the JSON route, so that a misconfigured shipper can't write into the wrong Project.
8. As a developer, I want gzip and zstd bodies accepted, so that shippers can compress.
9. As a developer, I want a body over 10 MB compressed or 50 MB decompressed rejected with `413` and nothing stored, so that I know to send smaller batches.
10. As a developer, I want one malformed line in an NDJSON batch to be dropped and counted rather than fail the batch, so that one bad record doesn't lose the rest.
11. As a developer, I want the JSON route's response to tell me how many Logs were dropped and truncated, so that I can spot a misbehaving sender.
12. As a developer, I want a Sentry envelope to always get `200`, as Sentry does, so that SDKs behave as they do against Sentry.
13. As a developer, I want overload to answer `503` with `Retry-After`, so that my shipper backs off and retries.
14. As a developer, I want a `200` to mean my Logs are stored, so that a crash or restart after the `200` loses nothing.

**What a Log is**

15. As a developer, I want my `msg`, `message`, `log` or `body` key to become the Log's message, so that common logger formats work unchanged.
16. As a developer, I want `time`, `timestamp`, `ts`, `@timestamp` or `_time` to become the Log's time, in ISO 8601 or a numeric epoch in s, ms, µs or ns, so that any logger's timestamp works.
17. As a developer, I want a Log without a timestamp to get the time SolidTrace received it, so that it still sorts.
18. As a developer, I want a timestamp older than my Project's Retention, or more than about an hour in the future, replaced with the receipt time and kept as `original.time`, so that clock errors don't hide Logs.
19. As a developer, I want `level`, `severity`, `lvl` or `log.level` mapped to one of six Levels, with common spellings (`warning`, `err`, `critical`, `notice` …) understood, so that every logger's levels line up.
20. As a developer, I want an unrecognised Level kept as `original.level` and the Log left with no Level, so that nothing I sent is lost.
21. As a developer, I want `service`/`service.name` and `environment`/`env`/`deployment.environment` to fill the Service and environment, so that I can tell sources apart within a Project.
22. As a developer, I want nested JSON flattened to dotted names, with `{"http": {"method": …}}` and `{"http.method": …}` meaning the same Attribute, so that I never write path syntax.
23. As a developer, I want each value to keep its own type, so that `status == 200` matches numbers and `status == "200"` matches strings.
24. As a developer, I want arrays kept as one value I can search with `contains`, so that tags and lists stay usable.
25. As a developer, I want a whole number too large for a double kept exactly, as a string, so that ids aren't rounded.
26. As a developer, I want an over-long message or value truncated at 64 KB and marked with `solidtrace.truncated`, rather than the Log dropped, so that I still see it.
27. As a developer, I want my own `solidtrace.*` keys kept as `original.solidtrace.*`, so that they never collide with what SolidTrace sets.
28. As a developer using a Sentry SDK, I want `body`, `timestamp`, `level`, `trace_id` and `sentry.environment` to fill the built-in fields and typed attributes unwrapped to plain values, so that Sentry Logs look like any other Logs.

**Storage, Retention and disk**

29. As an operator, I want Logs kept for 30 days by default, so that disk use is predictable without setup.
30. As an operator, I want to change the default Retention for the instance, so that I can fit my disk.
31. As an Organization admin, I want to set a Project's own Retention in whole days, so that a noisy Project can keep less.
32. As an operator, I want an instance-wide disk limit for Logs that deletes the oldest Logs first, from any Project, so that Logs never fill the disk.
33. As an operator, I want deleting a Project to delete its Logs, so that no orphaned data stays on disk.
34. As an operator, I want EventStore to absorb 5× the average ingest rate for 10 minutes without a `503` or lost Log, so that bursts are invisible to senders.
35. As an operator, I want Log checkpoints not to stall Event writes, so that Logs don't slow error tracking.
36. As an operator, I want the hourly export, Retention and the disk limit to run inside EventStore with no job to configure, so that Logs need no extra moving parts.
37. As an operator, I want a crash during an hourly export to leave no half-written file and lose no Log, so that a restart is always safe.
38. As an operator, I want to see in EventStore's logs when Log processing is behind, so that I can spot a backlog.

**Finding Logs in the explorer**

39. As an Organization member, I want a Logs page for each Project, so that I can explore Logs where I already explore Issues.
40. As an Organization member, I want the newest Logs to appear within about 2 seconds for any filter and range, so that the explorer feels live.
41. As an Organization member, I want each Log on one dense line (time, `[service]`, Level, message, a few `key=value` Fields), with errors and warnings marked on the left edge, so that I can scan many at once.
42. As an Organization member, I want to scroll back and have older Logs load 200 at a time, up to about 10,000, then be asked to narrow the range, so that scrolling stays smooth.
43. As an Organization member, I want to pick a Project and a time range at the start of the bar, so that every Query is scoped.
44. As an Organization member, I want to click any value in a line, including the Level and Service, and choose Show only this, Hide this or Copy value, so that I filter without typing.
45. As an Organization member, I want filters shown as chips (`field = value` / `field ≠ value`) whose operator I can flip and which I can remove, so that I see and edit what I'm filtering.
46. As an Organization member, I want Hide this to keep Logs that don't have that Field, so that hiding one value never hides unrelated Logs.
47. As an Organization member, I want a search box that finds text in the message and every string Attribute, case-insensitively, so that I can search without knowing Field names.
48. As an Organization member, I want a slim histogram of Logs over time, stacked by Level, so that I can see spikes.
49. As an Organization member, I want to drag across the histogram or click a bar to zoom the time range, so that I can narrow to a spike.
50. As an Organization member, I want a summary line with the Log count, the error rate and per-Level counts I can click to filter, so that I get the shape of the result at a glance.
51. As an Organization member, I want to open a Log in an inspector that overlays the stream, with Fields and JSON tabs and every value a filter tag, so that I can read one Log without losing my place.
52. As an Organization member, I want times shown in my browser's time zone, and the inspector to show the raw time in UTC, so that I read local times but can share exact ones.
53. As an Organization member, I want a day-long filter or group-by on a common Attribute to finish within about 5 seconds, so that I can investigate a whole day.
54. As an Organization member, I want a day-long search on a rare Attribute to stream matches newest first, so that I see results while it runs.
55. As an Organization member, I want a Cancel button while a Query runs, so that I can stop a slow one.

**Writing Queries**

56. As an Organization member, I want a `Builder | Query | SQL` switch, opening in Builder, so that I can start by clicking and move to writing.
57. As an Organization member, I want Query mode to open with my Builder's filters as Query text, so that I learn the language from what I clicked.
58. As an Organization member, I want to write `where`, `project`, `extend`, `summarize … by bin(time, …)`, `sort`, `take`, `top`, `count`, `distinct` and `search`, so that I can answer questions the Builder can't.
59. As an Organization member, I want `contains`, `startswith`, `matches regex`, `in`, `isempty` and the usual comparisons, so that common filters read naturally.
60. As an Organization member, I want pasted Axiom queries to often just work, so that I can bring queries with me.
61. As an Organization member, I want switching back to Builder to work silently when the Builder can show my Query, and otherwise to name what it can't show and offer Discard and switch or Stay in Query, so that I never lose work by surprise.
62. As an Organization member, I want Show only this and Hide this in Query mode to add a filter before any `summarize`, so that a pivot from a table narrows the Logs that feed it.
63. As an Organization member, I want a Query ending in `summarize`, `project`, `distinct`, `count` or `top` to show a table in place of the stream, with the histogram still counting the filtered Logs, so that I see both the answer and its source.
64. As an Organization member, I want a mistake in my Query shown under the Query box with its line and column, while the last good result stays, so that I can fix it in place.
65. As an Organization member, I want a run error to replace the result with its message, so that I know the Query failed.
66. As an Organization member, I want `dcount` and `percentile` documented as estimates, so that I read them correctly.

**SQL queries**

67. As an operator, I want SQL queries off by default and turned on by one instance setting whose description lists what members can then do, so that I opt in knowingly.
68. As an Organization member on an instance with SQL on, I want SQL mode to open with the compiled SQL of my current Query, so that I start from something that works.
69. As an Organization member, I want a documented `logs` view (`project_id`, the built-in fields, the Attributes) covering my bar's Project and time range, so that my SQL survives storage changes.
70. As an Organization member, I want leaving SQL mode with edits to ask before discarding them, so that I don't lose SQL by accident.
71. As an Organization member on an instance with SQL off, I want the switch to show only `Builder | Query`, so that I'm not offered what I can't use.

**Sharing and history**

72. As an Organization member, I want the time range, mode and Query text (and SQL text) in the URL, so that I can share exactly what I see.
73. As an Organization member, I want Back to undo my last run, so that I can step back through an investigation.
74. As an Organization member, I want a shared link containing SQL never to run on open, only when I press Run, so that a crafted link can't run SQL as me.
75. As an Organization member, I want a relative range such as "last hour" to stay relative in a shared link, so that the link shows the latest Logs.

**Maintainers**

76. As a SolidTrace maintainer, I want Log ingest and Log processing to mirror Event ingest and Event processing, so that one pattern is learnt once.
77. As a SolidTrace maintainer, I want the query compiler to be the only path from a Query to SQL, used by every widget, so that the explorer has one set of semantics.
78. As a SolidTrace maintainer, I want the promoted-set table to be the compiler's only source of which columns exist, so that queries never read Parquet footers.
79. As a SolidTrace maintainer, I want Logs to add no Go writes to Rails tables, so that ADR 0001's boundary holds.
80. As a SolidTrace maintainer, I want tests to drive time with a controllable clock, so that hourly and daily jobs are tested without sleeping.
81. As a SolidTrace maintainer, I want a load test on the target VM (4 vCPU, 16 GB) at 3–5 TB/month with 5× bursts, so that the design is proven at scale.

## Implementation Decisions

### Log ingest (EventStore)

From [Which ingest sources ship in v1, with what auth and limits?](../logs/issues/08-v1-ingest-sources-auth-limits.md), [What is a Log?](../logs/issues/07-log-domain-model.md) and [How are logs stored and retained?](../logs/issues/10-storage-architecture-and-retention.md).

- **Sources.**
  - **Envelope:** the existing envelope route stops dropping `log` items. Each `log` item's Logs (Sentry's item container of log records) are stored; an `event` item in the same envelope is handled exactly as today. An envelope always answers `200`, even when some of its Logs were dropped.
  - **JSON:** a new ingest route `POST /api/{project_id}/logs`. The body is a JSON array of objects, one object, or NDJSON (`application/x-ndjson`). Each object is one Log. An element or line that fails to parse is dropped and counted.
  - **Not in v1:** OTLP, Axiom-compatible paths.
- **Auth.** The Project's existing public key, in every `sentry_key=` form EventStore accepts today, on every route. The JSON route also accepts `Authorization: Bearer <public key>` and rejects a key whose Project differs from the path's `project_id` (`401`). Event routes keep today's behaviour.
- **Body.** Log routes accept `gzip`, `zstd` or identity and decompress in EventStore, not through Fiber's pass-through. Compressed body cap 10 MB, decompressed 50 MB; over either, `413` and nothing stored. Body limits (EventStore's server) are raised for Log routes only; Angie is removed from the deploy (amended 2026-10-02, [M1-11](issues/M1-11-remove-angie.md)).
- **Response.** The JSON route returns `200` with counts of Logs accepted, dropped and truncated.
- **Overload.** Logs share the existing `ingest_max_waiting` gate with Events. Every ingest `503`, Events included, gains a `Retry-After` of a few seconds. No `429`.
- **Normalisation** happens in the request, before the write, so Pebble holds normalised Logs:
  - **Built-in fields:** `time`, `level`, `message`, `service`, `environment`, `trace_id`, `span_id`. Everything else is an Attribute.
  - **JSON aliases**, the first present wins and is consumed (lower-ranked ones stay Attributes): `message` ← `message`, `msg`, `log`, `body`; `time` ← `time`, `timestamp`, `ts`, `@timestamp`, `_time` (RFC 3339 / ISO 8601, or a numeric epoch whose unit is inferred from its magnitude); `level` ← `level`, `severity`, `lvl`, `log.level`; `service` ← `service`, `service.name`; `environment` ← `environment`, `env`, `deployment.environment`; `trace_id` ← `trace_id`, `traceId`; `span_id` ← `span_id`, `spanId`.
  - **Sentry items:** `body` → `message`; `timestamp` (float seconds) → `time`; `level` and `trace_id` fill their fields; typed `{value, type}` attributes are unwrapped; `sentry.environment` fills `environment` and is consumed; other `sentry.*` keys stay Attributes; `service` comes from a `service.name` attribute.
  - **Levels:** `trace`, `debug`, `info`, `warn`, `error`, `fatal`, matched case-insensitively with `warning`→`warn`, `err`→`error`, `critical`/`crit`/`panic`/`emergency`→`fatal`, `notice`→`info`. A missing or unrecognised Level leaves the Log with none; there is no default.
  - **Time:** a missing timestamp, one older than the Project's Retention, or one more than 1 hour in the future becomes the receipt time.
  - **`original.<field>`:** a value that can't fill its built-in field (an unrecognised Level, a replaced time, a malformed trace id) is kept as that Attribute.
  - **`message`** is always a string, possibly empty.
  - **Flattening:** nested objects become dotted names; if `{"a.b": …}` and `{"a": {"b": …}}` both appear, the literal dotted key wins ([ADR 0002](../../docs/adr/0002-logs-scoped-by-project-flat-attributes.md)). Arrays are one value; objects inside arrays are not flattened.
  - **Types:** each value keeps its type. A whole number a DOUBLE can't hold exactly is stored as a string.
  - **Limits, truncate and keep:** strings (including `message`) truncated at 64 KB; Attribute names over 256 B dropped; beyond 1,000 Attributes the excess is dropped in a stable order; nesting past 10 levels kept as a JSON string from that depth; a Log still over 1 MB is dropped. A truncated Log carries `solidtrace.truncated = true`.
  - **Reserved prefix:** a sender's `solidtrace.*` key becomes `original.solidtrace.*`.
  - **Identity:** each Log gets a UUIDv7 id at ingest. No deduplication.
- **The write.** A request's normalised Logs are stored as one Pebble entry (a Log batch), written through the same process-wide writer and mutex as Events, with `NoSync`, then the request answers. Log batch keys live in their own keyspace, ordered per Project by UUIDv7 and never colliding with Event keys or processing cursors. Writing wakes Log processing as Event writes wake Event processing.

### Log processing (EventStore)

From [How are logs stored and retained?](../logs/issues/10-storage-architecture-and-retention.md) and [ADR 0005](../../docs/adr/0005-logs-per-project-hot-tables-promote-at-ingest.md).

- **What it is.** One worker, separate from Event processing, mirroring it: woken by writes and a 1 s tick, reading each Project's Log batches in order from that Project's processing cursor.
- **Per batch, in one DuckDB transaction in `logs.duckdb`:**
  1. If the batch holds the first Log of a new UTC day for the Project, compute that day's promoted set (below) and apply its `ALTER TABLE`s.
  2. Promote any new (type, key) pair while the Project's set has room: `ALTER TABLE ADD COLUMN` and a promoted-set row recording when it was added.
  3. Append each Log to the Project's hot table if its `time` is inside the hot window (from the start of the previous hour to the start of the next), else to `logs_late`.
  4. Add the batch's key counts to the popularity counts.
  5. Move the Project's Log processing cursor.

  Then delete the Pebble batches up to the cursor. Keeping the cursor in `logs.duckdb`, committed with the rows, makes a crash between commit and delete harmless: replay skips batches at or below the cursor. (See Further Notes: this differs from Event processing, whose cursor is in Pebble.)
- **Appenders.** One per Project hot table, reopened after any `ALTER TABLE` on it.
- **Errors** follow Event processing: store errors retry with backoff and skip nothing; a batch that fails for its content is retried, then isolated Log by Log, and a Log that can never be appended is skipped and logged. A periodic log line names Projects whose oldest unprocessed batch is older than a threshold.

### Log storage (EventStore)

From [How are logs stored and retained?](../logs/issues/10-storage-architecture-and-retention.md), [ADR 0005](../../docs/adr/0005-logs-per-project-hot-tables-promote-at-ingest.md), and the benchmarks [09](../logs/research/09-storage-bench-reduced.md) and [14](../logs/research/14-storage-design-bench.md).

- **Engine:** DuckDB 1.5.6 (already in place). `logs.duckdb` is a separate file attached to the same DuckDB instance as `events.duckdb`: its own WAL and checkpoints, a shared memory limit.
- **Hot tables:** one per Project, created with its first Log. Columns: `id`, the seven built-in fields, four typed maps (`attrs_string`, `attrs_number` DOUBLE, `attrs_bool`, `attrs_json` for arrays and deep nesting as JSON text), plus one typed column per promoted (type, key), named `"<map>.<key>"` (for example `"attrs_string.http.status"`). A promoted value is stored in its column and not in its map.
- **`logs_late`:** one shared table with the same fixed columns, maps only, never promoted.
- **Promotion** ([ADR 0005](../../docs/adr/0005-logs-per-project-hot-tables-promote-at-ingest.md)):
  - Up to **100 (type, key) pairs per Project**; `attrs_json` keys never; built-in fields don't count.
  - **A Log's day** is the UTC day of its `time`. Each (Project, day) has one promoted set, kept as long as Retention keeps the day.
  - **New keys** are promoted at ingest while the set has room; past 100 they stay in the maps.
  - **Daily set:** when the first Log of a new day arrives, the new day's set is yesterday's unchanged if it wasn't full; otherwise the top 100 keys from the most recent day with counts.
  - **Popularity:** the number of Logs carrying a key per Project per UTC day, kept as a top-K of about 1,000 keys per Project per day.
  - **Demoted columns** are dropped at the export that deletes the previous day's last hour (01:00 UTC), in the same transaction.
  - **The promoted-set table** (Project, day, type, key, time added) lives in `logs.duckdb` and is cached in memory by EventStore.
- **Hourly export** (a timer inside EventStore, at each hour start, for hour h-2):
  - Per Project, one zstd Parquet file under `project=<id>/day=<d>/hour=<h>/data.parquet`, 30,720-row row groups, sorted by time, with that day's promoted columns. `logs_late` rows for that hour and Project join it.
  - `logs_late` rows for hours already exported become `late-<n>.parquet` in that hour's folder, with the day's promoted columns (values pulled out of the maps).
  - The rows are deleted from the hot table and `logs_late`, then a checkpoint runs.
  - Files are written under a temporary name and renamed after the delete commits; startup recovers or removes staged files, as the Event archive does.
- **Retention:** whole days, an instance default (30) and an optional per-Project override, read from Console's SQLite. A daily pass deletes each Project's `day=` folders past its Retention, with those days' promoted-set and count rows.
- **Disk limit:** one instance-wide limit for Log files; when exceeded, the oldest hour files are deleted first, from any Project.
- **Project deletion:** when a Project's row disappears from Console's `projects` table, EventStore drops its hot table, its `logs_late` rows, its folders and its promoted-set and count rows (see Further Notes).
- **Expected disk** at 1.9 MB/s: Parquet about 28–30 GB/day (about 0.9 TB for 30 days); hot tables a few GB; Pebble seconds of Logs, up to about 6 GB after a 10-minute 5× burst.

### Query compiler and query endpoints (EventStore)

From [What query language(s) do users write?](../logs/issues/11-query-language.md), [How do the Builder, APL and SQL modes relate?](../logs/issues/12-query-builder-ux.md), [How do log queries run?](../logs/issues/15-how-log-queries-run.md) and [How does the explorer show non-Log results and keep its state?](../logs/issues/16-explorer-building-blocks.md).

- **The language:** KQL-style, unbranded (never "APL" or "KQL" in the UI); KQL's semantics win where KQL and APL differ.
  - `where` with `and`/`or`/`not`, comparisons, `contains` (case-insensitive), `contains_cs`, `startswith`, `matches regex`, `in`/`!in`, `isempty`/`isnotempty`.
  - `project`, `extend`; `summarize` with `count`, `dcount`, `sum`, `avg`, `min`, `max`, `percentile`, by Fields and `bin(time, …)`; `sort`, `take`, `top`, `count`, `distinct`, `search`. Unknown functions are rejected.
  - A leading `logs |` is optional. The Project and time range come from the request; a Query may narrow the range, never widen it or leave the Project.
  - `==` never coerces; `!=` and `!in` include Logs missing the Field; a filter built from a tag uses that value's own type. The search box is a case-insensitive `contains` over `message` and every string Attribute.
- **The compiler** is hand-written Go in EventStore, borrowing `runreveal/pql`'s structure. It is the single path from a Query to SQL: the stream, chips, search, histogram, summary line and per-Level counts are all Queries.
  - **Columns** come only from the cached promoted-set table. It compiles one branch per day: the column for promoted keys, `coalesce(column, map[key])` for keys added during that day (that day only), maps only for `logs_late`. No Parquet footer reads.
  - **"Latest N"** scans newest first, hour by hour, and stops once it has N rows; older pages continue from a (time, id) cursor.
  - **Readable SQL:** the compiler also prints the Query as readable SQL against the `logs` view (one CTE per stage), for SQL mode.
- **Endpoints on the query app** (shape is the implementer's; these are the operations):
  - **Run a Query** for a Project and time range: Log rows with a next-page cursor, or a table for non-Log results.
  - **Histogram and summary** for the same filters: counts per time bucket per Level, the Log count and per-Level counts.
  - **Validate a Query:** errors with line and column; and, when the Query is only `where field ==|!= literal` stages and one `search`, its chips. The browser never parses Queries itself.
  - **Fetch one Log** by id, for the inspector and links.
  - **Run SQL** for a Project and time range, only when the instance setting is on: EventStore builds the `logs` view as a temporary view on that query's connection (the Project's hot table, its days in Parquet and its `logs_late` rows, promoted columns merged back into the four maps) and runs the SQL as written.
  - **Readable SQL** for a Query, to seed SQL mode.
- **Execution** ([ticket 15](../logs/issues/15-how-log-queries-run.md)): no per-query timeouts in v1; a run stops when the user cancels. Routing and authorization, whether cancel stops the query on the server, and result caps are the implementer's call. Precedent: Console calls the loopback-only query app, as Event queries do.

### Console

- **Settings storage.** Console has no instance-level settings today. Add one instance settings record holding **SQL queries enabled** (default off) and the **default Log Retention** in days (default 30), and a nullable **Log Retention** column on Projects. EventStore reads them read-only from Console's SQLite, as it reads `project_keys`; Logs add no Go writes to Rails tables ([ADR 0001](../../docs/adr/0001-eventstore-writes-console-tables.md)). The SQL setting's description lists what turning it on allows ([ADR 0003](../../docs/adr/0003-unrestricted-sql-queries-behind-an-instance-setting.md)): any member can read every Organization's Logs and Events and files on the server, write files, drop tables and starve ingest.
- **Who edits what.** A Project's Retention: Organization admins, in Project settings. Instance settings: the operator (see Further Notes).
- **Logs explorer page**, per Project, under the existing Organization and Project routes, behind the existing membership check. A "Logs" entry sits next to Issues.
- **Proxying.** Console's EventStore client gains Log operations mirroring the query endpoints above. SQL operations are refused by Console as well when the setting is off.

### Logs explorer (Console frontend)

From [How should the Logs explorer be laid out?](../logs/issues/06-logs-explorer-layout.md), [How do the Builder, APL and SQL modes relate?](../logs/issues/12-query-builder-ux.md) and [How does the explorer show non-Log results and keep its state?](../logs/issues/16-explorer-building-blocks.md). Prototypes: branches `prototype/logs-explorer-layout` (variant D) and `prototype/logs-query-modes` (variant B).

- **Bar:** Project and time-range selects, then the `Builder | Query | SQL` switch (`Builder | Query` when SQL is off), then the mode's controls. Opens in Builder.
- **Builder:** filter chips (`field = value` / `field ≠ value`; click the operator to flip, × to remove) and a search box. No level toggles, no summarize controls.
- **Query mode:** an editable Query text box holding the Builder's Query. Query → Builder switches silently when the validate endpoint returns chips; otherwise it names the stages the Builder can't show and offers **Discard and switch** or **Stay in Query**. Pivots add a `where` stage after the leading filters, before any `summarize`, and re-run.
- **SQL mode:** opens with the readable SQL of the current Query, editable, run only on Run. One-way: leaving with edits asks to discard. Chips and histogram are hidden.
- **Histogram:** a slim strip (about 48 px of bars) stacked by Level, with time labels; drag to zoom or click a bar. Bucket size follows the range. Always shown outside SQL mode, counting the Logs that pass the filters, even above a table.
- **Summary line:** Log count, error rate = (error + fatal) ÷ Log count, and per-Level counts that filter on click. No p95.
- **Stream:** dense terminal-style lines, newest first: time, `[service]`, Level, message, a few `key=value` Fields; errors and warnings get a coloured left edge. Every value is a tag with Show only this, Hide this, Copy value. Loads 200, then 200 older per scroll, up to about 10,000, then "Narrow the time range to see older Logs". Virtualised.
- **Non-Log results:** a table in place of the stream. No charts in v1.
- **Inspector:** clicking a line opens an overlay on the right of the stream (it does not narrow it), with Fields and JSON tabs; every value is a tag; the raw `time` is shown in UTC ISO.
- **Time:** the browser's local time on screen; absolute ranges in the URL as UTC ISO timestamps.
- **URL:** the time range as given (relative stays relative), the mode, the Query text and the SQL text; the Project is in the path. Each run pushes a history entry; typing doesn't. SQL never runs on page load.
- **Errors:** compile errors under the Query box with line and column, keeping the last good result; run errors replace the result area; a Cancel button while a Query runs.
- **Libraries:** the implementer researches and chooses the virtualised list, histogram approach and inspector drawer, compatible with shadcn (base-ui) and the existing React 19, Inertia 2 and Tailwind stack. Requirements: smooth with 10,000 lines; the inspector overlays the stream. Trap from the prototype: base-ui's `DropdownMenuLabel` throws outside a `DropdownMenuGroup`.

### Glossary (`GLOSSARY.md`)

- New **Log ingest** and **Log processing**, defined like **Event ingest** and **Event processing**; **Processing cursor** widened to cover Log processing.
- New **Logs explorer**: the Console page where a Project's Logs are searched and read. **Builder**, **Query** and **SQL query** already describe its modes.

## Testing Decisions

- **A good test** acts as a sender or a user would and checks what they can observe: status codes and response bodies from ingest, and the rows a Query, the histogram or SQL returns. It doesn't assert on generated SQL, table columns, Parquet files, appenders or call order. Storage shape (promotion, export, late Logs) is tested by showing that Queries return the same rows before and after the hour is exported, the day changes or a key is demoted.
- **Seam 1: the EventStore test environment** (main seam), extended from the existing integration and processing tests:
  - Real Pebble, DuckDB (`events.duckdb` with `logs.duckdb` attached) and SQLite in temp dirs; the ingest app and the query app.
  - Event processing and Log processing driven by explicit "process until caught up" steps.
  - **A controllable clock** (the one new substitution point) drives the hourly export, the daily set, Retention and the disk limit, and decides what is late.
  - **Cases:** each source and body form; Bearer and `sentry_key` auth and the Project mismatch `401`; gzip and zstd; `413` on both caps; dropped and truncated counts; every alias and Level spelling; `original.*`; flattening and the dotted-key clash; type preservation and large integers; the 64 KB, 256 B, 1,000-Attribute, depth-10 and 1 MB rules; `Retry-After` on `503`; a `200` survives a restart before processing; a crash between the DuckDB commit and the Pebble delete creates no duplicates; promotion up to 100 and the daily re-rank; a key promoted partway through a day; late and future Logs; Queries agree across the hourly export; Retention and the disk limit delete the right files; Project deletion.
  - **Language cases:** each operator and function; `==` without coercion; `!=` keeping missing Fields; search; narrowing but never widening the range; validate returning errors with positions and chips only for Builder-showable Queries.
  - **Cross-check:** for a set of Queries, the readable SQL run through the SQL endpoint returns the same rows as the Query.
- **Seam 2: Console request tests** (Minitest, WebMock stubbing EventStore): only Organization members open the explorer; SQL operations are refused and hidden while the setting is off; Organization admins change a Project's Retention, members can't; the explorer passes the Project and time range through.
- **Seam 3: vitest for the explorer's state logic** (new; Console has no frontend tests today): URL ⇄ explorer state (relative and absolute ranges, mode, Query and SQL text), history entries per run and not per keystroke, SQL never auto-running from a URL, and the mode-switch rules (silent switch when chips are returned, Discard/Stay otherwise, SQL's one-way discard prompt). Keep this logic in plain modules the components call, so it is tested without rendering.
- **Load test** (a first-milestone ticket, not a prerequisite for this spec): the target VM (4 vCPU, 16 GB) at 3–5 TB/month with 5× bursts for 10 minutes. It measures the 2 s and 5 s targets, per-Project hot tables, the daily set change, and the checkpoint after each hourly delete at full scale (0.4–3 s at reduced scale).
- **Prior art:** `processing_test.go` and `integration_test.go` (environment, "process until caught up", envelope ingest), `routes_test.go` (real stores in temp dirs via Fiber's `app.Test`), `ingest/service_test.go` (envelope parsing, overload), `storage/duckdb_test.go` (archive and staged-file recovery); Console's `issues_controller_test.rb` and `services/event_store_test.rb`.

## Out of Scope

- **v2** (the map's fog): OTLP/HTTP ingest; Axiom-compatible ingest; an S3/GCS tier (with a DuckLake re-check); the DuckDB 2.0 upgrade and VARIANT; a search index for rare-key day searches; isolating Logs from Events (a Log flood failing Event ingest); metrics, health reporting and disk alarms; live tail; duplicate detection; saved and shared queries; dashboards, including charts for `summarize … by bin(time, …)`; monitors; Log ↔ Issue/Event correlation; traces and metrics.
- **Not planned:** horizontal scaling and HA; hosted multi-tenant SaaS; permissions finer than Organization membership; AI query generation; profiling and RUM; Loki and Elasticsearch-bulk ingest.
- **Deferred past v1:** per-query timeouts; a sandbox for SQL queries (ADR 0003); `429` and per-Project quotas; a `ValidateKey` cache; Event retention (it can reuse this mechanism; prod-readiness §9).
- **Data migration:** nothing to migrate.

## Further Notes

- **Choices made in this spec, not by a ticket** (challenge any of them):
  - **The Log processing cursor lives in `logs.duckdb`**, committed with the rows, counts and promoted-set changes, so replay never appends twice. Ticket 10 said "as Event processing does", whose cursor is in Pebble; the Event spec kept DuckDB analytical only, but `logs.duckdb` already holds the counts and promoted sets in the same transaction.
  - **Instance settings are edited by the operator, not in the UI.** Console has no instance-admin role (roles are per Organization), so the SQL setting and default Retention are set with a Rails task or console command. A UI waits for an instance-admin role.
  - **The disk limit** is a setting beside them (or EventStore config); the implementer picks, with a sensible default such as a share of free disk.
  - **The future-time window** is fixed at 1 hour (ticket 07 left configurability to the spec).
  - **Project deletion** is detected by EventStore when a Project's row vanishes from `projects`, since Console doesn't tell EventStore today. Not when its keys vanish (amended 2026-10-02 at ticketing): keys are hard-deleted, so deleting a Project's last key would have wiped its Logs. Console has no Project destroy route yet.
  - **Validate returns chips**, so the Builder never needs a second parser in the browser.
  - **A Project-mismatch key** on the JSON route answers `401`.
- **Known risks, for the load test:** the checkpoint after each hourly delete may grow with rows deleted (Log processing stalls; Pebble buffers); 3–4 s checkpoint stalls were seen at 1.5.6; the 5 s day target was projected (4–7 s), not measured.
- **Proposed milestones** (for [Write the v1 spec and first-milestone tickets](../logs/issues/13-write-v1-spec-and-first-milestone-tickets.md)):
  1. **Logs are stored.** Log ingest (both sources, auth, body rules, normalisation), Log processing, hot tables with promotion, `logs_late`, hourly export, Retention, the disk limit, Console settings storage, and a minimal "latest N for a Project" query; plus the load test.
  2. **Logs explorer with the Builder.** The compiler (filters and search first), run/histogram/summary/validate/fetch endpoints, the Console page with stream, chips, search, histogram, summary line, inspector and URL state; vitest.
  3. **Query and SQL modes.** The full language surface, non-Log tables, pivots, readable SQL, the SQL setting and the `logs` view.
- **Benchmark harness:** `event_store/bench/logslayouts` is throwaway and imported by nothing in production.

## Comments

### 2026-10-02 tickets written

Tickets for all three milestones are in [issues/](issues/), named by milestone (`M1-01` … `M3-04`), written with `/to-tickets`. Project deletion is now detected by the `projects` row, not the keys (Further Notes amended). M1-10 (load test) is `ready-for-human`: it needs the target VM.

### 2026-10-02 Angie removed

At the user's request, during implementation: [M1-11](issues/M1-11-remove-angie.md) removes Angie (nginx) from the deploy. EventStore's ingest app listens on TCP itself and Console serves through Thruster, so the Log routes' body limits live only in EventStore. M1-04 and the Body bullet are amended.

### 2026-10-03 code review

Fixes from the code review of `feature/logs-v1` (branch `logs-v1/review-fixes`). One line per item.

**Must fix**
1. **A long read holds up nothing** (`9293e59`). Each read (`storage/logs_read.go`, `logRead`) opens a DuckDB transaction on its own connection and lists and pins the Log files it needs, both under `publishMu` only while it begins, then runs without the lock. An export commits and renames while it runs; DuckDB's snapshot still shows the read the Logs as they were. Retention, the disk limit and Project deletion leave pinned files and return `ErrLogFilesInUse`, and the cleanup retries a minute later. Choice: `RunSQL`'s SQL now runs inside that transaction, which commits when the run ends, so a `BEGIN` in user SQL fails. **Cancel:** the DuckDB driver interrupts a query when its context ends, and every read passes the request context. A context tied to the client's connection isn't feasible end to end, so the query isn't stopped when the client goes away. Fiber v2 (fasthttp) gives handlers no context that ends with the connection. Puma doesn't tell a Rails action that the browser left, so Console's call to EventStore runs on after the explorer's Cancel anyway.
2. **No read timeout on Log runs** (`1cc1ad7`). `query_logs`, `logs_histogram` and `run_logs_sql` pass `read_timeout: nil`; the open timeout and the Event calls are unchanged.
3. **`contains` matches arrays** (`3ebe6eb`). `contains`, `contains_cs` and their negations on an Attribute read its string, or else its JSON text, so `tags contains "db"` matches `["api","db"]`. Choice: `search` is unchanged, because the spec scopes it to the message and string Attributes. The docs say to use `where … contains` for arrays.
4. **Stable disk-limit default** (`af246cc`). With `logs_disk_limit_gb` unset, the limit is half the size of the disk holding the Log files, read once at startup (`storage.NewLogDiskLimit`). Why half: Logs are the largest thing on the disk, but Pebble, the Event databases and archive, the hot tables and the system share it. Half leaves them room and doesn't move when they grow.
5. **Monotonic Log batch keys** (`8790d77`). A batch id is a UUIDv7 of the receipt time from the injected clock. If it doesn't sort after every id written, stored or used as a cursor, it becomes the next id after them. Pebble seeds that floor from its newest batches when it opens, and Log processing's `Prepare` raises it to every cursor before ingest starts. Side effect: after a clock step back, the ids carry the floor's time, so the backlog line under-reports their age until the clock catches up.
6. **An empty `projects` table deletes nothing** (`b39553b`). The daily pass logs a warning and deletes no Project's Logs while the table has no rows.
7. **Rare searches stream** (`a4dca66`). A run of Log rows that has read for 1.5 s (`LogsHandler.ScanBudget`), with at least one hour read, answers with what it found. Its `next_cursor` is the start of the last hour it read, encoded as that time with the nil id. Console's explorer (`fillStream` in `lib/logs/stream.ts`) shows each answer and keeps fetching while the stream is short of its target and a cursor remains. The target is 200, then 200 more per scroll, up to 10,000. Cancel keeps the Logs found so far and offers Try again. Paging, `take` and the 10,000 cap are unchanged.
8. **An envelope answers 200 once its Event is stored** (`4ae82ce`). After the Event is written, a failed Retention read or Log write is logged and the Logs dropped. Choice: a Logs-only envelope whose write fails still fails, because nothing of it is stored.

**Decided by the user 2026-10-03**
- **A. Console on port 3000** (`252011c`). Thruster listens on 3000 (`HTTP_PORT`) and runs Puma on 3001 (`TARGET_PORT`). These are set in compose and in the image, whose `EXPOSE` is now 3000. The sysctl note is gone. The dev DSN in `ProjectKey#dsn` (`localhost:3000`) doesn't conflict and is left for its deferred item.
- **B. Compose dependency cycle** (`8929ba3`). `db_prepare` has `depends_on: []`. `docker compose config` parses the file; it failed with "dependency cycle detected" before.
- **C. Decode after the gate** (`ba7c193`). Ingest takes the body as an `ingest.Body`, decoded only once the request holds a waiting place, so overload answers 503 with `Retry-After` before any decompression. The as-sent cap (413) and the encoding check (415) still answer before the gate. The decoded cap (413) and undecodable bodies (400) answer from the decode.

**Cleanups**
- `DeleteExpiredLogs` is `DeleteLogsPastRetention`, and its messages speak of Retention (`289d31e`).
- One `sqltext.String` and one `sqltext.Identifier` in `pkg/sqltext`. The two JSON conversions are now `jsonEncodable` (storage) and `tableCell` (logquery) (`24af039`).
- `logprocessing/worker.go` holds the Start/Stop lifecycle the three timers share (`bb77941`).
- `rangeProblem` checks every Log request's time range. The readable-SQL comment says Console gates it (`ec0f66c`).
- `LogsController` uses `rescue_from` and a `set_time_range` before_action (`398cd02`).
- `EventStore.post!` reads a 403 as `SQLQueriesOff` only for the SQL endpoints; elsewhere it's `Unavailable` (`ac4ca99`).
- `api.ts` fetches through one `callConsole` helper (`c479d51`).

## Implementation decisions

Decisions made during implementation that depart from this spec, or from what it assumed, with the reasoning. The user made them on 2026-10-03. Each one lists where the change landed.

1. **Console listens on port 3000; there's no reverse proxy in front.**
   - **What the spec assumed:** Angie in front of both services. The Body bullet originally said Log routes' limits are raised "in EventStore's server and Angie". [M1-11](issues/M1-11-remove-angie.md) removed Angie, so EventStore's ingest app listens on TCP 4000 itself. Console then had to bind port 80 directly.
   - **Decision:** Console's Thruster listens on **3000**, with Puma behind it on 3001.
   - **Reasoning:**
     - Console runs as a non-root user (uid 1000) under `network_mode: host`, so it can't bind a port below 1024.
     - `cap_add: NET_BIND_SERVICE` doesn't help a non-root user.
     - The alternatives were a host `sysctl` (an easily forgotten setup step), `setcap` in the image (untested here) or running as root. A high port needs neither.
   - **Consequence:** whatever sits in front of the VM must now send Console traffic to port 3000 rather than 80. That could be a cloud load balancer, a firewall rule or a TLS terminator.
   - **Where it landed:** `252011c`, plus M1-11's Comments.
2. **Every Project edit is admin-only, not just Retention.**
   - **What the spec says:** "A Project's Retention: Organization admins, in Project settings."
   - **Decision:** [M1-02](issues/M1-02-console-instance-settings-and-project-retention.md) added a `ProjectPolicy` that makes the whole Project update admin-only. Members can no longer rename a Project or change its platform either.
   - **Reasoning:**
     - Retention is edited on the same Project settings form and update action as the other fields.
     - Gating the action is simpler and harder to get wrong than gating individual fields.
     - Editing a Project is an admin task in practice.
   - **Where it landed:** M1-02.
3. **The stream is a table with columns the reader picks, and its values aren't tags.**
   - **What the spec says:** the Stream bullet asks for "dense terminal-style lines … time, `[service]`, Level, message, a few `key=value` Fields", and "Every value is a tag". Story 44 asks to "click any value in a line … and choose Show only this, Hide this or Copy value".
   - **Decision:** the stream is a virtualised table.
     - It opens with time, service, Level and message.
     - A column picker adds and removes built-in fields, the Attributes in view, or any Field typed by name. "Add as column" in the inspector's tag menu does the same.
     - Columns move by dragging their headers, resize by dragging their edges, and are kept in the URL as one `col` per column, so a shared link shows them. Changing them replaces the history entry, because it's a view setting, not a run. Dragged widths are kept in the browser.
     - Columns keep their width, with lines between them; the space past the last column stays empty.
     - Each row starts with a small square in its Level's colour, in place of the coloured left edge.
     - Hovering a cell offers Copy. Clicking anywhere else on a row opens the inspector. The inspector's values are still tags with Show only this, Hide this and Copy value, and so are a tabular Query's cells.
   - **Reasoning:** the user asked for both changes on 2026-10-03, after using the explorer, and shared a screenshot of the table they wanted.
     - Tags covered almost the whole line, so there was little space that opened the inspector. Filtering from a row now takes one more click, through the inspector.
     - Columns let the reader line up the Attributes that matter to them, which inline `key=value` Fields couldn't.
   - **Where it landed:** `LogStream.tsx`, `lib/logs/columns.ts` (with vitest tests), and the tag menu's "Add as column".

4. **The Builder is a panel of clauses: Where, Summarize and More.**
   - **What the spec says:** the Builder bullet says "filter chips (`field = value` / `field ≠ value`; click the operator to flip, × to remove) and a search box. No level toggles, no summarize controls." Story 56 calls the Builder the place to "start by clicking".
   - **Decision:** in Builder mode a panel under the bar holds three rows, after the user's screenshot of Axiom's query builder.
     - **Where:** filters with `==`, `!=`, `>`, `>=`, `<`, `<=`, `contains`, `!contains`, `startswith`, `!startswith`, `in`, `!in`, exists (`isnotnull`) and missing (`isnull`), one `where` stage each. Values keep their type, and quoting a value makes it a string.
     - **Summarize:** `count`, `dcount`, `sum`, `avg`, `min`, `max` and `percentile` of one Field, by Fields and `bin(time, …)` or `bin_auto(time)`. A Summarize shows a table, as in Query mode, and the histogram still counts the filtered Logs.
     - **More:** the `search` (the message and every string Attribute, ignoring case), which was a search box in the bar until the user asked to move it here; `sort by` columns (a Summarize's own columns when there is one) and a `take` limit. Once added, each has a row of its own (Search, Sort by, Limit) above More, as the user asked; Sort by has its own + for further keys. More offers only what's not there yet, and goes when all three are.
     - Each clause is edited in a small popover and runs at once; × removes it. Show only this and Hide this still add `==` / `!=` filters.
     - A + adds a clause in steps, each a list to type into and pick from: a Field, then the operator and value for a filter; a function, then its Field; time bins or a Field to group by; search, sort or limit under More. Picking a suggested value applies a filter at once (in a list it's added or taken out). A Field not in the list can still be typed.
     - Fields and values are suggested from up to 2,000 Logs of the recent runs, newest first, so a Query narrowed by a filter still suggests the values the filter left out.
   - **How it round-trips:** the Builder is still read back only from validate. Its `chips` now carry `summarize`, `sort` and `take` beside `filters` and `search`, read from the Query's syntax tree, so `in` and `isnull` come back as written. A stage counts as showable (`builder: true`) where it stands: filters first, then at most one each of `summarize`, `sort` and `take`, in that order. Anything else (named columns, `countif`, `project`, `top`, `or`, regexes, `where` after the tail) still sends the Query to Query mode.
   - **Reasoning:** the user asked for it on 2026-10-03, so that most questions can be built by clicking rather than writing a Query.
     - The steps came after the user's screenshots of the first version, the same day: the Project list was as narrow as its button, so the name was clipped; the Field suggestions covered the operator, value and Apply; and More's active tab looked like its disabled Apply.
   - **Where it landed:** `logquery/chips.go` and the validate tests, `lib/logs/builder.ts` and `lib/logs/pick.ts` (with vitest tests), `BuilderPanel.tsx` and `PickList.tsx`.

Two of the user's 2026-10-03 decisions don't change this spec's behaviour, and are noted here for completeness:
- **Fixing the compose dependency cycle** (`8929ba3`). It was a deploy bug that predated this spec.
- **Decoding ingest bodies only after the `ingest_max_waiting` gate** (`ba7c193`). The Overload bullet says Logs share that gate but doesn't fix the order. Decoding after it means overload answers `503` with `Retry-After` before any memory is spent on decompression. The `413` and `415` rules are unchanged.
