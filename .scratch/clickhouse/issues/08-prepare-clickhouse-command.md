# 08: `prepare-clickhouse` in the last DuckDB release

**What to build:** An offline command in the DuckDB EventStore. It leaves an install ready for the ClickHouse release: every Log is in a Log file, every file is uploaded, and the state the ClickHouse release needs is written as prepared files and a marker. See the [spec](../spec.md)'s "Cutover". **Build it on `feature/prepare-clickhouse` from `main`, not on `feature/clickhouse`**, since it ships in the last DuckDB release.

**Blocked by:** 01

**Status:** ready-for-agent

- [ ] **`event_store prepare-clickhouse`** runs with the stack stopped, against the configured stores, and:
  1. processes every Project's waiting Log batches;
  2. exports every hot and late Log to Log files, whatever the busy rule and cutoffs say;
  3. finishes pending deletions;
  4. uploads every file when a bucket is configured, failing with a clear message if it can't;
  5. writes Parquet of the manifest rows, the promoted sets, the key counts and the Log Processing cursors to `prepared/` in the storage directory, then a marker naming the release and time.
- [ ] Events and `events.duckdb` aren't touched.
- [ ] Run a second time, it finds the marker and does nothing.
- [ ] **The prepared files' format** (names, columns, types) is written down in this ticket's Answer. It's the contract 09 reads.
- [ ] **A small fixture set**, written by the command from a test store, is committed where 09's tests can read it. Cover two Projects, a day with a key promoted partway through, a late file, and a quarter-hour file.
- [ ] **Storage tests on the DuckDB side:** a store with hot, late and exported Logs, a pending deletion and waiting Log batches comes out with everything in files and the prepared files right. A second run changes nothing. With a bucket (the fake), every file is uploaded.
- [ ] Targeted tests only.
