# SolidTrace Logs

Label: wayfinder:map

## Destination

A spec for SolidTrace Logs v1 (plus a separate v2 spec for the full vision) and an ordered set of tickets for v1's first milestone. Every product and architecture decision in the specs traces to a resolved ticket on this map; decisions that are hard to reverse are recorded as ADRs. The destination is documents, not working code: benchmarks and spikes appear only as tasks or prototypes that a decision waits on.

## Notes

- **Domain:** self-hosted log ingest, storage, query and exploration for SolidTrace, alongside the existing error tracking. Tickets are worked in `.scratch/logs/issues/` per `docs/agents/issue-tracker.md` ("Wayfinding operations").
- **Scope:** v1 = ingest, storage and retention, query language, Logs explorer, query builder, raw DuckDB SQL mode (unrestricted, opt-in per instance; ADR 0003). v2 = live tail, saved and shared queries, dashboards, monitors, log ↔ Issue/Event correlation, traces and metrics.
- **Fixed principles:** Rails (Console) for admin logic, Go (EventStore) for performance paths, shadcn for UI; zero-config ingest (no required indexing or schema, every JSON field queryable at once, as in Axiom); logs live inside the two existing services, no third service. DuckDB is the engine, and logs stay on local disk in v1 (decided in [How are logs stored and retained?](issues/10-storage-architecture-and-retention.md)).
- **Target scale (decided 2026-09-19, replaces the self-contradictory 10k logs/s figure):** one VM ingesting 3–5 TB of uncompressed log data per month. That averages 100–170 GB/day, or 1.2–1.9 MB/s; at a typical 300 B–1 KB per log it is roughly 1–6k logs/s on average. Bursts of 5× the average for 10 minutes are absorbed without a 503. The target VM is 4 vCPU and 16 GB (both decided 2026-10-02 in [How are logs stored and retained?](issues/10-storage-architecture-and-retention.md)).
- **Engine line (re-decided 2026-10-02):** the released DuckDB 1.5 line, not the 2.0 alpha. EventStore runs 1.5.6 (duckdb-go `v2.10506.0`, the newest 1.5.x tag when it moved). The storage design needs only 1.5-level features, so 2.0 is a later upgrade, not a dependency ([How are logs stored and retained?](issues/10-storage-architecture-and-retention.md); the move is [Move EventStore back to DuckDB 1.5.5](../upgrade-pebble-duckdb/issues/02-duckdb-2-0-0-release.md)). Earlier, from 2026-09-19, the target was DuckDB 2.0, and EventStore linked the alpha `v2.0.0-alpha43385` as a shared library because duckdb-go had no 2.0 build; the benchmark in ticket 09 ran on that alpha.
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
- [Benchmark the shortlisted storage layouts at reduced scale on the dev box](issues/09-benchmark-storage-layouts.md): typed MAPs rank first (up to 5× faster Attribute queries than JSON text, 139 vs 205 B/Log, 20k Logs/s unthrottled), then JSON text, then VARIANT without shredding. Shredded VARIANT is unusable on the 2.0 alpha: slow checkpoints and queries, a heap-corruption crash, and a database-invalidating filter error on default-shredded Parquet. Local Parquet gains nothing from async read-ahead and prefers 30k-row row groups; a day-long Attribute search over Parquet projects to 2–4 min on 2 cores. Findings: [research/09](research/09-storage-bench-reduced.md), on branch `feature/logging`.
- [How do the Builder, APL and SQL modes relate?](issues/12-query-builder-ux.md): a `Builder | Query | SQL` switch (SQL only when the instance setting is on). The Builder is filter chips + search only; Builder → Query always works, Query → Builder only when the Builder can show the Query, else it offers to discard. SQL mode starts from the compiled SQL of the current Query and is one-way. Query mode replaces ticket 06's read-only query toggle. Prototype on branch `prototype/logs-query-modes`.
- [How are logs stored and retained?](issues/10-storage-architecture-and-retention.md): DuckDB 1.5.6. Pebble first, then one hot table per Project in `logs.duckdb` (current and previous hour), then hourly zstd Parquet on local disk, one file per Project per hour. Attributes in four typed MAPs; up to 100 (type, key) pairs per Project are promoted to key-named columns at ingest, re-ranked daily by how many Logs carry them; the promoted-set table is the compiler's only source of columns. Out-of-window Logs go to a MAP-only `logs_late`. SQL mode's `logs` view is built per query for the bar's Project and time range. Retention in days deletes whole folders, plus an instance-wide disk limit; 5× bursts for 10 minutes; "latest N" within 2 s, common-key day searches within 5 s. ADR 0005 (supersedes 0004); resolved twice, the second time after ticket 14.
- [Benchmark the chosen storage design on DuckDB 1.5.5](issues/14-benchmark-storage-design-on-1-5-5.md): on 1.5.6, MAP still beats JSON; checkpoint stalls of 3–4 s remain (no crash); the hot table plateaus; an hour exports in minutes without slowing ingest. The 5 s day target is missed because the hot table has no promoted columns; promoting there fixes most of it. Decided: promote at ingest, 100 keys per Project, re-ranked nightly; promoted set in `logs.duckdb`; plain files, no DuckLake. Findings: [research/14](research/14-storage-design-bench.md).
- [How do log queries run?](issues/15-how-log-queries-run.md): no per-query timeouts in v1 (the user cancels); routing, authorization, server-side cancel and result caps are left to the implementer.
- [How does the explorer show non-Log results and keep its state?](issues/16-explorer-building-blocks.md): non-Log results are a table only (charts wait for dashboards); the histogram always counts the filtered Logs. The time range, mode, Query and SQL text live in the URL, and SQL never runs on load. The stream loads 200 at a time up to about 10,000; the summary line is count, per-level counts and error rate. Local time on screen, UTC in URLs. Libraries are the implementer's call, compatible with shadcn and the existing toolset.
- [Write the v1 spec and first-milestone tickets](issues/13-write-v1-spec-and-first-milestone-tickets.md): the [v1 spec](../logs-v1/spec.md) plus 19 tickets across three milestones in [logs-v1/issues](../logs-v1/issues/) (M1 Logs are stored, M2 Logs explorer with the Builder, M3 Query and SQL modes). Project deletion is detected by the `projects` row, not the keys.

## Not yet specified

**v1**
- Operations: a load-test scenario for log ingest and the chosen storage design on the target VM (4 vCPU, 16 GB) at 3–5 TB/month with 5× bursts; it replaces the parked full-scale benchmark and is a first-milestone ticket, not a prerequisite for the spec. It should measure the checkpoint after each hourly delete at full scale, which took 0.4–3 s at reduced scale (ticket 14). It should also cover per-Project hot tables and the daily promoted-set switch (ADR 0005), neither measured yet. The overload signal is decided: 503 + `Retry-After` on the shared gate.

**v2**
- S3/GCS cold tier for log Parquet files (local disk only in v1). Re-check DuckLake's catalog pruning then (ticket 14 kept plain files). DuckDB 2.0's asynchronous I/O (published ~3.7× faster Parquet on S3) makes it more realistic.
- DuckDB 2.0 upgrade, with a re-check of VARIANT for Attributes (`logsbench` and `TestDefaultVariantParquet`).
- A search index so day-long searches on rare Attribute keys finish in seconds (v1 streams them newest first).
- OTLP/HTTP log ingest (protobuf; OTLP JSON waits on Go 1.26), so OTel Collector users have a path. Mirror Sentry's `/api/{project_id}/integration/otlp/v1/logs` and serve `/v1/logs`.
- Axiom-compatible ingest (Axiom paths, dataset→Project mapping).
- Logs never starve Events: isolating log ingest and disk use so a log flood cannot fail Event ingest.
- Observability: a metrics endpoint, health reporting, disk alarms.
- Live tail.
- Duplicate detection for Logs stored twice when a shipper retries a batch.
- Saved and shared queries.
- Dashboards, including charts for `summarize … by bin(time, …)` (v1 shows a table only; [ticket 16](issues/16-explorer-building-blocks.md)).
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
