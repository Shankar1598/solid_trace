# SolidTrace Logs

Label: wayfinder:map

## Destination

A spec for SolidTrace Logs v1 (plus a separate v2 spec for the full vision) and an ordered set of tickets for v1's first milestone. Every product and architecture decision in the specs traces to a resolved ticket on this map; decisions that are hard to reverse are recorded as ADRs. The destination is documents, not working code: benchmarks and spikes appear only as tasks or prototypes that a decision waits on.

## Notes

- **Domain:** self-hosted log ingest, storage, query and exploration for SolidTrace, alongside the existing error tracking. Tickets are worked in `.scratch/logs/issues/` per `docs/agents/issue-tracker.md` ("Wayfinding operations").
- **Scope:** v1 = ingest, storage and retention, query language, Logs explorer, query builder, raw DuckDB SQL mode (unrestricted, opt-in per instance; ADR 0003). v2 = live tail, saved and shared queries, dashboards, monitors, log ↔ Issue/Event correlation, traces and metrics.
- **Fixed principles:** Rails (Console) for admin logic, Go (EventStore) for performance paths, shadcn for UI; zero-config ingest (no required indexing or schema, every JSON field queryable at once, as in Axiom); logs live inside the two existing services, no third service. DuckDB as the engine and S3/GCS tiering for logs are **open**, to be decided on this map.
- **Target scale (decided 2026-09-19, replaces the self-contradictory 10k logs/s figure):** one VM ingesting 3–5 TB of uncompressed log data per month. That averages 100–170 GB/day, or 1.2–1.9 MB/s; at a typical 300 B–1 KB per log it is roughly 1–6k logs/s on average. Burst headroom above the average is still to be decided.
- **Engine line (decided 2026-09-19):** target DuckDB 2.0, not 1.4 LTS or 1.5. As of 2026-09-19 it is a preview ("coming this fall", [2.0 highlights](https://duckdb.org/2026/08/17/duckdb-20-highlights)), so benchmarks run on preview or RC builds, and duckdb-go support for 2.0 is a risk to check rather than assume. **Checked 2026-09-27** ([upgrade-pebble-duckdb 01](../upgrade-pebble-duckdb/issues/01-upgrade-pebble-and-duckdb.md)): duckdb-go has no build of the real 2.0 alpha; reaching it from Go means linking the alpha `libduckdb.so` with `-tags=duckdb_use_lib`. The `v2.20000.0-N.preview` tags are engine 1.5.4 plus 2.0 backports, not 2.0. EventStore now links the real alpha `v2.0.0-alpha43385` through `event_store/scripts/fetch-duckdb`, until `v2.20000.0` ships with 2.0.0 (due 2026-10-21). Quack (2.0 as a separate server) cannot be reached from any 1.5.x Go client, because the protocol changed.
- **Stance on Axiom:** Axiom-inspired, not Axiom-compatible. Match its ease of use and APL feel; how much compatibility would cost is a research fact, not a goal.
- **Prior material is reference only.** The 2026-09-13 design was made without this map; none of its decisions stand until a ticket here makes them. Old spec, architecture and tickets: `.scratch/logging-old/`. Its research and codebase surveys (not re-verified): `.scratch/logging-old/research/`. Partial code, ADRs 0001–0005 and old glossary terms: branch `backup/logging-v0`.
- **Production readiness:** gaps in `.scratch/prod-readiness/review.md` are open for discussion; when the logs work depends on one, discuss it and bring it into scope rather than treating it as someone else's work.
- **Rails tables (decided 2026-10-01):** logs add no new Go writes to Rails tables; they only read Project keys (review §8).
- **Skills:** grilling tickets call `grilling` and `domain-modeling` (glossary terms go into `CONTEXT.md` as they resolve; hard-to-reverse choices become ADRs in `docs/adr/`). Prototype tickets call `prototype`. Research tickets run as subagents calling `research`, with findings on a `research/<name>` branch and a pointer from the ticket.

## Decisions so far

<!-- one line per resolved ticket: [title](issues/NN-slug.md): gist -->

- [How do Axiom and Better Stack work for logs?](issues/01-axiom-and-better-stack-teardown.md): Axiom auto-columnises every field (no user indexes); Better Stack needs extraction rules for speed. Axiom ingest compatibility is small and bounded; pasteable APL is open-ended. `runreveal/pql` (Go, Apache-2.0) is the closest existing KQL-style→SQL compiler.
- [How can DuckDB store schemaless logs at 10k logs/s?](issues/03-duckdb-schemaless-storage-options.md): no hard blocker. VARIANT needs DuckDB 1.5 and has immature Go support; JSON-as-text and typed MAPs work today. Parquet or DuckLake beat the native file for retention and S3. Four layouts shortlisted for the benchmark; the map's own scale target (10k/s vs 50–100 GB/day) is inconsistent.
- [Which ingest protocols and shippers should logs support?](issues/02-ingest-protocols-and-shippers.md): Sentry SDKs already send logs by default and SolidTrace drops them; the envelope parser also loses events followed by another item. OTLP/HTTP protobuf is the universal shipper format (cheap in Go); Vector's easy path is a plain JSON `http` sink. Mirroring Sentry's OTLP path would make Sentry's published shipper recipes work unchanged.
- [What APL/KQL surface is worth supporting, and can DuckDB SQL mode be sandboxed?](issues/04-apl-kql-surface-and-sql-sandboxing.md): the common core (where, project/extend, summarize by bin, sort, take, search) maps to DuckDB. No mature Go KQL parser exists; the best prior art is C# and TS. DuckDB calls untrusted SQL "unsafe by design": settings are instance-wide, several escape routes were observed on 1.4.3, and only AST validation, context cancel and separate instances or processes help.
- [Which production-readiness gaps must be fixed before or alongside logs?](issues/05-prod-readiness-gaps-for-logs.md): only ingest write path 04–06 must land before logs code; log retention is designed on this map; logs-vs-Events isolation, observability and ingest limits/quotas are deferred past v1; the rest stays in prod-readiness.
- [How should the Logs explorer be laid out?](issues/06-logs-explorer-layout.md): a stream of terminal-style lines under a filter-chip bar (query text behind a toggle, no level toggles), a slim histogram with a one-line summary, every value in a line a filter tag, and an inspector that overlays the stream. Prototype on branch `prototype/logs-explorer-layout`.
- [What is a Log?](issues/07-log-domain-model.md): a Log belongs to a Project (no Datasets) and has seven built-in fields (time, level, message, service, environment, trace id, span id); everything else is an Attribute in one flat dotted namespace, each value keeping its own type. Six fixed levels, no received time, reserved built-in names (rejected values kept as `original.<field>`), no dedup in v1. ADR 0002.
- [Which ingest sources ship in v1, with what auth and limits?](issues/08-v1-ingest-sources-auth-limits.md): Sentry `log` envelope items plus a plain JSON/NDJSON `POST /api/{project_id}/logs`; no OTLP in v1. Existing project keys, with Bearer accepted on the JSON route; gzip/zstd, 10 MB compressed and 50 MB decompressed. 503 + `Retry-After` on overload; per-Log limits truncate and keep (`solidtrace.truncated`); ranked JSON key aliases fill the built-ins.
- [What query language(s) do users write?](issues/11-query-language.md): one KQL-style, unbranded query language (core where/project/extend/summarize/sort/take/top/distinct/search, `contains` not `has`), compiled by a hand-written Go compiler in EventStore that every explorer widget also goes through; Project and time range come from the bar; `!=` keeps Logs missing the Field, `==` never coerces. Raw DuckDB SQL ships unvalidated behind an off-by-default instance setting, against a stable `logs` view (ADR 0003).

## Not yet specified

**v1**
- Query execution boundary: how log reads are authorized (Console proxy vs direct), per-query timeouts and result caps. Precedent since 2026-09-26: event queries go Console → the event store's loopback-only query listener, unauthenticated; log reads may follow it, SQL queries are unsandboxed and share EventStore's DuckDB with Event processing (ADR 0003), so timeouts and result caps matter for both kinds of query.
- Explorer building blocks: the chosen layout needs a virtualised log stream, a small stacked histogram and an overlay drawer. Library choices, and whether the histogram stays hand-rolled, are open.
- Operations: a load-test scenario for log ingest (the overload signal is decided: 503 + `Retry-After` on the shared gate).
- Whether logs use the README's S3/GCS tiered storage, and how. DuckDB 2.0's asynchronous I/O (published ~3.7× faster Parquet on S3) makes an object-storage cold tier far more realistic; the benchmark measures it.

**v2**
- OTLP/HTTP log ingest (protobuf; OTLP JSON waits on Go 1.26), so OTel Collector users have a path. Mirror Sentry's `/api/{project_id}/integration/otlp/v1/logs` and serve `/v1/logs`.
- Axiom-compatible ingest (Axiom paths, dataset→Project mapping).
- Logs never starve Events: isolating log ingest and disk use so a log flood cannot fail Event ingest.
- Observability: a metrics endpoint, health reporting, disk alarms.
- Live tail.
- Duplicate detection for Logs stored twice when a shipper retries a batch.
- Saved and shared queries.
- Dashboards.
- Monitors, reusing the existing Integrations and notification pipeline.
- Log ↔ Issue/Event correlation (for example, a trace's logs on an Issue page).
- Traces and metrics: data model and whether they share the logs store.
- Writing the v2 spec.

## Out of scope

- Horizontal scaling and high availability.
- A hosted multi-tenant SaaS.
- Per-user permissions finer than Organization membership.
- AI or natural-language query generation.
- Continuous profiling and real-user monitoring.
- Loki push and Elasticsearch-bulk ingest compatibility (decided in [Which ingest sources ship in v1, with what auth and limits?](issues/08-v1-ingest-sources-auth-limits.md)).
