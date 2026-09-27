# SolidTrace Logs: architecture and delivery plan

## Context

SolidTrace is a self-hosted error tracker built on two services (Rails Console, Go EventStore) and embedded databases (SQLite, Pebble, DuckDB + Parquet). The goal is to add **logging and log-based monitoring** with Axiom's ease of use (schemaless ingest, zero configuration, a KQL-style query language, query builder, dashboards, monitors) while keeping the stack: Rails for admin logic, Go for the hot paths, DuckDB instead of ClickHouse, shadcn for UI.

Research was done against primary sources (Axiom docs, Better Stack docs, Microsoft KQL reference and grammar, DuckDB 1.4/1.5 docs and duckdb-go source, Sentry/OTLP/Vector/Fluent Bit specs, and a survey of Parseable/OpenObserve/Quickwit/SigNoz/Loki/VictoriaLogs/HyperDX). The decisions below are grounded in that research. This document is the technical source of truth for the feature; `spec.md` states the product behaviour and `issues/` holds the tracer-bullet tickets derived from the milestones below. Decisions 1-5 are recorded as ADRs 0001-0005 in `docs/adr/`.

**Out of scope (owned by another agent):** every item in `.scratch/prod-readiness/review.md`. They are listed as dependencies at the end, never as work here.

---

## Key decisions (the "why", one line each)

| # | Decision | Why |
|---|----------|-----|
| 1 | **Logs live in the same DuckDB database as events, in their own tables** (`logs_hot`, `log_files`, `log_fields`); they **bypass Pebble** | Logs are small and fully represented in columns; Pebble exists only to serve 8KB raw event blobs by UUID. One DuckDB instance = one memory budget, one writer process (DuckDB rule). |
| 2 | **Schemaless attributes stored as three typed maps** `attrs_str / attrs_num / attrs_bool` (MAP columns) with nested objects flattened to dotted keys; a per-project **field catalog** records key → type | The pattern every ClickHouse-based tool converged on (SigNoz, HyperDX). No JSON parse per row, numeric comparisons without casts, zero user configuration. VARIANT is 1.5-only; JSON is stored as text in 1.4.3. |
| 3 | **Hourly Parquet files per project + a manifest table**, queries built as `read_parquet([explicit file list])`, never a glob view | Tailpipe hit 1,463 file opens per query with globs; `union_by_name` reads every footer. Manifest gives time-range pruning before DuckDB opens anything. |
| 4 | **KQL-Lite parser + DuckDB SQL generator written in Go** (`event_store/kql/`) | No usable Go KQL parser exists (only AGPL/extraction-only or Kibana-KQL libraries). Transpiling in Go keeps Rails thin and lets monitors, dashboards and the UI share one engine. |
| 5 | **Rails proxies all log reads**; the browser never calls Go | Keeps the single auth door; the Go read API auth is being added by the other agent (issue 002) and the logs endpoints simply join that group. |
| 6 | **Ingest via three adapters onto one canonical record**: Sentry envelope `log` items (existing SDKs, zero user config), generic `POST /api/:project_id/logs` (Vector, Fluent Bit, curl), OTLP/HTTP `/v1/logs` (milestone 5) | Sentry SDKs (Ruby 7, JS 10.71+, Python 2.68+, Go 0.47+) already send structured logs by default; today the envelope parser silently drops them. |
| 7 | **Retention per project in days, enforced by Go** by deleting whole Parquet files | Files are the unit of deletion; no row-level deletes on Parquet. |
| 8 | **Editor = CodeMirror 6 with a small KQL tokenizer; diagnostics come from the Go parser**; charts = recharts via shadcn `chart`; tables = TanStack Virtual | monaco-kusto validates full KQL (would accept what we reject) and costs a 32MB language service + Monaco. One parser = one truth. |
| 9 | **Raw DuckDB SQL mode is milestone 6, behind AST validation**, not v1 | DuckDB security settings are instance-global (would disable the archiver's `COPY TO`), so SQL needs `json_serialize_sql` validation + a wrapping project-scoped CTE + timeouts. KQL covers the product need first. |
| 10 | **Stay on the pinned DuckDB 1.4.3** for this feature; design is 1.5-compatible | Nothing here needs VARIANT. Upgrading the engine is a separate, whole-service decision. |

---

## Architecture

```
Sentry SDK ──envelope(log items)──┐
Vector/Fluent Bit/curl ──JSON/NDJSON─┤   Go EventStore (Fiber)
OTel exporter ──OTLP/HTTP (M5)──────┘        │
        auth: project key (existing ValidateKey)
                                              ▼
                 adapters → canonical LogRecord → logsChan (bounded, 429 on full)
                                              ▼
                 LogsIngester: batch → DuckDB Appender → logs_hot  (1s flush)
                                              │            + log_fields catalog upsert
                 hourly: closed hour → Parquet file (per project) → log_files manifest → delete from logs_hot
                 nightly: compact small hourly files per (project, day); retention sweep deletes files > N days
                                              ▼
   Rails Console ──POST /api/:pid/logs/query {kql,start,end}──▶ kql parser → SQL over logs_hot ∪ read_parquet([files])
        ▲   fields / facets / recent(tail) / validate endpoints
   React explorer (Builder | KQL editor, histogram, table, detail sheet, live tail)
   Dashboards (elements = kql + viz), Monitors (Solid Queue job → Go query → Integration notifiers)
```

---

## Data model

### DuckDB (Go-owned; add to `event_store/storage/`, new file `logs_duckdb.go`, opened from the existing `sql.DB` in `storage/duckdb.go`)

```sql
CREATE TABLE IF NOT EXISTS logs_hot (
  id              UUID,              -- UUIDv7 assigned at ingest (ingest order; live-tail cursor)
  project_id      INTEGER,
  ts              TIMESTAMP,         -- event time  (KQL: _time)
  observed_ts     TIMESTAMP,         -- ingest time (KQL: _sysTime)
  level           VARCHAR,           -- trace|debug|info|warn|error|fatal (normalised)
  severity_number SMALLINT,          -- OTLP 1..24 (trace=1 debug=5 info=9 warn=13 error=17 fatal=21)
  service         VARCHAR,           -- service.name / sentry.sdk.name fallback / Vector host / ''
  environment     VARCHAR,
  host            VARCHAR,
  source          VARCHAR,           -- sentry|http|otlp (adapter that produced the row)
  trace_id        VARCHAR,           -- 32 hex or NULL
  span_id         VARCHAR,           -- 16 hex or NULL
  message         VARCHAR,           -- body
  attrs_str       MAP(VARCHAR, VARCHAR),
  attrs_num       MAP(VARCHAR, DOUBLE),
  attrs_bool      MAP(VARCHAR, BOOLEAN)
);
CREATE TABLE IF NOT EXISTS log_files (      -- Parquet manifest
  path VARCHAR PRIMARY KEY, project_id INTEGER, day DATE, hour SMALLINT,
  min_ts TIMESTAMP, max_ts TIMESTAMP, row_count BIGINT, bytes BIGINT, created_at TIMESTAMP
);
CREATE TABLE IF NOT EXISTS log_fields (     -- per-project field catalog for the UI/transpiler
  project_id INTEGER, key VARCHAR, kind VARCHAR,   -- kind: str|num|bool|json
  first_seen TIMESTAMP, last_seen TIMESTAMP, seen_count BIGINT, PRIMARY KEY (project_id, key, kind)
);
```

Rules:
- Nested objects flatten to dotted keys (`http.request.method`); arrays and objects deeper than 10 levels are stored JSON-serialised in `attrs_str` with catalog kind `json`.
- Limits (Axiom's): 10,000 records/request, 1 MiB/record, key ≤ 200 bytes, ≤ 1,024 distinct keys per project (beyond that new keys go to `attrs_str` under a single `_overflow` key as JSON).
- Appender gotchas (verified in duckdb-go 2.5.4 source): `ts` needs `time.Time`, `id` needs `duckdb.UUID` (16 bytes, not a string), maps need `duckdb.Map`.
- Parquet layout: `<logs_parquet_path>/project_id=<id>/day=YYYY-MM-DD/hour=HH-<uuidv7>.parquet`, written with `COPY (SELECT … ORDER BY ts) TO … (FORMAT parquet, ROW_GROUP_SIZE 122880, COMPRESSION zstd)`; fixed column list (no `union_by_name`); `SET parquet_metadata_cache = true`.
- Archive transaction: write temp file → rename → in ONE DuckDB transaction insert `log_files` row + `DELETE FROM logs_hot` for that (project, hour). On startup, files on disk with no manifest row are deleted (their rows are still in `logs_hot`).
- Compaction (nightly, Go ticker): per (project, day) merge hourly files into one when total < 512 MB; otherwise leave. Retention sweep (hourly): delete files whose `max_ts` < now − `projects.logs_retention_days`; Go reads that column through the existing read-only SQLite connection pattern in `event_store/auth/project.go`.

### Rails SQLite (`console/db/migrate/*`)

- `projects`: add `logs_retention_days INTEGER DEFAULT 30 NOT NULL`.
- `log_saved_queries`: organization_id, organization_user_id, name, kql TEXT, time_range (e.g. `15m`), created/updated. Unique (organization_id, name).
- `dashboards`: organization_id, name, slug, time_range, refresh_seconds, position; `dashboard_elements`: dashboard_id, kind (`timeseries|stat|table|logstream|note`), title, project_id, kql TEXT, viz JSON (chart variant, unit, y-scale), layout JSON `{x,y,w,h}` on a 12-col grid, position.
- `log_monitors`: organization_id, project_id, name, kind (`threshold|match`), kql TEXT, comparator (`above|above_or_equal|below|below_or_equal`), threshold DECIMAL, frequency_minutes, range_minutes, alert_on_no_data BOOL, integration_id, enabled BOOL, state (`ok|alerting|no_data`), state_changed_at, last_evaluated_at, last_error TEXT. `log_monitor_events`: log_monitor_id, kind (`triggered|resolved|no_data`), value DECIMAL, group_key, created_at.

---

## Ingest (Go)

### Canonical record and adapters (`event_store/logs/` new package)

`logs.Record{ProjectID, ID(uuid7), TS, ObservedTS, Level, SeverityNumber, Service, Environment, Host, Source, TraceID, SpanID, Message, AttrsStr, AttrsNum, AttrsBool}` plus a shared `normalizeLevel()` (accepts warning/err/critical/panic/emerg…) and `flatten()`.

1. **Sentry envelope log items** — extend `ingest.Service.IngestEnvelope` (`event_store/ingest/service.go`) to iterate all items honouring the item `length` header (fallback: next newline), dispatch `event|transaction` to the existing path and `log` (content type `application/vnd.sentry.items.log+json`, payload `{"items":[…]}`) to `logs.FromSentryItems`. Attribute values are `{value,type}` with type `string|integer|double|boolean|array`. Well-known keys map to columns: `sentry.environment→environment`, `server.address→host`, `sentry.sdk.name→service` (fallback), the rest to maps. *Coordinate with the other agent: prod-readiness §6 also rewrites this parser; whoever lands first, the item iterator must be shared.*
2. **Generic HTTP** — `POST /api/:project_id/logs` in new `handler/logs.go`. Auth: reuse `extractSentryKey` (move it to `auth/` as `KeyFromRequest`) and accept `Authorization: Bearer <public_key>` too. Body sniffed by first non-whitespace byte: `[` → JSON array, `{` → NDJSON/single object (streaming `json.Decoder` loop). `Content-Encoding: gzip/deflate/br` are handled by Fiber; **zstd is not** (verified in fasthttp 1.51) → decode with the already-present `klauspost/compress/zstd`. Timestamp detection order `_time > timestamp > time > ts > dt > @timestamp > date`; strings RFC3339/ISO8601, numbers by magnitude (s < 1e11, ms < 1e14, µs < 1e17, else ns; floats = seconds). Message keys `message > msg > body > log > event`; level keys `level > severity > severity_text > log.level`. Response `200 {"ingested":n,"failed":m,"failures":[{"index":i,"error":"…"}]}`; `401` bad key, `413` over limits, `429 + Retry-After: 2` when `logsChan` is full (Vector/Fluent Bit retry on 429).
3. **OTLP/HTTP** (milestone 5) — `POST /api/:project_id/otlp/v1/logs` and `POST /v1/logs` (key via `Authorization: Bearer`), `application/x-protobuf` via `go.opentelemetry.io/proto/otlp` (grpc/protobuf already indirect deps; `collector/pdata` needs Go 1.26 and is out), `application/json` via a hand-written decoder for the logs subset (protojson does not hex-decode `traceId`). Resource attrs → `resource.*` keys with `service.name/deployment.environment/host.name` promoted to columns; response `ExportLogsServiceResponse` with `partialSuccess`.

### Pipeline (`event_store/pipeline/logs_ingester.go`, modelled on `duckdb_ingester.go`)

`logsChan` (size `logs_channel_size`, default 200k) → batch up to 10k or 1 s → Appender into `logs_hot` → catalog upsert (`log_fields`) from the batch's key/kind set (in-memory dedupe, flush every 10 s). On write error: retry 3× with backoff, then log and drop (documented). Hourly `LogsArchiver` and `LogsRetention` goroutines follow the `Run()/Stop()` pattern of `pipeline/archive_consumer.go`. Shutdown: close `logsChan` and drain before `app.Shutdown()` returns (independent of the events-channel fix owned elsewhere).

### Config (`event_store/config/config.go`, three edits per key as the file requires) and deploy

`logs_parquet_storage_path`, `logs_channel_size`, `logs_flush_timeout`, `logs_max_body_bytes` (16 MiB), `logs_query_timeout` (30s), `logs_query_max_rows` (10k), `logs_hot_hours` (2). Fiber `BodyLimit` raised to 16 MiB (app-wide setting). `deploy/angie/angie.conf` `:4000` block gains `client_max_body_size 16m;` (required for batched shippers; Angie's default 1 MB would 413 them). Document in `deploy/event_store.yml`.

---

## Query API (Go, `handler/logs_query.go`; all routes registered in `main.go` inside the read-API auth group once issue 002 lands)

| Route | Body / params | Returns |
|---|---|---|
| `POST /api/:pid/logs/query` | `{kql, start, end, limit?, offset?}` | `{columns:[{name,type}], rows:[[…]], stats:{elapsed_ms, rows_scanned, bin_width_seconds?}, viz?:{kind,…}}` (render hint parsed from `render`) |
| `POST /api/:pid/logs/validate` | `{kql}` | `{errors:[{offset,length,message}], columns:[…], sql}` (sql only when `debug=1`) |
| `GET /api/:pid/logs/fields?start&end` | | `[{key, kind, seen_count, last_seen}]` from `log_fields` |
| `GET /api/:pid/logs/facets?field&kql&start&end&limit=10` | | `[{value, count}]` via `approx_top_k` / `GROUP BY` over the filtered set |
| `GET /api/:pid/logs/recent?kql&after=<uuid>&limit=200` | | rows from `logs_hot` only with `id > after` (UUIDv7 cursor; live tail, polled every 2 s) |
| `GET /api/:pid/logs/records/:id` | | one full record (detail sheet) |

Execution: dedicated read connection pool (2–4 connections from the same `sql.DB`, separate from the appender connection), `context.WithTimeout(logs_query_timeout)` (duckdb-go interrupts on cancel), every query gets `project_id = ?` and `ts BETWEEN ? AND ?` injected on the source scan and a default `LIMIT` when no `take/count/summarize`. Source = `logs_hot` rows in range `UNION ALL read_parquet([files from log_files where project & time overlap])`; the Parquet leg is omitted when the list is empty.

---

## KQL-Lite (`event_store/kql/`: `lexer.go`, `parser.go` (Pratt), `ast.go`, `resolve.go`, `sqlgen.go`, golden tests)

**v1 surface:** `let` (scalar constants), one tabular statement, source `logs` (also the project slug); operators `where/filter`, `extend`, `project`, `project-away`, `project-keep`, `project-rename`, `summarize … by …` with `bin()` / `bin_auto()`, `count`, `take/limit`, `sort/order by [asc|desc] [nulls first|last]`, `top N by`, `distinct`, `search` (bare terms, `col:"term"`, `kind=case_sensitive`, wildcard→has/hasprefix/hassuffix/contains/regex), `parse … with …` (simple|regex|relaxed), `render <chart>` (last operator, returned as viz hint only). Scalars: `==, !=, =~, !~, <,<=,>,>=, +,-,*,/,%, and, or, not(), in, !in, in~, between`, string ops `has, !has, has_cs, has_any, hasprefix, hassuffix, contains, !contains, contains_cs, startswith[_cs], endswith[_cs], matches regex`; functions `ago, now, datetime(), bin, bin_at, startofday/week/month, format_datetime, datetime_diff, tostring/toint/tolong/todouble/tobool/todatetime, parse_json→(json kind only), strcat, strlen, substring, tolower, toupper, trim, split, extract, extract_all, replace_string, replace_regex, indexof, iff/iif, case, coalesce, isnull/isnotnull/isempty/isnotempty`; aggregates `count, countif, dcount, count_distinct, sum, sumif, avg, avgif, min, max, minif, maxif, percentile, percentiles, make_list, make_set, arg_max, arg_min, take_any, stdev, variance`. Timespan literals `5m 1.5h 2d 30s 100ms`. Bracket identifiers `['http.status']`. `//` comments.
**v2:** `mv-expand`, `union`, `join`, `make-series`, `serialize/row_number`, virtual fields.
**Rejected with diagnostics:** `evaluate`, `cluster()/database()`, `materialize`, `scan`, `top-nested`, `set`.

**Field resolution** (`resolve.go`, uses `log_fields`): fixed columns by canonical name (`_time→ts`, `_sysTime→observed_ts`, `level, severity_number, service, environment, host, source, trace_id, span_id, message`); any other identifier → catalog lookup → `attrs_str['k']` / `attrs_num['k']` / `attrs_bool['k']` (a key present with several kinds resolves by the operator's expected type, else `coalesce(attrs_str[k], CAST(attrs_num[k] AS VARCHAR))`); unknown → error "unknown field `k`" with suggestions.

**Semantics to honour (from the KQL reference):** case-insensitive `=~ contains has startswith endswith in~`; case-sensitive `== != in matches regex *_cs`; `has` is term-based → `regexp_matches(col, '(?i)(^|[^0-9A-Za-z])' || regexp_escape(t) || '([^0-9A-Za-z]|$)')` (RE2 `\b` would be wrong); `now()`/`ago()` evaluated once in Go and inlined; `bin(_time, 5m)` → `time_bucket(INTERVAL '5 minutes', ts)`; `bin_auto` → width chosen for ~100 buckets over `[start,end]` snapped to `1s 5s 10s 30s 1m 5m 15m 1h 6h 1d`, reported in `stats`; summarize default names `count_, sum_x, dcount_x, percentile_x_95, list_x, set_x`; `dcount→approx_count_distinct`, `percentile→approx_quantile(x, p/100)`, `arg_max(a,b)→arg_max(b,a)` (argument order reversed in DuckDB), `make_list→list(x) FILTER (WHERE x IS NOT NULL)`, `make_set→list(DISTINCT x)`, `extract→nullif(regexp_extract(...),'')`, `toint→TRY_CAST`, `tostring→coalesce(CAST(x AS VARCHAR),'')`, `isempty→(x IS NULL OR x='')`, sort default `DESC NULLS LAST`. Regexes validated with Go `regexp` (RE2) at transpile time. Every literal is a bound parameter; every identifier double-quoted. Pipes compile to nested subqueries with a schema-tracking pass (needed for `*`, wildcards, default names).

---

## Console (Rails + React)

**Routes** (inside `scope "/:org_slug"` in `console/config/routes.rb`): `resources :logs, only: [:index]` plus `post "logs/query"`, `post "logs/validate"`, `get "logs/fields"`, `get "logs/facets"`, `get "logs/recent"`, `get "logs/records/:id"`, `get "logs/stream"`; `resources :log_saved_queries, only: [:index,:create,:update,:destroy]`; `resources :dashboards` with nested `elements`; under `scope "/settings"`: `resources :log_monitors`; `projects#update` accepts `logs_retention_days`.

**Controllers**: `logs_controller.rb` (Inertia `Logs/Index` and `Logs/Stream` for the shells; JSON actions for `query/validate/fields/facets/recent/record` called with `fetch` + CSRF, not Inertia visits, so the explorer re-queries without re-rendering the page), `dashboards_controller.rb`, `dashboard_elements_controller.rb`, `log_monitors_controller.rb`, `log_saved_queries_controller.rb`. Each sets `@current_org` exactly like `issues_controller.rb`; project selection is validated against `@current_org.projects` before any call to Go.

**Service**: `app/services/log_store.rb` mirroring `app/services/event_store.rb` (rest-client, `base_url`, one `make_request` that will carry the internal token once issue 002 lands) — but errors must surface to the UI as `{error: …}` rather than being swallowed to defaults.

**Serializers**: `log_saved_query_serializer.rb`, `dashboard_serializer.rb`, `dashboard_element_serializer.rb`, `log_monitor_serializer.rb` (hand-written `as_json` like the others). Never per-row calls to Go.

**Frontend** (`console/app/frontend/`): types in `types/index.ts` (`LogRecord`, `LogQueryResult`, `LogField`, `Dashboard`, `DashboardElement`, `LogMonitor`, `SavedQuery`); Sidebar entries `Logs`, `Dashboards` (`components/Sidebar.tsx`, fix `url.includes` matching); pages `Logs/Index.tsx`, `Logs/Stream.tsx`, `Dashboards/Index.tsx`, `Dashboards/Show.tsx`, `Settings/LogMonitors/{Index,New,Edit}.tsx`; components under `pages/Logs/components/`: `QueryBar` (Builder | KQL toggle, Run, time range), `KqlEditor` (CodeMirror 6: `@codemirror/state`, `@codemirror/view`, `@codemirror/language` StreamLanguage tokenizer, `@codemirror/autocomplete` fed by `/logs/fields`, `@codemirror/lint` fed by `/logs/validate`), `FilterBuilder` (field/operator/value rows + visualization + group-by; compiles to KQL text one-way), `TimeRangePicker` (popover, presets `5m 15m 1h 6h 24h 7d`, absolute range), `Histogram` (recharts bar chart with brush/drag-to-zoom, coloured by level), `LogTable` (TanStack Virtual, column chooser), `LogDetailSheet` (fields tab / JSON tab, "filter for/out value", "View in context", copy link), `FieldSidebar` (`/logs/fields` + `/logs/facets` on click), `SavedQueriesMenu`, `MoreMenu` (Add to dashboard, Create monitor, Copy link relative/absolute, Download CSV/JSON). URL is the state (`?project=&q=&range=`), as in `pages/Issues/Index.tsx`.
shadcn components to add with the repo's registry style (`base-lyra`, Base UI, hugeicons): `chart`, `popover`, `sheet`, `scroll-area`, `command`, `toggle-group`, `switch`, `skeleton`, `resizable`, `calendar`. New deps: `recharts`, `@codemirror/*`, `@tanstack/react-virtual`.

**Dashboards**: `Dashboards/Show.tsx` renders elements on a 12-column CSS grid from `layout`; each element runs `POST logs/query` with the dashboard time range (element override allowed) and renders by `kind` (`timeseries` line/area/bar, `stat`, `table`, `logstream`, `note`); edit mode = dialog per element (title, project, KQL, kind, size), reorder via up/down + width presets in v1 (drag-and-drop later). "Add to dashboard" from the explorer pre-fills the element from the current query.

**Monitors** (Rails-evaluated, Solid Queue): `LogMonitorEvaluationJob` registered in `config/recurring.yml` every minute; selects monitors due by `frequency_minutes`, runs the KQL over `[now-range, now]` through `LogStore`, applies comparator (threshold; `count()` of matches for `match`), updates `state`, writes `log_monitor_events`, and on transition creates a `Notification` with `event_type: "log_monitor_triggered" | "log_monitor_resolved"` and payload `{log_monitor_id, value, group_key}` and enqueues `IntegrationNotificationProcessorJob` exactly as `IntegrationNotifier` does today. Generalise the notification path: `IntegrationNotifier.deliver_batch` resolves the subject by `event_type` (Issue vs LogMonitor) and `Notifiers::{Slack,Email,Pagerduty}` gain `when "log_monitor_*"` payload builders (subject passed positionally as today; rename the ivar to `subject`). Threshold monitors require the KQL to end in `summarize` (validated via `/logs/validate` columns); `alert_on_no_data` → `no_data` state. Per-integration rate limit stays shared (documented).

---

## Process artifacts (done with this document)

- `CONTEXT.md`: add **Log** ("A timestamped record with a message, a level and attributes, accepted by EventStore through Log ingest." _Avoid_: log line, log event), **Log ingest**, **Field** (a catalogued attribute key with an inferred kind), **Query** (a KQL-Lite pipeline executed by EventStore over a project's Logs), **Saved Query**, **Dashboard**, **Element**, **Monitor**; change Event's avoid-list to `_Avoid_: message` and record the Event/Log boundary under Flagged ambiguities.
- ADRs in `docs/adr/`: `0001-logs-in-eventstore-duckdb-bypassing-pebble.md`, `0002-typed-attribute-maps-with-field-catalog.md`, `0003-kql-lite-transpiled-to-duckdb-in-go.md`, `0004-parquet-manifest-instead-of-glob-view.md`, `0005-rails-proxies-all-log-reads.md`.
- `.scratch/logging/spec.md` (to-spec template) and `.scratch/logging/issues/NN-*.md` (to-tickets template, `Status: ready-for-agent`) from the milestone list below.

---

## Milestones and tracer-bullet tickets

**M1 — First log visible (ingest → store → explore)**
1. Process artifacts above (CONTEXT terms, ADRs, spec, tickets).
2. Schema spike: `logs_hot` DDL + Appender writer + manifest/catalog tables with unit tests (`t.TempDir()`, pattern of `storage/duckdb_test.go`); measure appender throughput for typed maps on the dev VM (go/no-go: ≥ 20k rows/s sustained with 10k batches).
3. Generic `POST /api/:project_id/logs` end to end: adapter, limits, gzip/zstd, 429 backpressure, `logs_ingester`, k6 scenario `load_testing/log_ingestion.js` (+ `bin/load-test --kind logs`), Angie body size.
4. Sentry envelope `log` items: multi-item envelope iterator + `logs.FromSentryItems`; verified with `sentry-ruby` 7 in dev (`Sentry.logger.info`).
5. Query API v0 (`where`-only KQL + time range + limit, `fields`, `records/:id`) and the Logs page: project selector, KQL input, time range, histogram, virtualised table, detail sheet. Demo: a Rails app's `Sentry.logger` lines appear and are filterable.

**M2 — Query language and explorer parity**
6. KQL-Lite full v1 surface with golden tests (KQL → SQL) and execution tests against DuckDB fixtures; `/logs/validate`; CodeMirror editor with completion + lint.
7. Builder mode (filters, visualization, group-by) compiling to KQL; result rendering by shape (table vs series chart); `render` hints; `bin_auto`.
8. Field sidebar with facets; click-to-filter from sidebar and detail sheet.
9. Live tail page (`/logs/recent` polling, pause/resume, level colouring).
10. Saved queries + query history (per user), copy link relative/absolute, download CSV/JSON.

**M3 — Storage lifecycle**
11. Hourly Parquet archive + manifest + startup reconciliation; query planner reads hot ∪ manifest files; `EXPLAIN` test that time filters prune.
12. Nightly compaction + per-project retention (project settings UI field `logs_retention_days`, Go sweep); disk usage in `/health`.

**M4 — Dashboards**
13. Dashboard + element models, CRUD, Show page with grid and per-element queries; "Add to dashboard" from the explorer.

**M5 — Monitors and OTLP**
14. Threshold monitors: model, settings UI, evaluation job, notification generalisation, Slack/email payloads; state transitions tested with WebMock-stubbed Go responses.
15. Match monitors (per-event notifications with 10/min, 500/day caps).
16. OTLP/HTTP logs (protobuf + JSON) with a Collector `otlphttp` exporter example in docs.

**M6 — Advanced**
17. Raw DuckDB SQL mode behind `json_serialize_sql` validation (single SELECT, relation/function allow-list, project-scoped CTE, timeout, LIMIT).
18. Promoted columns for hot keys at archive time (Parquet gets real columns + bloom filters) and `mv-expand/union/join/make-series`.

---

## Dependencies on work owned by the other agent (not in scope here)

- Read-API authentication (`.scratch/prod-readiness/issues/002`): the logs read routes must be registered in the protected group; until then they are internal-only like the events routes.
- Multi-item envelope parsing (review §6): the Sentry log adapter needs the item iterator; share it rather than duplicating.
- Dockerfile cgo/Go version (§3), shutdown drain of `pebbleChan` (§4), `ValidateKey` cache (§10): the logs pipeline drains its own channel and adds no new SQLite reads beyond the existing key lookup, but ingest throughput will still be bounded by the uncached key lookup until that lands.

---

## Verification

- Go: `cd event_store && go test ./...` — new packages `logs`, `kql`, `storage` (logs writer, manifest, archive, retention with `t.TempDir()`), `pipeline` (ingester with a fake writer), `handler` (Fiber `app.Test` for `/logs` ingest and query routes); KQL golden files under `kql/testdata/*.kql` → `.sql` and execution tests over an in-memory DuckDB with fixture rows.
- Rails: `cd console && bin/rails test` — controllers with `sign_in_as` + WebMock stubs of `LogStore`, monitor evaluation job with `ActiveJob::TestHelper`, notifier payloads, serializers; system smoke test opens `/:org/logs`.
- End to end (`bin/dev`): (1) `curl -X POST localhost:4000/api/1/logs -H 'Authorization: Bearer <public_key>' -d '[{"message":"hello","level":"info","http":{"status":500}}]'`; (2) a Rails app with `sentry-ruby` 7 pointed at the DSN emits `Sentry.logger.warn`; (3) Vector `http` sink and Fluent Bit `http` output using the configs from the research; then in the console run `logs | where level == "error" | summarize count() by bin_auto(_time), service`, add it to a dashboard, create a threshold monitor with a Slack integration and see it fire and resolve.
- Load: `bin/load-test --kind logs --scenario wide_schema --vus 100` reports rows/s, p95, success rate; disk growth after the hourly archive matches the manifest.

## Assumptions

- Logs page is org-level with a project selector (like Issues); one dataset per project; `service` is a column, not a separate "source" entity.
- Shippers authenticate with the project's existing public key (as a Bearer token); no new token type.
- DuckDB stays at the pinned 1.4.3; an engine upgrade is a separate decision.
- SQL mode ships after KQL (M6), for the sandboxing reasons above.
- Monitors are evaluated by Rails (Solid Queue) at one-minute granularity; Go only answers queries.
