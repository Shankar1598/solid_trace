# 07: Count Log processing's commits on production

**Status:** ready-for-human

**What to do:** Find out whether Log processing's commits cost enough to batch them. Asked for by the user on 2026-10-22, after weighing appending to DuckDB every 5 seconds. Each ingest request wakes Log processing, which commits what is waiting (up to about 10,000 Logs a Project), so commits follow the request rate. Batching would save each commit's fixed cost, but not the hot table's checkpoints, which follow WAL bytes (idea 6), and it would delay Logs.

## Design

Once a minute, in the backlog check, Log processing logs one line per Project that stored Logs since the last check:

```
level=info message="Log processing commits" kind=log_commits project_id=123 seconds=60.002 commits=58 batches=412 logs=213004 took_ms=5321.408 slowest_ms=2310.5
```

- `commits`: transactions that stored Logs.
- `batches`: Log batches they stored, one per ingest request.
- `logs`: Logs in those batches, before the Log transform.
- `took_ms`: time spent storing them, from the append to the commit. A checkpoint DuckDB runs at a commit is included.
- `slowest_ms`: the longest of those.

Vector turns the numbers into numbers, as it does for `log_read_sql`, so a Query on the Meta Project can sum them.

## Acceptance

- [x] A root test sees a line a minute with the Project's commits, batches and Logs, and none for a minute without commits
- [x] Vector's unit test turns the line's numbers into numbers
- [ ] Once released, a few hours of `kind == "log_commits"` for `tagalys` in this ticket's Comments: commits a second, batches a second, Logs a commit, and the share of each minute spent storing (`took_ms / (seconds × 1000)`), with `slowest_ms` at the export runs
- [ ] With those and ticket 04's profiles, decide whether to add a short wait between commits (about 250 ms to 1 s) or raise `checkpoint_threshold` (idea 6)

## Comments

### Built (2026-10-23, `feature/log-commit-counts`)

`logprocessing/processor.go` counts each commit in `storeLogs` and logs the counts in `checkBacklog`, then clears them. The first check after start logs nothing, since it has no window. `TestTheCommitsLineCountsAProjectsCommitsBatchesAndLogsEachMinute` (root package), the backlog tests and `vector test` (7 tests, 0.58.0 image) pass.
