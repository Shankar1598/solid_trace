# 10: DuckDB out; the ADR, the glossary, the docs and Console's Log file order

**What to build:** The branch's last code ticket. Nothing in EventStore or Console depends on DuckDB any more, the design is recorded, and Console no longer offers the Log file order. See the [spec](../spec.md)'s "What goes" and "Domain words and records".

**Blocked by:** 03, 04, 05, 06, 07, 09

**Status:** resolved

- [x] **EventStore:**
  - duckdb-go leaves `go.mod`;
  - the DuckDB writer, `LogStore`'s DuckDB parts, the export, the manifest's DuckDB tables and httpfs are deleted;
  - so are the DuckDB benches (`logslayouts`, `logexport`, `logreads`, `objectstore`), `duckdb_engine_test.go`, `duckdb_bucket_test.go` and `pkg/sqltext`'s DuckDB quoting (ClickHouse quoting if it's still needed).
- [x] **Build and deploy:**
  - the Dockerfile's extensions stage and `DUCKDB_VERSION` go;
  - the `duckdb_*` keys, `logs_duckdb_path` and `parquet_storage_path` are ignored with a warning;
  - compose's 30 s checkpoint grace and DuckDB comments go;
  - `deploy/event_store.yml` and `deploy/install` lose DuckDB.
- [x] **Console:**
  - the Log file order card goes from Project settings;
  - ~~a migration removes `projects.log_file_order`~~ `projects.log_file_order` stays, ignored, so that a rollback to the DuckDB release works ([12](12-remove-the-old-log-file-path.md) drops it; amended after the code review);
  - `list_projects` drops `log_file_order`;
  - their Rails tests, vitest and flows follow.
- [x] **ADR 0016**, Logs and Events in ClickHouse, with the probe's numbers, the options weighed and the SQL user mechanism. It supersedes 0004, 0005, 0007 and 0013 and ADR 0008's Log file and Event archive parts. It notes the amendment 07 made to 0003. Each superseded ADR gets a status line.
- [x] **GLOSSARY.md, `README.md`, `docs/logs/object-store.md`** and the docs under `docs/` follow the spec's word list.
- [x] **Searches:** `grep -ri duckdb` over `event_store/`, `console/app`, `deploy/` and `docs/` finds only history (ADRs) and the old-Log-file module's comments.
- [ ] **The release check, which needs the user:** the Go suite and Playwright in parallel, then Rails, as `docs/agents/testing.md` says. Leave it for the user and say so in the Answer.

## Comments

- 2026-10-23, from tickets 05 and 07 (details in `~/.cache/solid-trace/clickhouse-notes/05-notes.md` and `07-notes.md`):
  - **JSON decoding:** clickhouse-go 2.49 can't decode a `JSON` column that 26.8 sends after a sort merges several parts ("unsupported column type", "unexpected value 116 for boolean"). The SQL users' profile sets `output_format_native_use_flattened_dynamic_and_json_serialization = 1`; EventStore's own connection doesn't. Set it there too, with a root test that reads `attributes` sorted across several parts with no `LIMIT`.
  - **A deleted Project's SQL user** (and its row policies) is dropped with the Project's data.
  - **SQL queries are on by default today** (`instance_settings.sql_queries_enabled` defaults to true), not off as the spec says. 07 left the default alone; the user decides whether to change it.

## Answer

Built on `clickhouse/10-duckdb-out`, from `feature/clickhouse` at 17091ea, which hadn't moved when it finished.

**EventStore**
- duckdb-go is out of `go.mod`. Deleted with their tests:
  - the DuckDB writer and all of `LogStore`: hot tables, `logs_late`, promotion, the export, the manifest, uploads, eviction, DuckDB's Retention and disk limit, and its bin cache;
  - httpfs, the Log file order reader and `pkg/sqltext` (`pkg/chsql` is ClickHouse's quoting);
  - the benches `logslayouts`, `logexport` and `objectstore` (`logreads` had gone with 03);
  - the S3 adapter's `URL` and its folder store, which only DuckDB used.
- **Kept for the old-Log-file module:**
  - the key counts' helpers (`AttributeType`, `typedKey`, `logKeys`, `mergeKeyCounts`), `LogDay`, `dayString`, `latestTime` and `earliestTime`, now in `storage/logs_keys.go`;
  - `logObjectPrefix`, moved into `logs_old_files.go`;
  - the S3 adapter.
- `/health` checks Pebble and ClickHouse only.
- **JSON decoding:** EventStore's own connection sets `output_format_native_use_flattened_dynamic_and_json_serialization = 1`. Root test `TestEventStoresConnectionReadsAttributesSortedAcrossSeveralParts` reads `attributes` over four parts, sorted, with no `LIMIT`. Without the setting it failed with "unexpected packet 9".
- **SQL users:** `DeleteProjectLogs` drops the deleted Project's SQL user and its two row policies. Root test `TestADeletedProjectsSQLUserGoesWithItsLogs` checks that the other Project's SQL still runs. A deleted Project with an SQL user but no Logs left is never found by Cleanup, so its user stays; it's harmless, as 07 found.

**Build and deploy**
- The Dockerfile loses its extensions stage and `DUCKDB_VERSION`.
- Compose loses the 30 s stop grace and its DuckDB comments.
- `deploy/install` no longer sets `duckdb_memory_limit`.
- **Config keys:** `duckdb_path`, `logs_duckdb_path`, `parquet_storage_path` and the three `duckdb_*` keys load with one warning each (`config.Config.Warnings`, which `main` logs). `duckdb_path` and `logs_duckdb_path` still say where the DuckDB release's `logs.duckdb` and Log files are, for the start's check: the warning says so.
- **Finding logs.duckdb:** without either key, it is looked for beside `logs_parquet_path`. `deploy/event_store.yml` now names `logs_parquet_path: /storage/duckdb/production/logs_parquet` and drops every DuckDB key. `deploy/install`'s `prepare_duckdb_logs` looks in the same place.
- `setup-gcp`'s storage prompts now speak of older Logs.

**Console**
- The Log file order card, its lib and vitest, and the `project-log-file-order` flow are deleted.
- `projects.log_file_order` stays, and `Project` ignores it (`ignored_columns`): the DuckDB release reads it, so dropping it would break a rollback. The migration that dropped it (20261023090000) was removed after the code review, and `models/project_test.rb` checks that the column stays and isn't read. Ticket 12 drops it.
- `list_projects` answers no `log_file_order`. The MCP instructions and the run, add-chart and monitor tools point at `service` and `file` instead.
- The language reference's "Reading less" is rewritten for ClickHouse, in both copies.

**Records:** ADR 0016 has the research's and the probe's numbers, the five options weighed and the SQL users, and notes 07's amendment to 0003. Status lines were added to 0004, 0005, 0007, 0008 and 0013, and ADR 0006's Match monitor note was updated. GLOSSARY, README (with an upgrade paragraph), `docs/logs/object-store.md` (now "Logs in a bucket"), `docs/mcp.md`, `docs/logs/monitors.md` and the docs index follow.

**Search:** `grep -ri duckdb` over `event_store/`, `console/app`, `deploy/` and `docs/` finds:
- the ADRs;
- the old-Log-file module and its hooks: `logs_old_files.go`, its test, `main.go`, `integration_test.go`, `clickhousetest/bucket.go` and `deploy/install`'s `prepare_duckdb_logs`;
- the paths and comments of the Log file store (`/storage/duckdb/production/logs_parquet`) in `deploy/event_store.yml` and `docker-compose.yml`;
- the ignored keys in `config`;
- the fixture README;
- a scratch path in `bench/pebblevalsep`.

**Tests run** (targeted): `go test -p 1` of `config`, `storage`, `handler`, `ingest`, `processing` and `pkg/...`, `go vet ./...`, and about 3.5 min of root tests (health, old Log files and the start checks, SQL, Retention and deletion, bucket, Events, processing, Fields, newest page, body). Rails: `projects_controller_test`, `mcp/context_tools_test`, `mcp/logs_tools_test` and `models/project_test` (55 runs). Also typecheck and vitest (441). No Playwright flow was run.

**Left for the user: the release check.**
1. Start the dev ClickHouse: `bin/clickhouse-dev`.
2. With the dev stack stopped, ready the main checkout's DuckDB dev data, which has no marker, so EventStore won't start on it. Either:
   - prepare it: in the main checkout, check out `feature/prepare-clickhouse`, then run `cd event_store && vrl/build && go run . prepare-clickhouse`, then check out the release commit again; or
   - move it aside: `mv storage/duckdb/development/logs.duckdb storage/duckdb/development/logs.duckdb.aside`. Its hot Logs then aren't read.
3. On the release commit, run `bin/rails db:migrate` in `console/`. Then start the dev stack's EventStore and wait until it answers.
4. Run the Go suite (`vrl/build && go test -p 1 -count=1 -timeout 60m $(go list ./... | grep -v /bench/)`, with MinIO up for the bucket tests) and Playwright (`bun run e2e --reporter=line`) in parallel.
5. Once Go is done, run Rails: `RAILS_ENV=test bin/rails db:test:prepare && bin/rails test`.

**Flows to watch:** `project-log-file-order` is deleted. These pages or reads changed:
- `project-keys`, `project-delete` and `log-transform` (the Project settings page);
- `logs-v1-sql` and the other `logs-v1-*` flows, `charts`, `dashboards` and `monitors` (reads on ClickHouse);
- `issues` (Events).

**Not done:**
- SQL queries stay on by default, as 07 left them; it's the user's call.
- A full suite or flow run.
