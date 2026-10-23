# prepared_clickhouse

An install prepared by `event_store prepare-clickhouse`, for the tests of
Logs from before the switch (ticket 09). It is static here: the command ships
only in the last DuckDB release, so it lives on `feature/prepare-clickhouse`
and `main`, not on this branch.

To regenerate it, on `feature/prepare-clickhouse` or `main`, from
`event_store/`:

```
go test ./logprocessing -run TestTheCommittedFixtureSetIsWhatPrepareWrites -update-fixtures
```

then copy this folder over. The prepared files' format and what the set holds
are in the Answer of `.scratch/clickhouse/issues/08-prepare-clickhouse-command.md`.

`order placed`'s id, `01a0f6e8-ff60-7000-9000-000000000001`, was edited in by
hand (in `logs.ndjson`, both copies of its `data-00.parquet`, and that file's
`bytes` in `prepared/log_files.parquet`). Its file's `id` statistics are then
in byte order but not in ClickHouse's UUID order, as real UUIDv7 ids are, which
ClickHouse rejects when a filter on `id` reads them as UUIDs. Edit it in again
after regenerating.
