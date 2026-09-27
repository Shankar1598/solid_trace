# SolidTrace Logs

Label: wayfinder:map

## Destination

A spec for the SolidTrace Logs **MVP** and an ordered set of tickets to build it (scoped down from v1 + v2 on 2026-09-27). The MVP does one job: find the logs around a time in a Project, search them, and open one log. It covers JSON-over-HTTP ingest, the benchmarked storage layout, one global retention window, SQL mode plus a filter bar, and a one-page Logs explorer. Every product and architecture decision in the spec traces to a resolved ticket on this map; decisions that are hard to reverse are recorded as ADRs. The destination is documents, not working code: benchmarks and spikes appear only as tasks or prototypes that a decision waits on.

## Notes

- **Domain:** self-hosted log ingest, storage, query and exploration for SolidTrace, alongside the existing error tracking. Tickets are worked in `.scratch/logs/issues/` per `docs/agents/issue-tracker.md` ("Wayfinding operations").
- **Scope (decided 2026-09-27):** the MVP in the Destination. Everything planned beyond it is on the **Future** list below; it never graduates on this map, and returns as a fresh effort once the MVP is built.
- **Fixed principles:** Rails (Console) for admin logic, Go (EventStore) for performance paths, shadcn for UI; zero-config ingest (no required indexing or schema, every JSON field queryable at once, as in Axiom); logs live inside the two existing services, no third service. The DuckDB 2.0 engine line is decided (below); S3/GCS tiering is on the Future list.
- **Target scale (decided 2026-09-19, replaces the self-contradictory 10k logs/s figure):** one VM ingesting 3–5 TB of uncompressed log data per month. That averages 100–170 GB/day, or 1.2–1.9 MB/s; at a typical 300 B–1 KB per log it is roughly 1–6k logs/s on average. Burst headroom above the average is still to be decided.
- **Engine line (decided 2026-09-19):** target DuckDB 2.0, not 1.4 LTS or 1.5. As of 2026-09-19 it is a preview ("coming this fall", [2.0 highlights](https://duckdb.org/2026/08/17/duckdb-20-highlights)), so benchmarks run on preview or RC builds, and duckdb-go support for 2.0 is a risk to check rather than assume. **Checked 2026-09-27** ([upgrade-pebble-duckdb 01](../upgrade-pebble-duckdb/issues/01-upgrade-pebble-and-duckdb.md)): duckdb-go has no build of the real 2.0 alpha; reaching it from Go means linking the alpha `libduckdb.so` with `-tags=duckdb_use_lib`. The `v2.20000.0-N.preview` tags are engine 1.5.4 plus 2.0 backports, not 2.0. EventStore now links the real alpha `v2.0.0-alpha43385` through `event_store/scripts/fetch-duckdb`, until `v2.20000.0` ships with 2.0.0 (due 2026-10-21). Quack (2.0 as a separate server) cannot be reached from any 1.5.x Go client, because the protocol changed.
- **Stance on Axiom:** Axiom-inspired, not Axiom-compatible. Match its ease of use and APL feel; how much compatibility would cost is a research fact, not a goal.
- **SQL mode (decided 2026-09-27):** ships in the MVP, read-only, for one self-hosted Organization. Accepted MVP risks: a large query may take down EventStore (Event ingest included), and a query can read any file EventStore can see. Resource limits and host-file confinement are deferred until the feature is built. Detail and the deferred items: [What query language(s) do users write?](issues/11-query-language.md).
- **Prior material is reference only.** The 2026-09-13 design was made without this map; none of its decisions stand until a ticket here makes them. Old spec, architecture and tickets: `.scratch/logging-old/`. Its research and codebase surveys (not re-verified): `.scratch/logging-old/research/`. Partial code, ADRs 0001–0005 and old glossary terms: branch `backup/logging-v0`.
- **Production readiness:** gaps in `.scratch/prod-readiness/review.md` are open for discussion; when the logs work depends on one, discuss it and bring it into scope rather than treating it as someone else's work.
- **Skills:** grilling tickets call `grilling` and `domain-modeling` (glossary terms go into `CONTEXT.md` as they resolve; hard-to-reverse choices become ADRs in `docs/adr/`). Prototype tickets call `prototype`. Research tickets run as subagents calling `research`, with findings on a `research/<name>` branch and a pointer from the ticket.

## Decisions so far

<!-- one line per resolved ticket: [title](issues/NN-slug.md): gist -->

- [How do Axiom and Better Stack work for logs?](issues/01-axiom-and-better-stack-teardown.md): Axiom auto-columnises every field (no user indexes); Better Stack needs extraction rules for speed. Axiom ingest compatibility is small and bounded; pasteable APL is open-ended. `runreveal/pql` (Go, Apache-2.0) is the closest existing KQL-style→SQL compiler.
- [How can DuckDB store schemaless logs at 10k logs/s?](issues/03-duckdb-schemaless-storage-options.md): no hard blocker. VARIANT needs DuckDB 1.5 and has immature Go support; JSON-as-text and typed MAPs work today. Parquet or DuckLake beat the native file for retention and S3. Four layouts shortlisted for the benchmark; the map's own scale target (10k/s vs 50–100 GB/day) is inconsistent.
- [Which ingest protocols and shippers should logs support?](issues/02-ingest-protocols-and-shippers.md): Sentry SDKs already send logs by default and SolidTrace drops them; the envelope parser also loses events followed by another item. OTLP/HTTP protobuf is the universal shipper format (cheap in Go); Vector's easy path is a plain JSON `http` sink. Mirroring Sentry's OTLP path would make Sentry's published shipper recipes work unchanged.
- [What APL/KQL surface is worth supporting, and can DuckDB SQL mode be sandboxed?](issues/04-apl-kql-surface-and-sql-sandboxing.md): the common core (where, project/extend, summarize by bin, sort, take, search) maps to DuckDB. No mature Go KQL parser exists; the best prior art is C# and TS. DuckDB calls untrusted SQL "unsafe by design": settings are instance-wide, several escape routes were observed on 1.4.3, and only AST validation, context cancel and separate instances or processes help.
- [How should the Logs explorer be laid out?](issues/06-logs-explorer-layout.md): one page: filter bar (Project, time range, free-text search), results table newest first, click a row for its full JSON, and a SQL tab into the same table. No histogram or field sidebar.

## Not yet specified

- Query execution boundary: how the explorer's log reads reach EventStore. Precedent since 2026-09-26: Event queries go Console → EventStore's loopback-only query listener, unauthenticated; log reads may follow it. SQL mode runs read-only on its own DuckDB instance inside EventStore, with no resource limits in the MVP (see [How do the filter bar and SQL mode query logs?](issues/11-query-language.md)).

## Future

<!-- planned after the MVP; returns as a fresh effort, never graduates on this map -->

- Query: APL, and the query builder with its mode round-trips ([How do the Builder, APL and SQL modes relate?](issues/12-query-builder-ux.md), closed).
- Ingest: Sentry SDK logs (SDKs already send them; envelope log items are dropped today), OTLP/HTTP, Vector- and Axiom-compatible endpoints.
- Storage: per-Project retention and disk budgets; S3/GCS tiered storage (DuckDB 2.0's async I/O makes an object-storage cold tier more realistic).
- Durable acknowledgements for logs beyond what the MVP layout gives.
- Operations: metrics endpoint, health reporting, disk alarms.
- SQL-mode hardening: resource limits and host-file confinement (the deferred items on [How do the filter bar and SQL mode query logs?](issues/11-query-language.md)).
- Explorer: histogram, field sidebar, value-to-filter pivot.
- Live tail; saved and shared queries; dashboards; monitors (reusing the Integrations and notification pipeline).
- Log ↔ Issue/Event correlation (for example, a trace's logs on an Issue page).
- Traces and metrics: data model and whether they share the logs store.
- A spec for everything after the MVP.

## Out of scope

- Horizontal scaling and high availability.
- A hosted multi-tenant SaaS.
- Per-user permissions finer than Organization membership.
- AI or natural-language query generation.
- Continuous profiling and real-user monitoring.
