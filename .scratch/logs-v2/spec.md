# SolidTrace Logs v2: charts, dashboards, monitors and a bucket for Parquet

Status: in progress

**Origin:** the plan the user approved on 2026-10-10. They asked to implement it directly, so this file is the spec. Departures go in Implementation decisions at the end.

## Context

Logs v1 is built on `feature/logs-v1`: ingest, per-Project hot tables, hourly Parquet on local disk, a KQL-style Query language and the Logs explorer with Builder, Query and SQL modes. The v1 spec deferred the rest of the Axiom-style experience to v2: charts for `summarize … by bin(time, …)`, dashboards, monitors and an S3/GCS tier (`.scratch/logs/map.md:48-63`, `.scratch/logs-v1/spec.md:270`). The user wants these built next, with a polished, developer-friendly UI, and Axiom as the reference.

**What already exists to build on:**
- **The language** already does `summarize count()/avg/percentile … by bin(time, 5m)` and `bin_auto(time)`. Tables come back as typed columns plus long-form rows (`event_store/logquery/table.go:233-282`, `handler/logs.go:137-160`). No chart library is declared, and the histogram strip is built from flex divs.
- **Integrations** already cover Slack (webhook, Block Kit), PagerDuty (Events v2, trigger only) and email, through an outbox with retries (`console/app/services/integration_notification.rb`, `app/services/notifiers/*`). The outbox is tied to Issues: `notifications.issue_id` is NOT NULL, and rows without an Issue are skipped as "Issue missing" (`integration_notification.rb:146`).
- **Log Parquet** is local-only and found by globbing directories (`storage/logs_files.go`, `logs_read.go`, `logs_retention.go`, `logs_disk_limit.go`).
  - The Event archive is read through one local glob view (`storage/duckdb.go:102-140`).
  - httpfs is not in the static DuckDB build (`duckdb-go v2.10506.0`, DuckDB 1.5.6).
  - Config is YAML (`event_store/config/config.go`).

**Decided with the user (2026-10-10):**
- **Self-hosted, on DuckDB and Parquet.** BigQuery was costed and rejected: ingest and storage alone cost about as much as the whole VM, and per-scan billing punishes explorer, dashboard and monitor traffic.
- **"Metrics"** are numbers computed from Log Queries. There is no metrics ingest.
- **Dashboards** belong to an Organization, and each chart picks its own Project.
- **Monitors:** Threshold and Match, over Logs. The existing Issue rules on Integrations stay as they are.
- **Bucket (S3 or GCS):**
  - It holds Log Parquet and the Event archive.
  - Log files are uploaded at once, but the local copy stays for **24 hours** (configurable, and 0 is allowed).
  - With no bucket configured, everything stays local, as today.
- **Dashboard speed:** a **closed-bin result cache** is built now. Daily compaction in the bucket is added only if the probe measures long-range charts as slow.
- **Permissions:** any Organization member can view, create, edit and delete Dashboards and Monitors.
- **UX:** polished and very easy for developers. The UI is checked with Playwright screenshots.
- **Execution:** implement directly, phase by phase. This plan is the spec, and departures go in its Implementation decisions.

## UX principles (every phase)

The references are Axiom and the screenshots in `.scratch/logs-v1/*.png`.

1. **One click from any value.** Every value in the stream, a table, a chart legend or the inspector opens the same value menu, which extends `pages/Logs/components/TagMenu.tsx`: Show only, Hide, Group by, Chart this, Add column and Copy.
2. **Keyboard first:**
   - ⌘K opens a palette that jumps to a Project's Logs, a Dashboard, a Monitor or a recent Query.
   - `/` focuses the Query, ⌘↵ runs it, Esc closes the top panel, and j and k move through Logs.
   - `?` lists every shortcut.
3. **Every view is a link.** "Copy link" offers "as shown", which turns a relative range into an absolute one, and "live", which keeps it relative.
4. **Fast, honest feedback.**
   - Skeletons rather than spinners, and Cancel on every run.
   - Each run shows "1,415 Logs · 230 ms", from an `elapsed_ms` that EventStore adds to its responses.
   - Toasts confirm saves, and errors say what to do next.
5. **Zero-config start.** An empty Project shows copy-paste snippets with its key filled in: curl, Vector, Fluent Bit and a Sentry SDK. A "Send a test Log" button lets the page light up at once.
6. **One component per job.** The explorer, Dashboards and Monitors share one `TimeRangePicker`, `QueryEditor`, `QueryChart` and value menu.
7. **Looks right.**
   - It works in dark and light mode, at the dense style v1 set, from 390 px wide up.
   - No clipped text, such as the Project name in `1.png`.
   - No popover covers its own controls, the flaw seen in v1.
   - Focus rings are visible, and chart colours are checked with the dataviz skill's validator.

## Phase 0 — Set-up

1. **Commit the open work.** The working tree holds uncommitted Builder, PickList and inspector work on `feature/logs-v1`. Ask the user to commit it, or commit it for them with their approval, before branching.
2. **Branch.** Create `feature/logs-v2` from `feature/logs-v1`. Each phase gets its own branch, merged `--no-ff`. Fresh worktrees need `mise trust` and `bun install`.
3. **Tracker.**
   - Copy this plan to `.scratch/logs-v2/spec.md`, with an `## Implementation decisions` section.
   - Add a Logs v2 row and a Next entry to `.scratch/STATUS.md`.
   - Keep agent notes in `~/.cache/solid-trace/logs-v2-notes/`.
4. **Glossary** (`CONTEXT.md`, with the domain-modeling skill):
   - **Chart**, **Dashboard**, **Monitor**, **Threshold monitor** and **Match monitor.**
   - A Monitor is **firing** or **ok**.
   - **Log file store**, the local disk or a bucket, and **Bin cache**.
   - "Alert" stays on the _Avoid_ lists.
5. **ADRs:**
   - 0006: Monitors are evaluated by Console through the query API, and the outbox carries Monitor rows.
   - 0007: Parquet in a bucket, read by DuckDB through httpfs and tracked by a manifest.
   - Amend 0003: SQL mode can read and write the bucket through the instance's DuckDB secret.
6. **Playwright harness.** The user asked for it, and Chromium is already cached in `~/.cache/ms-playwright`.
   - Add `@playwright/test` (bun) with `console/playwright.config.ts` and specs in `console/e2e/`.
   - It runs against the `Procfile.dev` stack on `http://localhost:4001`, reusing a running server, with one headless worker.
   - Setup runs `db:seed` (`admin@solidtrace.local` / `password123`) and the dummy-Logs dev task from `3b962da`.
   - **Flows** assert what a user sees. **Screens** capture every page at 1440×900 and 390×844, in dark and light, into `~/.cache/solid-trace/screenshots/<phase>/`.
   - Scripts: `bun run e2e` and `bun run e2e:screens`. They run locally, not in CI.
   - **Review loop:** each UI ticket ends by running the screens, reading the PNGs and fixing what looks wrong.
7. **Click through Logs v1 first.** This is STATUS "Next" item 1.
   - Cover Builder, Query and SQL modes, the histogram drag, the inspector and Back with flows and screens.
   - Fix what's broken, and list the rough edges for Phase 6.

## Phase 1 — Charts in the Logs explorer

**EventStore: chart hints on table responses**
- New `event_store/logquery/shape.go`, called from `finish()` (`stages.go:347`), walks the compiled tail. Hints come from the tail, not from `chips.go`, which is nil for Queries the Builder can't show.
- **Rules:**
  - Take the last summarize S. Its `by` column whose expression is `bin` over a datetime gets `bin_seconds`.
    - `bin_auto` compiles to the same call with a literal size (`expr.go:670-682`).
  - A column whose expression is a bare aggregate gets `agg` (count, countif, dcount, sum, avg, min, max or percentile).
  - The hints survive a later `project` or `extend` only as a plain rename. Any other expression clears them.
  - `fill_gaps` is false when a `where`, `take`, `top`, `count`, `distinct` or `summarize` follows S, or when the table is `truncated`.
    - Truncation cuts the newest bins, because unsorted tables are `ORDER BY ALL`.
- **Response** (`table.go` Column, `handler/logs.go`): columns carry `agg` and `bin_seconds`. The table carries `fill_gaps`, `from` and `to` from `query.Range()`, which is the narrowed range, not the bar's.
- **Also:** `elapsed_ms` on every query response, and an optional `timeout_seconds` that cancels the scan on the server. Dashboards and Monitors use it; today a scan runs on after the client leaves (`logs.go:70-72`).
- **Tests** in `event_store/logs_language_test.go`: the hints with and without later stages, renames, arithmetic over aggregates, truncation and the timeout.

**Console: one chart module**
- **Dependencies:** declare `recharts` in `package.json`. Restore `components/ui/chart.tsx` from `backup/logging-v0`. Set palette tokens for light and dark with the dataviz skill.
- **`lib/charts/series.ts`** is pure, with vitest tests. It turns a table into a chart model:
  - **Time series:** the bin column is x, number columns are series, and string or bool columns split them.
  - **Bars:** a summarize with no time bin.
  - **Stat:** one row with one number.
  - **None** for anything else.
  - It pivots long rows to wide.
  - When `fill_gaps` is set, it fills empty bins across `from`–`to`: zero for count, countif, dcount and sum, and a gap for the other aggregates.
  - It keeps the top 10 series by total and says how many were left out.
- **`components/charts/QueryChart.tsx`:**
  - Line, area, stacked bars and a stat tile.
  - A tooltip, and a legend that toggles series and opens the value menu.
  - Dragging zooms the range, reusing `zoomRange` from `lib/logs/histogram.ts`.
- **Explorer** (`pages/Logs/Index.tsx` `ResultArea`, `LogTableView.tsx`):
  - A chartable table shows the chart above the table, with a Line, Area, Bars or Table switch.
  - `viz` goes in the URL through `lib/logs/explorerState.ts`, with tests.
  - The histogram strip hides while a time chart shows, as in Axiom. This is a departure from v1; record it.
- **Builder:** a new Summarize defaults to `by bin_auto(time)` (`lib/logs/builder.ts`, `BuilderPanel.tsx`).

## Phase 2 — Developer UX foundations

These are built once here and reused by Phases 3 and 4.

- **`components/TimeRangePicker.tsx`:**
  - The quick-range grid and start and end dates from `6.png`, plus typed shorthand (`15m`, `2d`, `yesterday`).
  - It replaces the explorer's range select. Its parsing lives in `lib/timeRange.ts`, with tests.
- **`components/QueryEditor.tsx`** (CodeMirror 6) replaces the `QueryBox` and `SqlBox` textareas.
  - Highlighting for the Query language, and `@codemirror/lang-sql` for SQL.
  - Autocomplete for stages after `|`, functions, and Fields with their types (from the field list in `lib/logs/pick.ts`).
  - Error squiggles at the line and column validate returns, and ⌘↵ to run.
- **Value menu:** extend `TagMenu.tsx` with Group by, Chart this (`summarize avg(f) by bin_auto(time)` for numbers, and `count() by f, bin_auto(time)` otherwise) and Add column. Use it everywhere.
- **Command palette and shortcuts:**
  - `components/CommandPalette.tsx`, built on the existing `dialog` and `combobox`.
  - `lib/shortcuts.ts` and a `?` sheet.
  - Recent Queries are kept per viewer in `localStorage`, inside try/catch.
- **Copy link** with "as shown" and "live", in `lib/logs/explorerState.ts`.
- **Run line:** "N Logs · 230 ms", skeleton loading states, and sonner toasts.
- **Empty Project:**
  - `pages/Logs/components/GetStarted.tsx` shows snippets with the Project's key: curl, Vector, Fluent Bit and Sentry SDK.
  - A **Send a test Log** button calls `POST /:org/projects/:slug/logs/test`. Console posts a sample Log to EventStore ingest with the Project's key.
- **Inspector:**
  - "Logs around this one": the same Service, ±1 minute, in the stream with this Log highlighted.
  - "Chart this field" on numeric Attributes.
- **Tests:** vitest for the range parsing, shortcuts, copy-link and editor completions (logic only); Playwright flows for the palette, the shortcuts and the empty state.

## Phase 3 — Dashboards

**Data:**
- `dashboards`: organization_id, name, description, default range, auto-refresh (off, 1m or 5m), created_by_id.
- `dashboard_charts`: dashboard_id, project_id, title, query, viz (line, area, bar, stat or table), options JSON (stacked, unit, decimals), and grid position x, y, w, h.
- A chart's Project must belong to the dashboard's Organization.

**Routes and controllers** (under `scope "/:org_slug"`):
- `resources :dashboards`, plus `PATCH layout`, and nested `charts`.
- `GET charts/:id/data?from&to` runs the saved Query through `EventStore.query_logs`, with `timeout_seconds`.
  - Long Query text never travels in a URL.
  - The Organization is checked on the server, as at `logs_controller.rb:114-120`.
- Move `LogsController`'s `rescue_from` and range parsing into a concern that both controllers share.
- Serializers match `app/serializers/*`.

**Frontend:**
- **`pages/Dashboards/Index.tsx`:** the list, and a new-dashboard dialog.
- **`pages/Dashboards/Show.tsx`:**
  - The shared `TimeRangePicker`, with the range in the URL.
  - Refresh, and an Edit mode with a drag-and-resize grid. Use `react-grid-layout` after checking it works with React 19; otherwise a CSS grid with preset widths.
  - Each chart has "Open in explorer", plus Duplicate and Delete.
- **Chart editor dialog:**
  - Project, then the Builder panel or the `QueryEditor`, checked through validate.
  - Viz options and a live `QueryChart` preview.
- **`lib/dashboards/fetchQueue.ts`:** at most 4 chart runs in flight. They are aborted on a range change or when the page unmounts.
- **"Add to dashboard"** in the explorer, beside the viz switch, saves the current Project, Query and viz to a new or existing dashboard.
- **Sidebar** (`components/Sidebar.tsx`): a "Dashboards" entry, with care over the `active` regexes.

**Tests:**
- Minitest request tests: membership; a Project from another Organization is rejected; the data endpoint passes the saved Query and range (WebMock); layout updates.
- vitest for the fetch queue.
- Playwright: build a dashboard from the explorer, resize it, and change its range.

## Phase 4 — Monitors, with Slack and PagerDuty

**Data:**
- `monitors`: organization_id, project_id, name, kind (threshold or match), query, interval_minutes, range_minutes, operator (`>`, `>=`, `<`, `<=`), threshold, notify_by_group, alert_on_no_data, disabled_at, last_run_at, last_error, ingest_cursor.
- `monitor_integrations`.
- `monitor_states`: monitor_id, group_key, status (ok or firing), since, last_value.
- `monitor_runs`: the last 100 per Monitor, pruned by the cleanup job.

**EventStore: "ingested after" for Match monitors**
- Log ids (`uuid.NewV7()` at `ingest/logs.go:74`) don't follow commit order, so `id > X` would miss Logs.
- Per Project, **batch ids do follow commit order**: they are made under the mutex with a floor, and the cursor commits with the rows (`logs_duckdb.go:295-304`).
- **Changes:**
  - Add a `batch` column to the hot tables and `logs_late`, with `ALTER … ADD COLUMN IF NOT EXISTS` at startup. It goes in `appendHotRows`' column list, at the end of `appendLateRows`' positional appender, and in `fixedColumns` (`logs_export.go:288`).
  - The manifest (Phase 5, step 1, done earlier on the Go track) records `batch_max` per file.
  - `logs/query` accepts `after_batch=W`.
    - It reads hot ∪ late ∪ the files with `batch_max > W`, keeping `batch > W AND batch <= C`, where C is the processing cursor read in the same transaction.
    - It ignores from and to, and returns `ingest_cursor: C`.
    - An empty W returns only C, which arms the Monitor.

**Evaluation (Console):**
- `MonitorSchedulerJob` runs every minute from `config/recurring.yml`. It enqueues `MonitorEvaluationJob` for each due Monitor, with `limits_concurrency` keyed by Monitor.
- `MonitorEvaluation.run(monitor, now)` is the one entry point, and the tests go through it.
- **Threshold monitor:**
  - The Query must end in a summarize with one numeric aggregate. Save checks this with validate and a dry run.
  - It runs over `[now − range − 60s, now − 60s)`, which allows for processing lag.
  - Each row is one group, keyed by its non-numeric columns.
  - `ok → firing` sends a "firing" notification, and `firing → ok` sends a "resolved" one. A Monitor that stays firing sends nothing more.
  - No rows count as ok unless alert_on_no_data is on.
- **Match monitor:**
  - It runs with `after_batch = ingest_cursor` and stores the new cursor.
  - It sends one message with up to 10 Logs and "N more", linking to the explorer.
- **EventStore down or timed out:** the run records `last_error`, changes no state, and the next tick tries again.

**Notifications:**
- **Outbox:**
  - `issue_id` becomes nullable (`schema.rb:112`), with `belongs_to :issue, optional: true`.
  - Add `monitor_id` and `group_key`, an index, and a check that exactly one subject is set.
- **`IntegrationNotification`:**
  - New entry point `monitor_changed`, and kinds `monitor_firing`, `monitor_resolved` and `monitor_matched`.
  - Monitor kinds stay out of `BATCHED_KINDS`.
  - Delivery splits rows by subject.
  - Rows for each (Monitor, group) are delivered as an ordered chain that stops at its first failure, so a resolve never overtakes a failed trigger.
  - De-duplication comes from `monitor_states` transitions.
- **Adapters:**
  - Monitor kinds build a `Notifiers::Message`: title, text, fields, link, severity, dedup key, and trigger or resolve.
  - The Issue branches stay unchanged.
  - **PagerDuty** gains `resolve`, with dedup key `solid-trace-monitor-<id>-<group>`.
  - **Slack** gets a Block Kit message with the value against the threshold and a link to the explorer.
  - **Email** gets a `MonitorMailer`.
  - Make the PagerDuty Events URL configurable, so tests and dev can point it at a stub.

**Frontend:**
- **`pages/Monitors/Index.tsx`:** status (firing, ok, disabled or error) and last run.
- **New/Edit:**
  - Project, kind, and the Builder or `QueryEditor`.
  - A Threshold monitor shows a 24-hour preview chart with the threshold drawn as a line, and the groups that would fire right now.
  - Interval, range and the toggles.
  - The Integrations multi-select links to "Add Integration".
- **Show:** status per group and the run history.
- **"Create monitor"** from the explorer, and a "Monitors" entry in the sidebar.

**Tests:**
- Go: `after_batch` across export, late Logs and two concurrent ingest requests.
- Minitest: a `MonitorEvaluation` service test (WebMock and `travel_to`) for transitions, groups, no data, the match cursor and EventStore down.
- Outbox tests for subject split and ordered chains, and notifier payload tests for trigger and resolve.
- Request tests for CRUD and scoping.
- Playwright: a Monitor fires into a local Slack-webhook stub.

## Phase 5 — Parquet in S3 or GCS, and the bin cache

This phase is Go-only, so it runs as a second track beside Phases 1–4.

**1. Manifest, in local mode too** (`storage/logs_manifest.go`):
- **Tables:** `log_files` (project, day, hour, name, bytes, min_time, max_time, batch_max, local, remote) and `log_file_deletions` (tombstones).
- **Export:** the row is inserted in the export transaction (`stageHour`) and is the commit marker. `recoverStagedFiles` becomes "row exists → rename, otherwise delete".
  - Keep `publishMu` around COMMIT plus rename.
  - Read the manifest inside `begin()` under the RLock.
- **Backfill** from disk once at startup, gated by a marker row.
- **Every listing moves to the manifest:** `hoursWithLogs`, `LogFiles.Hour`/`Project`, `nextHourFile` (late-n names), `projectDirs`, `dayDirs`, `DeleteProjectLogs` and `hourDirs`. Only `stagedFiles` keeps listing.
- **Pins** are keyed by the logical `project/day/hour/name`, not by path.
- **Test:** existing Queries return the same rows before and after.

**2. Probe** (under a minute, from the GCP dev box):
- LOAD httpfs into the static duckdb-go binary with `extension_directory` set and autoinstall and autoload off.
- Check that a secret created on one connection is seen by pooled connections.
- Read a mixed local and `gs://` file list.
- Time a 24-hour and a 7-day count-by-bin chart, cold and warm, with DuckDB's Parquet-metadata, HTTP-metadata and external-file caches on (setting names to confirm on 1.5.6).
- Record the results in `.scratch/logs-v2/research/`, and decide on daily compaction from them.
- **Fallback** if httpfs won't load: Go downloads remote files into the local cache before reading.

**3. Object store:**
- **Config** (`config/config.go`, `deploy/event_store.yml`):

  ```yaml
  object_store:          # absent: local only
    kind: s3 | gcs       # gcs uses the S3 interop API with HMAC keys
    bucket: …
    prefix: solidtrace/
    region: …
    endpoint: …          # MinIO, R2; set for you when kind is gcs
    local_cache_hours: 24
  ```

  Keys come from the `SOLIDTRACE_OBJECT_STORE_KEY_ID` and `_SECRET` environment variables.
- **`ObjectStore` interface:** `Put(ctx, key, localPath)`, `Delete`, `List`, `URL(key)`.
  - The aws-sdk-go-v2 adapter is new; don't reuse the Pebble `s3_storage.go` and `gcs_storage.go`.
  - Set `RequestChecksumCalculation` and `ResponseChecksumValidation` to `WhenRequired` for GCS interop.
  - The body is an `*os.File`, so nothing is buffered.
  - A nil store means local-only. A `dirStore` fake serves the tests.
- **DuckDB** (`storage/duckdb.go:50-66`): LOAD httpfs and CREATE SECRET only when a bucket is configured.
- **Dockerfile:** download the v1.5.6 httpfs for each `TARGETARCH` into a fixed extension directory.

**4. Upload and eviction:**
- **Uploads** (`storage/logs_upload.go`, plus a worker in `logprocessing`):
  - Each file is uploaded right after it is published.
  - Uploads run outside `writeMu`; only the "mark remote" UPDATE takes it.
  - Failures retry every minute.
- **Eviction** in `Cleanup` removes local copies of uploaded files older than `local_cache_hours`, respecting pins.
- **Reads** use the local path when one exists and `URL(key)` otherwise.
- **`LogByID`** without a time reads local files only.
- **The disk limit** first evicts local copies that are already uploaded. It falls back to deleting data only for files that aren't uploaded yet, and logs an error when it does.
- **Retention and Project deletion:**
  - They write tombstones in the transaction.
  - An async deleter removes objects one at a time (GCS has no multi-delete), respecting pins and never under `publishMu`.

**5. Bin cache**, in EventStore, for local and bucket modes:
- **Eligible Queries:** those whose hints say S has a time bin, it is the final stage, and the bin column is first.
- **Cached bins:** bins that lie wholly inside hours already exported, stored in an in-memory LRU of about 64 MB.
  - The key is (Project, Query hash, bin size, bin start), plus the manifest generation of the hours it covers. A late-n file or a retention delete bumps that generation.
- **A run** computes only the missing bins and the live span from the hot tables, then joins them in order.
- **Test:** the same rows with the cache on and off, across export, late Logs and retention.

**6. Event archive:**
- After `ArchiveEventsUpTo` publishes, upload the files and delete the local copies.
- `recreateEventsView` reads an explicit file list, taken from `ObjectStore.List` at startup and extended after each run, plus any local files still waiting to upload.

**7. Daily compaction** happens only if the probe calls for it: merge a closed day's hour files into one file with larger row groups, updating the manifest in one transaction.

## Phase 6 — Polish pass

- Run every Playwright screen at both sizes and in both themes, and read the PNGs.
- Work through the Phase 0 rough-edges list and anything new.
- **Check:**
  - Copy in the repo's plain style.
  - Empty, loading and error states on every page.
  - Focus order and aria labels.
  - Chart colours with the dataviz validator.
  - 10,000 stream lines staying smooth, and a 12-chart dashboard on a 7-day range.
- Update `docs/logs/query-language.md` (charts) and add `docs/logs/monitors.md` and `docs/logs/object-store.md`. The last covers the HMAC key steps for GCS and the IAM policy for S3.

## Order and parallelism

At most 2 sub-agents run tests at once. Go tests run with `-p 1`.

| Track | Order |
|---|---|
| A (Console and frontend) | 0 → 1 (Console) → 2 → 3 → 4 (Console) → 6 |
| B (EventStore) | 1 (hints, `elapsed_ms`, timeout) → 5.1 manifest → 4 (`batch`, `after_batch`) → 5.2 probe → 5.3–5.4 → 5.5 bin cache → 5.6 → 5.7 if needed |

## Verification

- **EventStore:** `mise exec -- go test -p 1 ./...` in `event_store/`.
  - This includes the end-to-end tests in `event_store/*_test.go` with `clock.Manual`.
  - An opt-in MinIO test runs when `SOLIDTRACE_TEST_S3_ENDPOINT` is set.
- **Console:**
  - `RAILS_ENV=test bin/rails db:reset`, then `mise exec -- bin/rails test`, run after the Go suite and not alongside it.
  - `bun run test` (vitest) and `tsc`.
- **UI:** `bun run e2e` (flows) and `bun run e2e:screens`, with the PNGs reviewed after every UI ticket.
- **Real bucket:** one manual run against a GCS bucket from the dev box, with an HMAC key the user provides. Upload, evict after `local_cache_hours`, query across local and remote hours, retention delete, and the Event archive view.
- **Real notifications:** a Monitor fires and resolves into a real Slack webhook and a PagerDuty test service, if the user provides them; otherwise into the local stubs.
- Benchmarks and probes stay under a minute.

## Risks to watch

- httpfs may not LOAD into the static binary. The fallback is downloading files into the local cache.
- GCS interop quirks: checksums, ListObjectsV2.
- `union_by_name` reads one footer per remote file.
- `alert_on_no_data` may fire falsely during a Log processing backlog. Consider holding it while the backlog is over 1 minute (`processor.go:33`).
- `react-grid-layout` with React 19.
- CodeMirror's bundle size, so lazy-load the editor.

## Implementation decisions

Decisions made while building that depart from or add to the text above. Each gives what the spec said, the decision, the reasoning and where it landed.
