# 10: DuckDB out; the ADR, the glossary, the docs and Console's Log file order

**What to build:** The branch's last code ticket. Nothing in EventStore or Console depends on DuckDB any more, the design is recorded, and Console no longer offers the Log file order. See the [spec](../spec.md)'s "What goes" and "Domain words and records".

**Blocked by:** 03, 04, 05, 06, 07, 09

**Status:** ready-for-agent

- [ ] **EventStore:**
  - duckdb-go leaves `go.mod`;
  - the DuckDB writer, `LogStore`'s DuckDB parts, the export, the manifest's DuckDB tables and httpfs are deleted;
  - so are the DuckDB benches (`logslayouts`, `logexport`, `logreads`, `objectstore`), `duckdb_engine_test.go`, `duckdb_bucket_test.go` and `pkg/sqltext`'s DuckDB quoting (ClickHouse quoting if it's still needed).
- [ ] **Build and deploy:**
  - the Dockerfile's extensions stage and `DUCKDB_VERSION` go;
  - the `duckdb_*` keys, `logs_duckdb_path` and `parquet_storage_path` are ignored with a warning;
  - compose's 30 s checkpoint grace and DuckDB comments go;
  - `deploy/event_store.yml` and `deploy/install` lose DuckDB.
- [ ] **Console:**
  - the Log file order card goes from Project settings;
  - a migration removes `projects.log_file_order`;
  - `list_projects` drops `log_file_order`;
  - their Rails tests, vitest and flows follow.
- [ ] **ADR 0016**, Logs and Events in ClickHouse, with the probe's numbers, the options weighed and the SQL user mechanism. It supersedes 0004, 0005, 0007 and 0013 and ADR 0008's Log file and Event archive parts. It notes the amendment 07 made to 0003. Each superseded ADR gets a status line.
- [ ] **GLOSSARY.md, `README.md`, `docs/logs/object-store.md`** and the docs under `docs/` follow the spec's word list.
- [ ] **Searches:** `grep -ri duckdb` over `event_store/`, `console/app`, `deploy/` and `docs/` finds only history (ADRs) and the old-Log-file module's comments.
- [ ] **The release check, which needs the user:** the Go suite and Playwright in parallel, then Rails, as `docs/agents/testing.md` says. Leave it for the user and say so in the Answer.
