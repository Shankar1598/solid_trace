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

### A0, Logs v1 click-through

- **Taller pick lists, not a reordered Group by list.** Decision: I raised the max height of PickList's option list from max-h-64 to max-h-72, so a longer list ends part-way through a row and reads as one to scroll. Why: This is the smallest fix within the current design. Putting Fields before the time bins, or grouping the bins differently, would be a redesign. Where: `console/app/frontend/pages/Logs/components/PickList.tsx (7731c82)`.
- **Empty Query in Query mode is the Project's slug.** Decision: A Query-mode URL with no q opens with ['<slug>'] in the box, and a pivot from an empty Query that ran writes ['<slug>'] followed by the where. Why: Switching to Query mode already writes the slug, and an empty Query reads the same Logs as the slug alone. Before this, a pivot produced a where with no Project before it. Where: `console/app/frontend/pages/Logs/Index.tsx (9fa01ea)`.
- **Reverted hiding the bar's rule on a phone.** Decision: The phone bar's layout is left as v1 had it and listed as rough edge 3, with a suggested fix. Why: Without the rule, the hint fitted beside the mode switch and wrapped into a column of single words, which looked worse. A proper fix (hide the hint and the rule together, push Run right) changes the bar's layout, which is Phase 6 work. Where: `console/app/frontend/pages/Logs/Index.tsx (fc3034a, reverted by 5794163)`.
- **Extra flow coverage beyond the six listed paths.** Decision: The flows also cover Cancel, Show only from a table cell, the JSON tab's fold buttons, Add as a column, Count by from the inspector, and pivots in Query mode. Why: Scratch probes showed these all work. Covering them guards the paths Phases 1–6 will extend, at little cost. Where: `console/e2e/flows/logs-v1-{builder,inspector,query}.spec.ts`.
- **Polish listed, not fixed.** Decision: Items that work but are rough, or that need design or EventStore changes, are listed and not fixed. Examples: font ligatures, number formatting, sorting by severity, a full-screen inspector on a phone, and "Ctrl+Enter" shown on a Mac. Why: The task says to fix what is broken with small commits within the existing design, and to leave polish to Phase 6. Where: `.scratch/logs-v2/research/v1-click-through.md`.
- **STATUS.md not updated.** Decision: I left .scratch/STATUS.md alone. Its "Next" item 1 (click through the Logs explorer) is now done and can be ticked by the orchestrator. Why: The brief says to stay within the unit, and the orchestrator keeps the tracker. Where: `.scratch/STATUS.md`.
- **Put the slug Query in what ran, not just the draft.** Decision: In Query mode an empty Query becomes the Project's slug Query in both current and draft. Opening a Query-mode link with no q doesn't rewrite the URL. Builder to Query and SQL to Query replace the history entry with q=slug. Why: '' and the slug Query read the same Logs. Making them equal at the source fixes the extra history entry, the extra rerun and the empty SQL-to-Query box in one place, with no '' versus slug comparisons spread through runExplorer and enterBuilder. Where: `console/app/frontend/lib/logs/explorerState.ts fillEmptyQuery; Index.tsx open, switchTo('query'), exitSql`.
- **An empty Builder skips validate for the slug Query.** Decision: builderView starts ready with EMPTY_BUILDER when current.query is '' or the slug Query. Why: Otherwise a Query-mode link with no q would now send an extra validate request on open, just to learn the Builder is empty. Where: `Index.tsx builderView initial state`.
- **Scroll DuckDB's LINE to its caret on a phone.** Decision: Only the LINE and caret pair is unwrapped. It sits in a sideways-scrolling pre in the app's font and is scrolled so the caret is centred when it lies past the right edge. The prose still wraps. The error notice is role=alert. Why: Unwrapping the whole error, as the review suggested, would make the prose scroll sideways on a phone too. Without the scroll, the caret would sit off-screen. Where: `Index.tsx ErrorNotice and PointerLines, lib/logs/errorParts.ts`.
- **Fix the Query-mode race in the test helper, not with a new busy state.** Decision: runBy waits for the run's histogram response before settled(). The page still shows nothing while validate answers. Why: This was the review's first suggestion. A busy state during validate would change the product within a click-through unit and would flash for 15–30 ms. Where: `console/e2e/support/logs.ts runBy`.
- **Turn SQL off only when the last live holder releases it.** Decision: Each run writes a holder file per process; I didn't use a lock or edit setup.md. Why: It is small, removes the duplicated toggle, and clears holders left by dead runs. One gap remains: while one run holds SQL on, another run's non-SQL screens show the SQL button in the mode switch. Only screenshots are affected. Where: `console/e2e/support/logs.ts holdSqlQueries and releaseSqlQueries`.
- **New rough edges go in their own section.** Decision: The sort tie-break and keyboard items are numbered 29 and 30 under 'Found in review', not slotted into the existing sections. Why: Items 1–28 keep their numbers, which reviewers and notes already cite, such as 'rough edge 6'. Where: `.scratch/logs-v2/research/v1-click-through.md`.
- **SQL-mode wording.** Spec said: Errors say what to do next (UX principle 4) Decision: 'Running the SQL…' and 'SQL cancelled. Run it again.' Why: SQL answers a table, not Logs, and CONTEXT.md keeps Query and SQL query apart. Where: `Index.tsx ResultArea`.
- **Restarted the Vite dev server.** Decision: Restarted vite. Its file watcher had stopped: its log had nothing after 21:03, and it was still serving the old Index.tsx after my edit. Why: Without the restart the fixed code never reached the browser, and the flow kept failing against stale code. Other agents should know that frontend edits from about 21:04 onwards may not have been served until this restart (around 22:10). Where: `dev stack (stack.sh restart vite)`.
- **Fixed inline, tested with a Playwright flow.** Decision: The fix is one line in the page, as the finding suggested. I did not add a new lib helper with a vitest test. Why: The bug is in how the page wires state helpers that already exist. The flow acts as a user, covers both the empty and the filtered Builder, and failed before the fix. Where: `console/app/frontend/pages/Logs/Index.tsx enterBuilder`.
- **The flow checks the q parameter, not the whole URL.** Decision: The test reads `q` from the URL and checks it has no '| project'. The finding suggested checking that the URL has no 'project'. Why: The URL path always contains 'projects/solid-trace-project', and the Query's source is ['solid-trace-project'], so a check for plain 'project' would always fail. Where: `console/e2e/flows/logs-v1-query.spec.ts`.

### B1, chart hints, run timing and timeouts

- **base_commit is the commit before the unit, not HEAD at start.** Spec said: Record git rev-parse HEAD before your first change as base_commit Decision: Reported 85fd16d rather than 79416ef. Why: 79416ef is this unit's own chart-hints commit, from an earlier run that was stopped part-way. Diffing from 85fd16d lets the review cover the whole unit. Where: `git history of feature/logs-v2-go`.
- **timeout_seconds is checked and capped at one day.** Spec said: an optional timeout_seconds (positive number) Decision: 0, negative values, values over 86400 and non-numbers get 400 with {"error"}, the same shape as the other request-shape errors. Why: Rejects nonsense values, keeps time.Duration well clear of overflow, and no Dashboard or Monitor needs more than a day. Where: `event_store/handler/logs_run.go startRun`.
- **Wording of the 504 message.** Spec said: saying the run was stopped after N s Decision: "the run was stopped after N s, its timeout: try a shorter time range". Lowercase with no full stop, like the other EventStore errors, and it says what to try next (UX principle 4). Why: The message should read as a cause plus a next step for whoever sees it on a Dashboard or Monitor. Where: `event_store/handler/logs_run.go stopped`.
- **elapsed_ms only on successful answers, measured from after the body is parsed.** Spec said: elapsed_ms on every query, histogram and sql answer Decision: Added to 200 answers only; 400, 403 and 504 bodies keep their existing shapes. The time counts compile, the Project slug lookup and the scan. Why: Keeps the error shapes exactly as the contract states them, and the run's time is what the explorer's "230 ms" means. Where: `event_store/handler/logs_run.go answer`.
- **A Log-rows run past its timeout answers 504, not a partial page.** Decision: When the timeout passes during a Log-rows scan, the answer is 504, the same as for tables, even though rows scans already return partial pages with a next_cursor after ScanBudget. Why: Matches the contract: past the timeout the scan stops and the answer is 504. ScanBudget (1.5 s) still bounds ordinary rows runs. Where: `event_store/handler/logs.go Query`.
- **A time bin made before the last summarize counts.** Spec said: Its by column whose expression is bin over a datetime gets bin_seconds Decision: The last summarize's by column also gets bin_seconds when it groups by a column that an earlier extend, project or distinct binned (for example `extend hour = bin(time, 1h) | summarize count() by hour`), or when it groups by an earlier summarize's bin column. Columns binned after the last summarize, or with no summarize at all, get no hint. Why: This is a common way to write a KQL time series, and the column's expression really is a time bin. It costs a flag in shapeOf. Where: `event_store/logquery/shape.go shapeOf, renamed`.
- **distinct keeps hints for plain columns.** Spec said: hints survive a later project or extend only as a plain rename; fill_gaps false when distinct follows S Decision: distinct follows the plain-rename rule for hints and clears fill_gaps. The earlier run had cleared all hints after distinct. Why: distinct of columns keeps their values unchanged, the same as project, so their hints still hold. Where: `event_store/logquery/shape.go`.
- **Losing the time-bin column clears fill_gaps; only one by column is the time axis.** Decision: fill_gaps also needs a time-bin column to survive to the output: if a later project drops it, or an extend redefines it, fill_gaps is false. When a summarize groups by two time bins, only the first gets bin_seconds and the second is an ordinary group. floor(x, X) is treated like bin. (These came from the earlier run of this unit.) Why: Without a bin column there are no bins to fill, and a chart needs exactly one time axis. Where: `event_store/logquery/shape.go`.
- **Bins of any datetime get bin_seconds and may fill gaps.** Spec said: bin(time, X) or bin_auto(time) of a datetime Decision: Followed the contract as written: any datetime counts, not only the Log's own time. Why: This matches the contract. Caveat: for bins of some other datetime (for example a todatetime() of an Attribute), filling across from–to, which is the Log time range, could add zero bins that mean nothing. That case is rare, so I flag it here rather than design around it. Where: `event_store/logquery/shape.go timeBin`.
- **Tests in new files beside logs_language_test.go.** Spec said: Tests in event_store/logs_language_test.go Decision: Put them in event_store/logs_chart_hints_test.go and event_store/logs_run_test.go, in package main_test next to logs_language_test.go. Why: logs_language_test.go is already long. The new files reuse its seed (seedLanguageLogs) and helpers, and the orchestrator's prompt asked for tests beside it. Where: `event_store/`.
- **How the timeout test shows the scan really stopped.** Decision: SQL that would take minutes, `SELECT sum(hash(i)) FROM range(1e12)`, with timeout_seconds 0.25 must answer 504 within 5 s. The handler answers only after DuckDB returns, so a quick 504 means the query itself was interrupted. Query and histogram runs use a 1 µs timeout. A run after the timeouts must still count correctly. Why: No asserts on generated SQL, and no new test seam needed. A separate throwaway probe confirmed that table scans stopped part-way leave pooled connections healthy. Where: `event_store/logs_run_test.go`.
- **How the Query-path timeout coverage is split.** Spec said: a timeout returns 504 and stops the scan (use a tiny timeout over enough data, or a test seam for a slow query) Decision: Coverage is split three ways. A storage test stops ScanLogTable and ScanLogs mid-DuckDB and then reuses the pooled connection. An end-to-end test stops a Log rows run part-way through its hours. The existing 1 µs end-to-end cases show the handler passes run.ctx to storage for query, table and histogram. There is no end-to-end table run that is stopped while DuckDB is running it. Why: A table scan runs as one DuckDB query, so making it slow end to end would need tens of millions of seeded Logs. That is too heavy for this low-memory box. The three tests together catch each regression the findings describe, and mutation probes confirmed it. Where: `event_store/storage/logs_read_test.go, event_store/logs_run_test.go`.
- **Which column is the time axis.** Spec said: The time-bin column carries bin_seconds (singular) Decision: Only the first column in the table's output order that holds time bins carries bin_seconds. A copy made by a rename is an ordinary datetime column. agg may still be copied to more than one column. Why: A chart needs exactly one x axis, and the Console series module can rely on this. Where: `event_store/logquery/shape.go shapeOf, logquery/table.go Column doc`.
- **Size of a time bin that is binned again.** Decision: When bin/floor is applied to a column that already holds bins of size H with new size S, bin_seconds is H if S divides H, otherwise S. Why: All bins count from 1970 (time_bucket origin), so bins of a size S divides stay where they are. With any other S, rows move onto S's grid, and S is the spacing a chart should fill gaps by. Where: `event_store/logquery/shape.go timeBin`.
- **bin_auto sizes its bins by the narrowed range.** Decision: The head is narrowed before the tail compiles, so bin_auto uses the same range as the table's from/to and the run's scan. Why: Otherwise a wide request range with a narrowing where gave bins larger than the table's own range, so a chart showed a single bin dated before its from. Where: `event_store/logquery/compile.go Compile`.

### A1, charts in the explorer

- **At most 8 series, not 10.** Spec said: It keeps the top 10 series by total and says how many were left out. Decision: MAX_SERIES = 8. When there are several number columns, the split values are ranked by the first column's total and keep floor(8 / number of columns) of them, each with every number column. The legend says 'N more not shown'. Why: The dataviz skill allows at most 8 validated categorical hues and forbids cycling or inventing a 9th. Ten series would force two of them to share a colour. Where: `console/app/frontend/lib/charts/series.ts`.
- **Histogram strip hides while a time chart shows.** Spec said: The histogram strip hides while a time chart shows, as in Axiom. This is a departure from v1; record it. Decision: The strip is hidden while the last answered table is a time chart and viz is not Table. That choice holds through a re-run or a cancel, so the strip doesn't flash back. It returns for Table, bars, a stat, a Log stream or an error. The histogram is still fetched. Why: Departure from v1 that the spec asks to record. Holding the choice through a re-run avoids the layout jumping. Where: `console/app/frontend/pages/Logs/Index.tsx`.
- **viz values, and which switch options each table gets.** Spec said: Line, Area, Bars or Table switch; viz in the URL. Decision: viz is auto|line|area|bars|table, with auto as the default (not written to the URL). A time series offers Line/Area/Bars/Table, bars offer Bars/Table, and a single number offers Number/Table. Choosing Number goes back to auto. A form the table can't take falls back to the table's own form. Why: Line and area over categories, or bars for one number, would mislead. Where: `lib/logs/explorerState.ts, lib/charts/series.ts formFor, pages/Logs/components/ChartSwitch.tsx`.
- **Stacking only for values that add up.** Spec said: Line, area, stacked bars and a stat tile. Decision: Bars and areas stack only when the one number column is count, countif or sum. Otherwise bars sit side by side and areas overlap with a 10% fill. Why: Stacking averages, percentiles, distinct counts, or two different columns gives a total that means nothing. Where: `lib/charts/series.ts (stacked), components/charts/QueryChart.tsx`.
- **When a table is charted.** Spec said: Time series / Bars: a summarize with no time bin / Stat: one row with one number / None otherwise. Decision: Rules for which tables get which chart:
- **Stat:** exactly one row and one column, a number. This covers `count` and SQL `SELECT count(*)`.
- **Bars:** needs an agg column and a string or bool column for the bars. The first such column names the bars, the other string/bool columns split them, and at most 50 bars (the largest totals) are kept.
- **Number columns:** a number column before the first aggregate counts as a by column that splits series (e.g. status); one after it is a series (e.g. arithmetic over aggregates).
- **Not charted:** no rows, a dynamic or second datetime column, or two rows with the same time bin and split values. Why: Simple rules that never draw something misleading. The table below still shows everything. Where: `lib/charts/series.ts`.
- **Filling empty bins.** Spec said: When fill_gaps is set, fill empty bins across from–to. Decision: Bins run from floor(from / bin) × bin to just before `to`, at most 10,000 of them (beyond that, nothing is filled). When fill_gaps is set, a series missing from an existing bin gets the same zero-or-gap rule. Without fill_gaps, a missing value is a gap. Why: Matches EventStore's epoch-aligned bins and keeps very fine bins over long ranges cheap. Where: `lib/charts/series.ts`.
- **New Summarize from the inspector also starts with bin_auto(time).** Spec said: Builder: a new Summarize defaults to by bin_auto(time). Decision: Both adding the first function and the inspector's 'Count Logs by <field>' (groupBy) start a new Summarize with by bin_auto(time). A by clause added before any function does not get the time bin. Why: One click gives a chart. 'summarize by bin_auto(time), service' with no function would only list distinct pairs. Where: `lib/logs/builder.ts, BuilderPanel.tsx`.
- **Level series wear the histogram's Level colours.** Spec said: Keep series coloured by level consistent with the histogram strip. Decision: When level alone splits series and there is one number column:
- **Order:** series go worst first (fatal at the bottom of a stack, as in the histogram).
- **Colours:** sky/amber/red-500/red-800 for info/warn/error/fatal, as in the strip. Trace, debug and none get greys stepped at least 15 ΔE apart. With other splits or several number columns, series use the categorical palette. Why: Consistent with the strip. The level set fails the validator's lightness-band and chroma checks by design (fixed meanings, greys on purpose). It passes the neighbour-distinctness checks (CVD and normal vision) in both themes. Colours below 3:1 contrast are covered by the legend and the table always shown below. Where: `css/globals.css, lib/charts/series.ts`.
- **Only a drag zooms, not a click.** Spec said: Dragging zooms the range, reusing zoomRange. Decision: Dragging across two or more bins zooms; a click does nothing. Only mouse dragging works; touch dragging on the chart does not zoom. Why: Avoids zooming by accident when hovering for the tooltip or clicking the chart. On touch, page scrolling would fight a drag. Where: `components/charts/QueryChart.tsx`.
- **The table scrolls with the chart on short screens.** Decision: The chart is 176px tall on phones and 224px from the sm breakpoint up. The table below keeps at least 192px, and the two scroll together when there isn't room. Why: So the table stays reachable on a phone. Where: `pages/Logs/components/LogTableView.tsx`.
- **Two v1 tests changed for the new default.** Decision: logs-v1-builder flow ('a Summarize shows a table…') and the logs-v1 screen 'Summarize: a table' now check that bin_auto(time) is there, then remove it, so they still test a table of counts by service. Why: Their first cell was now the time bin, and on a phone `service` wrapped to the next line. Where: `console/e2e/flows/logs-v1-builder.spec.ts, console/e2e/screens/logs-v1.spec.ts`.
- **Partial edge bins: whole-bin axis instead of clamping.** Spec said: The finding suggested clamping the x domain to [from, to] (spec is silent). Decision: The x axis still spans whole bins. Ticks are placed only within [from, to], and partial edge bins are faded and labelled 'partial bin' in the tooltip. Why: When the domain was clamped, a bar narrower than its bin was clipped off the edge and disappeared. Centring partial bars on the covered part shrank every bar to hairlines, because recharts sizes bars from the smallest gap between points. Restricting the ticks removes the tick after the range's end, which was the visible fault. Where: `components/charts/QueryChart.tsx (domain, partialAreas), lib/charts/series.ts (partial)`.
- **Histogram strip hides for any table over time, Table included.** Spec said: The histogram strip hides while a time chart shows. Decision: The strip hides whenever the last answered table is a time series, whether it is shown as a chart or as Table. Before the first answer, binsTime(query) guesses from the Query text. Why: Otherwise choosing Table moved the switch about 95px under the pointer (A1-ux-1-6). The first-run guess stops the strip from vanishing when the chart arrives (A1-ux-1-7). This adds to the departure from v1 already recorded. Where: `pages/Logs/Index.tsx`.
- **Panels by unit, from column names.** Spec said: Number columns are series (one axis implied). Decision: Columns share a panel when they measure one thing: count and countif together, and avg/min/max/percentile together when named after one Field (EventStore's default names, e.g. avg_duration_ms, percentile_duration_ms_95). A renamed column, a sum, dcount or arithmetic gets its own panel. When each panel draws one column, colour follows the split value across panels and up to 8 split values are kept. Why: The dataviz rule allows one axis per unit only. The table hints carry agg but not the source Field, so the default column name is the only signal. A renamed column gets its own panel, which is always safe. Where: `lib/charts/series.ts unitOf/panels, QueryChart.tsx`.
- **Fatal is lighter than error in dark mode.** Spec said: Keep Level colours consistent with the histogram strip. Decision: Dark-mode fatal is red-300 (oklch 80.8% 0.114 19.571) in the shared token. The strip uses the same token now. Why: red-800 measured 2.1:1 on the dark card. red-600 and red-700 were within ΔE 6–13 of error. red-300 passed the contrast, CVD and normal-vision checks, and the Log stream already shows fatal text as red-300 in dark mode. Where: `css/globals.css --chart-level-fatal (dark)`.
- **Many categories draw as horizontal bars and the chart grows.** Spec said: Bars: a summarize with no time bin. Decision: With more than 8 categories or a name over 12 characters, bars are horizontal with names beside them and 20px per bar, so up to MAX_BARS=50 bars give about a 1,000px chart that scrolls with the table. Why: That way every bar has its name, as the dataviz skill asks for many or long-named categories. Table still shows the rows on their own. Where: `components/charts/QueryChart.tsx (across, NameTick)`.
- **An empty count is drawn only with no split columns.** Spec said: When fill_gaps, fill empty bins across from..to with zero for count/countif/dcount/sum. Decision: A table with no rows is drawn as a flat zero only when nothing splits it. A split table with no rows still shows only 'No rows'. Why: With no rows the split values are unknown, so there is no series to fill. Where: `lib/charts/series.ts zeroWhenEmpty`.
- **Colour slots live with the caller.** Spec said: (silent) Decision: QueryChart takes an optional { current: Map<id, slot> } holder. The explorer keeps one across runs. Without it, colours follow rank. Why: QueryChart unmounts during a run, and Dashboards will need a holder per panel. The pure keepColors keeps the model testable. Where: `QueryChart `colors` prop, Index.tsx colorSlots ref`.
- **Hidden series are held by the owner as a separate ref.** Decision: A new optional `hidden` ref, next to `colors`. SeriesChart stays keyed per table and takes its first hidden set from keepHidden. Without the ref (for example a future Dashboard panel that passes none), each new table shows every series, as before. Why: The explorer unmounts the chart while a run is in flight, so the hidden set must live with the owner, as colour slots do. Keeping the table key still resets a half-made drag on a new table. Where: `components/charts/QueryChart.tsx (HiddenSeries, prop `hidden`), pages/Logs/components/LogTableView.tsx (TableChart.hidden), pages/Logs/Index.tsx`.
- **Lying bars always show their value at the tip.** Spec said: The finding suggested showing values only when cut names still collide. Decision: Every lying bar shows its value, or its stack's total, at its tip. This holds when the bars stack or one series is shown; grouped, unstacked bars show none. Why: On a desktop the value axis of a 50-bar chart is far below the fold, and the dataviz skill's mark spec is 'Bars -> value at the tip'. Labels that show or hide depending on collisions would be inconsistent. Upright bars are unchanged: their value axis sits beside them. Where: `QueryChart.tsx PanelChart (LabelList)`.
- **Partial bins fade only while they are at most a third of the bins.** Decision: Partial bins are faded only when they are at most a third of the bins. A line or area fades the half bin either side of the partial point, so half of the segment into it is faded, not the whole segment. Otherwise nothing is faded, and a legend note names the partial bins. Why: Fading only the bin's span after a line's last point would fade nothing visible: the point sits at the bin's start. The half bin around the point is the region that point owns, so it is a fair middle ground. One third keeps 7 days of 1d bins faded (2 of 8 bins) and leaves 1 day of 1d bins (2 of 2) unfaded. Where: `lib/charts/series.ts fadedSpans / legendNotes`.
- **The chart model is still computed per consumer.** Decision: Each place still calls chartOf on its own; I did not compute the model once and pass it to QueryChart. Why: After the ranking fix, the worst case (10,000 rows) takes about 45 ms per call. QueryChart's contract for Dashboards and Monitors is a table and options, not a precomputed model. Where: `pages/Logs/Index.tsx, LogTableView.tsx, QueryChart.tsx`.

### B2, the Log file manifest

- **No PRIMARY KEY constraint on log_files.** Spec said: key (project, day, hour, name) Decision: The key is unique because of how names are chosen, not because of an index constraint. An export picks a name that is in neither log_files nor log_file_deletions for that hour. Why: Under DuckDB's multi-version concurrency, an index can wrongly report a duplicate key when a deleted key is inserted again while older read transactions are still open. Long reads keep transactions open, and an index failure would block exports. The table is small, so the index buys nothing. Where: `event_store/storage/logs_manifest.go manifestTables, nextHourFile`.
- **Backfill runs before recoverStagedFiles; an older version's staged files are settled the old way.** Spec said: Backfill from disk once at startup, after recoverStagedFiles Decision: When the marker row is missing, NewLogStore runs the backfill first. The backfill settles staged files by whether their Logs are still in DuckDB (the old rule), then adds a row for every published file. Only after that does the row-based recoverStagedFiles run. Why: On an install whose manifest is still empty, the row rule would delete staged files that an older version had already committed. That loses Logs if a crash between commit and rename is followed by an upgrade. The old check is about 25 lines that already existed. Where: `storage/logs_duckdb.go NewLogStore; storage/logs_manifest.go backfillManifest, settleStagedFilesByTheirLogs`.
- **A tombstoned name is never reused.** Decision: nextHourFile also counts names in log_file_deletions. So an hour whose data.parquet is waiting for its bytes to be deleted gets late-1 for its next file. Why: Without this, a new file with the same name could be published while the old tombstone is still there, and the next sweep would delete the new file's bytes. Where: `storage/logs_manifest.go nextHourFile`.
- **The disk limit still stops at an hour a read has pinned.** Spec said: Retention, Project deletion and the disk limit delete manifest rows in the same transaction and write tombstones Decision: Retention and Project deletion delete rows at once, and only the bytes of pinned files wait. The disk limit keeps today's behaviour: at the oldest hour that has a pinned file it stops, leaves that hour's rows, and returns ErrLogFilesInUse. Hours it deletes before that are tombstoned and swept. The pin check happens outside publishMu. A read that begins between the check and the commit is still safe, because the sweep respects pins. Why: Deleting the rows of a pinned hour frees no disk until the read ends. Keeping the rows also keeps the existing test passing, which expects the Log to stay visible during the read (TestTheDiskLimitWaitsForAReadOfTheFilesItWouldDelete). Where: `storage/logs_disk_limit.go EnforceDiskLimit`.
- **Where the sweep runs.** Decision: deleteTombstonedFiles runs after every deleter's commit, under writeMu but outside publishMu. EnforceDiskLimit also runs it first on every pass, so bytes still pinned from a Retention pass are retried every minute, because Cleanup retries on ErrLogFilesInUse. Why: No read can pin a tombstoned file again, because no read sees its row any more. The later spec text also asks for a deleter that never holds publishMu. Where: `storage/logs_manifest.go deleteTombstonedFiles; storage/logs_retention.go deleteLogs/commitDeletion`.
- **The disk limit counts only rows on local disk.** Decision: localLogHours and its tombstoning filter on WHERE local. Every row has local = true for now. Why: Only local files take up disk, and this keeps the disk limit correct once a later unit marks evicted files local = false. Where: `storage/logs_disk_limit.go`.
- **Unreadable files at backfill.** Decision: A file the backfill cannot read is logged as an error and left out of the manifest. Startup does not fail. Why: Reads of that hour already failed on it. Leaving it out lets EventStore start, and the file stays on disk for an operator. Where: `storage/logs_manifest.go backfillManifest`.
- **Columns and API names.** Decision: Schema: log_files(project_id UINTEGER, day DATE, hour UTINYINT, name VARCHAR, bytes BIGINT, min_time TIMESTAMP, max_time TIMESTAMP, log_count BIGINT, batch_max UUID NULL, local BOOLEAN, remote BOOLEAN). Tombstones: (project_id, day, hour, name, reason, deleted_at). reason is one of 'retention', 'project deleted' or 'disk limit'. LogStore.Files(), LogFiles.Hour and LogFiles.Project are removed; LogStore.HourFiles(projectID, t) replaces them. Why: 'rows' clashes with an SQL keyword, and 'logs' would read badly inside the logs catalog, so the count is log_count. LogFiles.Project had no callers left. Where: `storage/logs_manifest.go`.
- **How the upgrade test builds an install from before the manifest.** Decision: While EventStore is closed, the test opens logs.duckdb with the duckdb driver and drops the three manifest tables. This is setup only: no test asserts on manifest tables or SQL. Why: An older version's database has none of these tables, and nothing else can recreate that state. Where: `event_store/logs_manifest_test.go forgetLogFileManifest`.
- **A rename that fails after the commit.** Decision: Unchanged: the staged file waits for the next export's recovery. Until then the row points at a missing file, so reads of that hour fail instead of silently missing those Logs, as they did before. Why: A rename failing inside the same folder means a disk fault. Failing loudly seems better than missing Logs, and recovery fixes it within the hour. Where: `storage/logs_export.go exportProject`.
- **An unreadable published file at the backfill is listed with only its size.** Spec said: The spec does not say. The implementer's recorded decision was to leave such a file out of the manifest so reads would skip it, with the file staying on disk for an operator. Decision: The backfill now inserts the file's row with bytes from os.Stat and NULL min_time, max_time and log_count. Reads of that hour fail on the file, as they did before the manifest. Its name stays taken, and Retention, Project deletion and the disk limit delete it and count its bytes. If os.Stat fails, startup fails and the next start retries. Why: This replaces the earlier 'leave it out' decision; please update the Implementation decisions. I preferred this over renaming the file aside, which two findings suggested. With a row, the file behaves exactly as it did before the manifest and nothing is orphaned. A one-off read failure at the upgrade then does not hide a good file's Logs for good, and a file that really is damaged still gets deleted by Retention. Where: `event_store/storage/logs_manifest.go backfillManifest`.
- **An unreadable staged file at the backfill is renamed to <staged name>.unreadable.** Spec said: The spec does not say. Decision: The backfill cannot tell whether that file's export committed, so it renames the file out of both globs and logs an error with its new name. The file is not deleted, and startup does not fail. Why: Failing startup would block EventStore for good on a file left half-written by a crash, which is the likeliest reason a staged file can't be read. Deleting it could lose Logs if the export had in fact committed. The file stays in its hour folder until an operator deletes it, and that folder is not removed until then. Where: `event_store/storage/logs_manifest.go settleStagedFilesByTheirLogs; storage/logs_files.go unreadableSuffix`.
- **Only a Project's own pinned files fail its deletion; the disk limit fails for any.** Spec said: The spec does not say. Decision: deleteTombstonedFiles returns the files it left because they are pinned. DeleteLogDaysBefore and DeleteProjectLogs return ErrLogFilesInUse only for their own Project's files. EnforceDiskLimit still returns it for a pinned file of any Project, so Cleanup's one-minute retry keeps sweeping until the read ends. This covers a deleted Project, which LogProjects no longer lists. Why: This is the smallest change that stops one read from failing every Project's deletion. It still guarantees that someone sweeps the pinned files later. Where: `event_store/storage/logs_retention.go deleteLogs; storage/logs_disk_limit.go EnforceDiskLimit`.
- **The manifest ADR is numbered 0007.** Spec said: Phase 0 planned ADR 0007 as 'Parquet in a bucket, read by DuckDB through httpfs and tracked by a manifest'. It was never written. Decision: I wrote 0007 for the manifest part only, which is built now. The bucket and httpfs part should be added to 0007 by the object-store step (5.3), after the 5.2 probe decides on httpfs. 0006 stays free for the Monitors ADR that Phase 0 planned. Why: The manifest decision was recorded in no ADR. I didn't want to decide the bucket design before the probe has run. Where: `docs/adr/0007-log-files-listed-in-a-manifest.md`.

### B3, after_batch for Match monitors

- **SQL mode does not see batch.** Spec said: Decide whether SQL mode sees it and record the decision. Decision: The `logs` view of SQL queries keeps its documented columns, with no batch. The column is not a Field, so Queries, the field list, Log JSON and Builder chips never see it. A Query's `batch` still means a sender's Attribute named batch, which a test checks. Why: docs/logs/sql.md promises the view keeps the same columns when storage changes. batch is a storage detail for Monitors, with no meaning to users. Unrestricted SQL can still read the raw tables, as it can today. Where: `storage/logs_sql.go (unchanged viewSelect); storage/logs_duckdb.go hotTableColumns comment`.
- **from, to and cursor are optional and ignored with after_batch.** Spec said: from and to are ignored for which Logs are read. Decision: With after_batch, from and to may be left out. If sent, they are not checked. cursor is ignored. The Query compiles with Bounds{Now} only, so ago() works and a where on time in the Query still filters. Why: The spec says from and to are ignored for which Logs are read. Requiring them would force Console to send a meaningless range. Where: `handler/logs.go afterBatch`.
- **Arming also checks the Query returns Logs.** Spec said: "" validates the Query and answers {"logs": [], "ingest_cursor": C, "total": 0}. Decision: after_batch "" compiles the Query and answers 400 for a table Query, exactly as a W run does. The arm answer has the same shape as a W run, including next_cursor null and elapsed_ms. Why: A Monitor with a Query it could never run is caught when saved, not on its first run. One answer shape is simpler for Console. Where: `handler/logs.go afterBatch`.
- **Arming reads the cursor outside a read transaction.** Spec said: C is read in the same read transaction as the data (inside begin(), under the RLock, like the manifest). Decision: For "", the cursor is read with LogStore.LogProcessingCursor, not inside begin(). W runs read C inside begin(), under the RLock, in the same transaction as the data. Why: Arming reads no Logs, so there is nothing for C to be consistent with. The cursor commits atomically with its rows, so every batch at or below it is already stored. Where: `handler/logs.go afterBatch`.
- **take caps the Logs returned but not total.** Spec said: total counts every matching Log in the window. Decision: limit becomes min(limit, take). total still counts every matching Log in the window. Why: The spec says total counts every matching Log, for the 'and N more' message. Where: `handler/logs.go afterBatch`.
- **Malformed after_batch error shape.** Spec said: A malformed after_batch answers 400. Decision: 400 with {"error": "after_batch must be a Log batch id, or empty to arm a Monitor"}. Any form uuid.Parse accepts is a valid W. Why: This matches the existing parameter errors, such as an invalid cursor or limit. The {"errors": [...]} shape stays for problems with the Query itself. Where: `handler/logs.go afterBatch`.
- **When W >= C, the run reads nothing more.** Decision: If W is not below C, the read stops after reading C: no file listing and no scan of the hot table or logs_late. It answers no Logs with ingest_cursor C. That includes a W from the future, such as one saved before logs.duckdb was reset. Why: A Monitor on a quiet Project runs every minute with W == C, so this skips the scans in the common case. Answering C (not W) after a reset lets new Logs be read rather than skipped. Where: `storage/logs_read.go beginIngestedRead, storage/logs_query.go ScanLogsIngestedAfter`.
- **Total uses a second query only when needed.** Decision: total equals the number of Logs returned when it is below the limit. Otherwise a count(*) query over the same sources runs in the same transaction. Why: This avoids a window count over every column, and avoids a second scan in the usual case of few new Logs. Where: `storage/logs_query.go ScanLogsIngestedAfter`.
- **The backfill never reads batch.** Spec said: The backfill leaves it NULL for older files. Decision: readLogFileStats takes a flag. The export reads max(batch); the backfill selects NULL. Why: Files from before the manifest are also from before the batch column, so they have no batch column to read. Where: `storage/logs_manifest.go readLogFileStats`.
- **Test helper renamed.** Decision: stoppedMessage in logs_run_test.go is now onlyErrorMessage, since it also reads the new 400 answers. Why: The old name was misleading for a 400. Where: `event_store/logs_run_test.go`.
- **A read takes its promoted sets from its own transaction.** Spec said: Logs v1 spec: the promoted-set table 'is cached in memory by EventStore'. Decision: Each read loads its Project's promoted sets for the days it covers from logs.log_promoted_sets, in the same transaction as the rows. The alternative was holding publishMu or promoted.mu across Log processing's COMMIT and put, and copying the cache in begin(). I chose not to do that. Why: Holding a lock across Log processing's COMMIT would make every read wait at begin() for each commit, including any auto-checkpoint inside it. Retention's deleteBefore after a read's begin would also need the same treatment. One small query per read keeps the sets consistent by construction, with no new lock ordering. The in-memory cache (PromotedSet and PromotedSets) is still updated, but no production reader uses it any more, only tests. I kept it because the v1 spec chose it. The orchestrator may decide to drop it. Where: `event_store/storage/logs_read.go (logRead.begin, readPromotedSets, promotedSet); logs_query.go scanHour and sources`.
- **A null after_batch answers 400.** Decision: A missing after_batch gives an ordinary run. An explicit null, or any non-string value, answers 400 'after_batch must be a Log batch id, or empty to arm a Monitor'. A4 must send "" to arm a Monitor whose ingest_cursor is nil, and never null. Why: The brief says after_batch is a string and a malformed one answers 400. Treating null as absent sent Monitors on an ordinary run that never arms them. Where: `event_store/handler/logs.go parseAfterBatch and the afterBatch doc comment`.
- **How a Match monitor that keeps timing out catches up (contract for A4).** Spec said: 'No ScanBudget cut-off in this mode; timeout_seconds still applies (504).' A timed-out run records last_error and changes no state. Decision: EventStore reads the whole window (W, C] with no ScanBudget and no partial cursor. A 504 moves no cursor. A4's MonitorEvaluation must re-arm a Monitor (after_batch "") after repeated timeouts, and record that the Logs ingested in between were skipped. Why: Capping a run by batch count would need the batch boundaries between W and C. Finding them means scanning the same data, and the Log processing cursors table keeps only the latest batch. Re-arming is simple and its cost is explicit. Where: `event_store/handler/logs.go afterBatch doc comment`.
- **After_batch timeout message.** Decision: An after_batch run that times out answers 504 with 'the run was stopped after N s, its timeout: too many Logs were ingested since the last run to read in time'. Other runs keep 'try a shorter time range'. Why: A4 will likely store this text as the Monitor's last_error, and a Match monitor has no time range to shorten. Where: `event_store/handler/logs_run.go stopped(c, advice); handler/logs.go afterBatch`.

### B3b, promoted sets

- **Late Logs still in logs_late don't make a new day's keys partway.** Spec said: The unit's brief: a key counts as partway for any day that already has Logs stored without it (exported files, late-<n> files, logs_late rows, or hot rows). Decision: Only Logs in the Project's hot table or its files (manifest rows for that day) make the keys partway. Logs still in logs_late don't. Why: Reads take logs_late rows from the maps (LogColumns{late: true}), and the export pulls their values out into the columns of the set their day has then (lateRows). So they can never sit in a map where a read looks in a column. Counting them would make keys partway on most days, because a sender whose clock runs a few seconds fast puts the next day's first Log in logs_late before midnight. Every key would then be read through coalesce for the whole day, which ADR 0005 measured at 4-6 times slower than the column. The first part of the repro test, about yesterday, guards this. Where: `event_store/storage/logs_promotion.go settleNewDay; event_store/logs_promotion_test.go TestLateLogsStoredBeforeTheirDaysSetAreFoundByTheKeysItPromotes`.
- **The day's stored Logs are checked only when a new day's set gains keys.** Decision: settleNewDay runs one EXISTS query on logs.log_files and the hot table. It runs only for a day with no stored set whose set gets keys in this transaction. A day that already has a stored set keeps the old rule: every key it gains later is partway. Why: This puts the decision where the set is made and reads no Parquet footers, as the v1 spec requires. It also adds no query to the batches of a Project whose Logs carry no keys. Where: `event_store/storage/logs_promotion.go promote, add, settleNewDay`.
- **The promoted-set cache is removed, not kept for promotion.** Spec said: Logs v1 spec: the promoted-set table is cached in memory by EventStore, and columns come only from the cached promoted-set table. Decision: No copy is kept in memory. Log processing and every read take the sets from logs.log_promoted_sets in their own transaction. Why: Since 8ac6088 no reader used the cache. Promotion already read the table inside its transaction, so the cache only cost a load at startup, a lock on every commit and clearing on Retention. Please record this as a departure from the v1 spec. Where: `event_store/storage/logs_promotion.go, logs_duckdb.go, logs_retention.go, logs_disk_limit.go, logs_read.go`.
- **Tests no longer check which keys a set holds.** Spec said: Move the tests that checked the cache to check what Queries return. Decision: The tests now check that each key finds its Logs, and that the Logs read back unchanged, in the hot table, in files and after a restart. Checks of set contents were dropped: the 100-key limit, which keys the re-rank picks, AddedAt and Partway flags. Several tests were renamed to say what they check, for example TestPromotionStopsAt100KeysAndTheRestStayAttributes became TestKeysPastAFullSetStayAttributesAndEachKeyFindsItsLogs. Why: Which keys a set holds is not visible to a Query, because writes and reads both follow the stored set. What a Query can see is a write and a read that disagree, which is the kind of bug fixed here. As a result, no test now guards the 100-key limit or the re-rank's choice. Adding one would mean reading the promoted-set table, which the brief rules out, so I left it for the orchestrator to decide. Where: `event_store/logs_promotion_test.go, logs_export_test.go, logs_query_test.go, logs_retention_test.go`.
- **The storage test of a stale cache was removed.** Decision: storage/logs_read_test.go TestAReadFiltersByThePromotedSetsOfTheLogsItReads is deleted. Why: It forced a stale set into the cache to test a race between the cache and the commit. Without the cache that race can't happen. Its remaining point, that every kind of read finds a key promoted by the batch before it, is covered end to end by assertEveryReadFinds in the new tests and by the existing after_batch tests. Where: `event_store/storage/logs_read_test.go`.
- **The Builder chips and the field list are checked through what they read.** Decision: assertEveryReadFinds checks that validate gives chips for a two-stage where Query and that running it finds the same Logs. It checks the field list through the Attributes of an unfiltered run. Why: Chips come from validate, which does not touch storage. The Console builds the field list from the Logs a run returns. So what has to agree is the run of the chips' Query and the Attributes of the Logs. Where: `event_store/logs_promotion_test.go assertEveryReadFinds`.
- **Promotion rules tested on the stored sets, in package storage.** Spec said: The brief says Go tests are end-to-end and check rows and status codes, never SQL or tables. Decision: The new tests are storage-package tests. They drive AppendLogBatches and ExportLogs and read the module's own output through storedSets(), which is what the query compiler reads. They never compare SQL. Why: A Query's rows are identical whichever keys a set holds and whether a key is partway, since coalesce gives the same rows. So no end-to-end test can guard ADR 0005's cap, carry-over, re-rank or partway rules. Both findings asked for exactly this, and logs_read_test.go is prior art for storage tests. Where: `event_store/storage/logs_promotion_test.go`.
- **Hot-table branch of settleNewDay kept as a defensive check.** Decision: I kept the hot-table EXISTS, narrowed to rows with an entry in a promotable map. I didn't remove it. Why: I traced the paths: a hot Log stored while its day had no set promoted every key it could. A promotable key is left in a map only when its column name clashes in case with another column, and on the orderings I traced that key never becomes promotable while the day is hot. So the branch changes no rows today. It costs one bounded EXISTS and protects against any future path that leaves such keys in maps. For that reason no test asserts that it fires: it would only fire in a case-clash scenario that marks unrelated keys partway, which is not behaviour worth pinning. Where: `event_store/storage/logs_promotion.go settleNewDay`.
- **Decision record corrected.** Decision: TestLateLogsStoredBeforeTheirDaysSetAreFoundByTheKeysItPromotes does not guard the rule that Logs in logs_late don't make keys partway. The storage subtest 'its first Logs are late and still in logs_late' does. Why: Finding B3b-correctness-1-1 showed the round-0 claim was wrong: rows are the same either way. Where: `B3b result of round 0`.
- **Popularity counts checked as well as sets.** Decision: The tests check log_key_counts as well as log_promoted_sets, after both Retention and Project deletion, using the production reader keyCounts. Why: The spec says Retention and Project deletion delete the count rows too. Leftover counts would also keep a deleted Project listed in LogProjects, and rankedKeys would read them. The finding marked this check as optional; it costs little. Where: `event_store/storage/logs_retention_test.go`.
- **A test of what a leftover set would do.** Decision: Besides reading the stored rows, one test checks the visible result: after Retention deletes a day, the next day's set does not start from the deleted day's set. Why: It guards the previousSet carry-over the finding describes, through the promotion behaviour rather than only through table rows. Where: `TestADayAfterTheDaysRetentionDeletedStartsItsSetAfresh`.
