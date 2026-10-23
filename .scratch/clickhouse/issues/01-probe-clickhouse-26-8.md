# 01: Probe: ClickHouse 26.8 with JSON Attributes, on a production quarter-hour

**What to build:** A probe, not product code. It settles the spec's open technical choices on real data before anything is built on them, and it gates the effort. It runs ClickHouse 26.8 LTS against the same production quarter-hour of `tagalys` as the [research](../../eventstore-performance/research/clickhouse-vs-duckdb.md), with the `logs` table the [spec](../spec.md) describes: JSON Attributes, a `file` sort column, `PARTITION BY (project_id, toDate(time))`, `ORDER BY (project_id, service, file, time)`. The results are compared with the research's figures for the same reads.

**Blocked by:** None (can start immediately)

**Status:** ready-for-agent

- [ ] **Data.** Copy one `tagalys` quarter-hour off production again (`~/Tagalys/apps/api/ssh 10.164.0.2`, as on 2026-10-23), and delete it afterwards. Check the user's OK under Comments before copying. The dev box has about 9.5 GB free, so keep only this file and ClickHouse's copy.
- [ ] **Setup.** Pin `clickhouse/clickhouse-server` to an exact 26.8 LTS patch. Run it on CPUs 2,3 with 3 GB, as the research did. Scripts and results go in `~/.cache/solid-trace/clickhouse-probe/`, and no run takes more than a minute.
- [ ] **JSON against promoted columns.** Load the file into the spec's table. Attributes go into `JSON` from the file's promoted columns and maps; array/JSON values go into the separate map. Run the research's 14 slow reads and Dashboard 1's 16 Charts, hand-written as in `~/.cache/solid-trace/prod-quarter/hand_sql.py`, with Attributes read through JSON paths. Record CPU and best-of-3 times next to the research's MergeTree-with-108-columns figures.
- [ ] **Typed values.** For the language's non-coercing `==`, check how a path holding both strings and numbers compares, how `tostring`/`tonumber` read it, and how a missing path reads.
- [ ] **Sort column.** Check whether a typed JSON hint for `file` can be in `ORDER BY`, and how a plain `file` column compares. Pick one.
- [ ] **`search`.** Time ways to match `message` and every string Attribute's *value* (never key names) on JSON. Pick one.
- [ ] **Newest page.** Time the two-step read (times and ids, then rows by id) against one step with lazy materialization up to 200 rows, with no filter and with a common filter.
- [ ] **Histogram MV.** Count by Project, minute and level from a summing MV against from the table.
- [ ] **Inserts while reading.** Insert about 3,600 Logs a second for under a minute, in one-second inserts as Log processing will, while a read loop runs. Record:
  - insert CPU per Log, against the research's 15 µs;
  - parts and merges;
  - read times during ingest.

  Then try the merge pool and `max_threads` for 2 CPUs.
- [ ] **Dedup.** Re-send an insert with the same token under `non_replicated_deduplication_window`, including one that spans two day partitions, and check it's dropped.
- [ ] **Old Log files.** The production quarter-hour is a DuckDB-written file. Read it with `file()` (and, if MinIO is up, `s3()`) as one source with a second file that has fewer promoted columns. Time a promoted-key filter with its map fallback (`coalesce(col, map[key])`) against DuckDB's time on the same file. Check that the MAP, UUID and timestamp columns read correctly.
- [ ] **Quantiles.** Note which ClickHouse quantile function matches DuckDB's `approx_quantile` closely enough for the root tests' expected values.
- [ ] **Breaking changes.** Read the 26.4–26.8 breaking changes for anything that touches the spec: JSON, S3 disks, row policies, settings constraints, lazy materialization, `clickhouse-go`.
- [ ] **Gate.** Pass if JSON paths cost no more than 1.5× the promoted columns' CPU on the common-key reads and the Charts, and inserts keep up at 3,600 Logs a second on 2 CPUs. If it passes, record the choices above under Answer for the later tickets. If it fails, stop and hand the table layout back to the user with the numbers (a table per Project with promoted columns, or typed `Map`s). Build nothing further.

## Comments

- 2026-10-23, planning: copying a production quarter-hour needs the user's OK, as for the research. Asked in the planning session.
