# SolidTrace Logs

Label: wayfinder:map

## Destination

A spec for SolidTrace Logs v1 (plus a separate v2 spec for the full vision) and an ordered set of tickets for v1's first milestone. Every product and architecture decision in the specs traces to a resolved ticket on this map; decisions that are hard to reverse are recorded as ADRs. The destination is documents, not working code: benchmarks and spikes appear only as tasks or prototypes that a decision waits on.

## Notes

- **Domain:** self-hosted log ingest, storage, query and exploration for SolidTrace, alongside the existing error tracking. Tickets are worked in `.scratch/logs/issues/` per `docs/agents/issue-tracker.md` ("Wayfinding operations").
- **Scope:** v1 = ingest, storage and retention, query language, Logs explorer, query builder, raw DuckDB SQL mode. v2 = live tail, saved and shared queries, dashboards, monitors, log ↔ Issue/Event correlation, traces and metrics.
- **Fixed principles:** Rails (Console) for admin logic, Go (EventStore) for performance paths, shadcn for UI; zero-config ingest (no required indexing or schema, every JSON field queryable at once, as in Axiom); logs live inside the two existing services, no third service. DuckDB as the engine and S3/GCS tiering for logs are **open**, to be decided on this map.
- **Target scale (decided 2026-09-19, replaces the self-contradictory 10k logs/s figure):** one VM ingesting 3–5 TB of uncompressed log data per month. That averages 100–170 GB/day, or 1.2–1.9 MB/s; at a typical 300 B–1 KB per log it is roughly 1–6k logs/s on average. Burst headroom above the average is still to be decided.
- **Engine line (decided 2026-09-19):** target DuckDB 2.0, not 1.4 LTS or 1.5. As of 2026-09-19 it is a preview ("coming this fall", [2.0 highlights](https://duckdb.org/2026/08/17/duckdb-20-highlights)), so benchmarks run on preview or RC builds, and duckdb-go support for 2.0 is a risk to check rather than assume.
- **Stance on Axiom:** Axiom-inspired, not Axiom-compatible. Match its ease of use and APL feel; how much compatibility would cost is a research fact, not a goal.
- **Prior material is reference only.** The 2026-09-13 design was made without this map; none of its decisions stand until a ticket here makes them. Old spec, architecture and tickets: `.scratch/logging-old/`. Its research and codebase surveys (not re-verified): `.scratch/logging-old/research/`. Partial code, ADRs 0001–0005 and old glossary terms: branch `backup/logging-v0`.
- **Production readiness:** gaps in `.scratch/prod-readiness/review.md` are open for discussion; when the logs work depends on one, discuss it and bring it into scope rather than treating it as someone else's work.
- **Skills:** grilling tickets call `grilling` and `domain-modeling` (glossary terms go into `CONTEXT.md` as they resolve; hard-to-reverse choices become ADRs in `docs/adr/`). Prototype tickets call `prototype`. Research tickets run as subagents calling `research`, with findings on a `research/<name>` branch and a pointer from the ticket.

## Decisions so far

<!-- one line per resolved ticket: [title](issues/NN-slug.md): gist -->

- [How do Axiom and Better Stack work for logs?](issues/01-axiom-and-better-stack-teardown.md): Axiom auto-columnises every field (no user indexes); Better Stack needs extraction rules for speed. Axiom ingest compatibility is small and bounded; pasteable APL is open-ended. `runreveal/pql` (Go, Apache-2.0) is the closest existing KQL-style→SQL compiler.
- [How can DuckDB store schemaless logs at 10k logs/s?](issues/03-duckdb-schemaless-storage-options.md): no hard blocker. VARIANT needs DuckDB 1.5 and has immature Go support; JSON-as-text and typed MAPs work today. Parquet or DuckLake beat the native file for retention and S3. Four layouts shortlisted for the benchmark; the map's own scale target (10k/s vs 50–100 GB/day) is inconsistent.
- [Which ingest protocols and shippers should logs support?](issues/02-ingest-protocols-and-shippers.md): Sentry SDKs already send logs by default and SolidTrace drops them; the envelope parser also loses events followed by another item. OTLP/HTTP protobuf is the universal shipper format (cheap in Go); Vector's easy path is a plain JSON `http` sink. Mirroring Sentry's OTLP path would make Sentry's published shipper recipes work unchanged.
- [What APL/KQL surface is worth supporting, and can DuckDB SQL mode be sandboxed?](issues/04-apl-kql-surface-and-sql-sandboxing.md): the common core (where, project/extend, summarize by bin, sort, take, search) maps to DuckDB. No mature Go KQL parser exists; the best prior art is C# and TS. DuckDB calls untrusted SQL "unsafe by design": settings are instance-wide, several escape routes were observed on 1.4.3, and only AST validation, context cancel and separate instances or processes help.

## Not yet specified

**v1**
- Query execution boundary: how log reads are authorized (Console proxy vs direct), per-query timeouts and result caps. Precedent since 2026-09-26: event queries go Console → the event store's loopback-only query listener, unauthenticated; log reads may follow it, but SQL mode's sandboxing needs may not fit a shared process.
- Explorer building blocks: editor, chart and virtualised-table library choices.
- Operations: ingest backpressure, health reporting, disk alarms, a load-test scenario for log ingest.
- Whether logs use the README's S3/GCS tiered storage, and how. DuckDB 2.0's asynchronous I/O (published ~3.7× faster Parquet on S3) makes an object-storage cold tier far more realistic; the benchmark measures it.
- Making the existing Sentry envelope parser read every item (today it drops log items and loses events followed by any other item), and how that ties into log ingest.

**v2**
- Live tail.
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
