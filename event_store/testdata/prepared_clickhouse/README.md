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
