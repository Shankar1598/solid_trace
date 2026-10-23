# 12: Remove the old Log file path

**What to build:** Once production's last pre-switch day has passed Retention (30 days after the upgrade), delete the code that reads old Log files ([09](09-logs-from-before-the-switch.md)) and its tables. Delete `prepare-clickhouse`'s loading too, and the S3 adapter if nothing else uses it. An install that never prepared is told at start to upgrade through the ClickHouse release first.

**Blocked by:** 11, plus 30 days after production's upgrade

**Status:** open

- [ ] On production, no old Log file rows are left, and the bucket's `logs/` prefix is empty.
- [ ] The old Log file module, its tables (dropped by a migration step) and its tests are gone, with each hook ADR 0016's "Logs from before the switch" lists. So are `logs_parquet_path`, `SOLIDTRACE_LOG_FILE_STORE` and the Log file store mount in compose.
- [ ] EventStore with a marker but no loaded state, or with `logs.duckdb`, refuses to start and names the release to go through.
- [ ] A Console migration drops `projects.log_file_order`, which the ClickHouse release ignores but keeps so a rollback to the DuckDB release works.
- [ ] ADR 0016 and GLOSSARY.md drop the Log file store.
- [ ] Targeted tests only.
