# 08: `prepare-clickhouse` in the last DuckDB release

**What to build:** An offline command in the DuckDB EventStore. It leaves an install ready for the ClickHouse release: every Log is in a Log file, every file is uploaded, and the state the ClickHouse release needs is written as prepared files and a marker. See the [spec](../spec.md)'s "Cutover". **Build it on `feature/prepare-clickhouse` from `main`, not on `feature/clickhouse`**, since it ships in the last DuckDB release.

**Blocked by:** 01

**Status:** resolved

- [x] **`event_store prepare-clickhouse`** runs with the stack stopped, against the configured stores, and:
  1. processes every Project's waiting Log batches;
  2. exports every hot and late Log to Log files, whatever the busy rule and cutoffs say;
  3. finishes pending deletions;
  4. uploads every file when a bucket is configured, failing with a clear message if it can't;
  5. writes Parquet of the manifest rows, the promoted sets, the key counts and the Log Processing cursors to `prepared/` in the storage directory, then a marker naming the release and time.
- [x] Events and `events.duckdb` aren't touched.
- [x] Run a second time, it finds the marker and does nothing.
- [x] **The prepared files' format** (names, columns, types) is written down in this ticket's Answer. It's the contract 09 reads.
- [x] **A small fixture set**, written by the command from a test store, is committed where 09's tests can read it. Cover two Projects, a day with a key promoted partway through, a late file, and a quarter-hour file.
- [x] **Storage tests on the DuckDB side:** a store with hot, late and exported Logs, a pending deletion and waiting Log batches comes out with everything in files and the prepared files right. A second run changes nothing. With a bucket (the fake), every file is uploaded.
- [x] Targeted tests only.

## Answer

Built on `feature/prepare-clickhouse` (from `main` at `dc3665f`), head `30ebea4`. Not merged into `feature/clickhouse`.

### Boxes

- [x] **`event_store prepare-clickhouse`** with the stack stopped, against the configured stores. Steps 1–5 all run. Exit 0 on success or when already prepared, 1 on failure (nothing is marked), 2 for an unknown command. Run while the stack is up, it fails opening Pebble's lock ("…runs with the stack stopped").
- [x] **Events untouched.** logs.duckdb is attached to an in-memory DuckDB (`storage.NewLogsDuckDB`), so events.duckdb is never opened. Pebble's Events are left as they are. A test checks events.duckdb's bytes and a waiting Event.
- [x] **A second run** finds the marker and does nothing. A test checks no file changes and a Log batch stays waiting.
- [x] **Format written down** (below).
- [x] **Fixture set committed** in `event_store/testdata/prepared_clickhouse/` (below).
- [x] **Storage tests on the DuckDB side** in `event_store/logprocessing/prepare_clickhouse_test.go`, with real Pebble and DuckDB and the fake bucket (`NewDirStore`):
  - hot Logs, late Logs, exported Logs, a busy Project's quarter-hours and waiting Log batches all end up in files;
  - prepared sets, counts and cursors;
  - marker plus second run;
  - a pending local deletion is finished;
  - with a bucket, every file is uploaded and a deleted file's object is deleted;
  - a failing upload makes it fail with no marker;
  - Logs that keep failing to store make it fail rather than retry for good;
  - Events are left alone.
- [x] **Targeted tests only:** `go test -p 1 ./storage ./logprocessing` (about 20 s). No root tests.

### What the command does

`logprocessing.PrepareClickHouse` runs these steps:

1. **Marker check.** If the marker exists, it returns.
2. **Log processing.** `Processor.Prepare` then `ProcessAll`, which works like `ProcessUntilCaughtUp` but gives up after 5 failures in a row for one Project.
3. **`LogStore.PrepareForClickHouse`:**
   - exports every hot and late Log, with no time cutoff. The busy rule still picks the file shape: an hour already going by quarter-hours keeps them, and any other hour gets `data.parquet` or a late file;
   - finishes tombstoned deletions, first local bytes and then bucket objects. It fails if a tombstone is left;
   - uploads every `local AND NOT remote` file, and fails if any upload fails;
   - writes the 4 Parquet files, then the marker.

The command also:
- reads the release from `SOLIDTRACE_VERSION`, which the Dockerfile now sets from the build arg the release workflow already passes; the default is `dev`;
- spills DuckDB to `duckdb_temp_directory`, or `<logs_duckdb_path>.tmp` when that isn't set;
- applies each Project's Log transform and Log file order from Console's SQLite.

**Change to normal DuckDB-release startup:** a normal start now removes `prepared/marker.json` and logs a warning. The Logs it stores after that aren't in the prepared files, so prepare has to run again. The prepared Parquet files stay until then.

### Contract: the prepared folder

**Location:** `prepared/` next to logs.duckdb, i.e. `filepath.Dir(logs_duckdb_path)/prepared`. Production: `/storage/duckdb/production/prepared/`. Use `storage.PreparedDir(path)`.

**Common types:**
- Types are DuckDB's as written to Parquet: UINTEGER→UINT32, UTINYINT→UINT8, DATE, TIMESTAMP is µs with no zone but always UTC, BIGINT, VARCHAR, BOOLEAN.
- UUIDs are written as text, so no Parquet UUID type is involved.
- Rows are sorted as listed.

**`log_files.parquet`** has one row per Log file in the manifest. Sort: project_id, day, hour, name.

| column | type | meaning |
|---|---|---|
| project_id | UINTEGER | |
| day | DATE | UTC day |
| hour | UTINYINT | UTC hour, 0–23 |
| name | VARCHAR | `data.parquet`, `data-00/15/30/45.parquet` (busy quarter-hour), `late-<n>.parquet` |
| key | VARCHAR | `project=<id>/day=<YYYY-MM-DD>/hour=<HH>/<name>` |
| bytes | BIGINT | |
| min_time, max_time | TIMESTAMP | NULL only for an unreadable file listed by the old backfill |
| log_count | BIGINT | NULL like min_time |
| batch_max | VARCHAR (UUID text) | highest Log batch id in the file; NULL for files from before the batch column |
| local | BOOLEAN | a local copy exists at `<logs_parquet_path>/<key>` |
| remote | BOOLEAN | the object exists at `<object_store.prefix>logs/<key>`; always true after prepare when a bucket is configured |
| generation | BIGINT | unique per row; the DuckDB bin cache's |

**`log_promoted_sets.parquet`** has one row per (Project, day, type, key). Sort: project_id, day, type, key.

| column | type | meaning |
|---|---|---|
| project_id | UINTEGER | |
| day | DATE | |
| type | VARCHAR | `string`, `number` or `bool` |
| key | VARCHAR | |
| column | VARCHAR | the column name in that day's files: `attrs_<type>.<key>` |
| added_at | TIMESTAMP | |
| partway | BOOLEAN | true means read `coalesce(column, map[key])` on that day |

**`log_key_counts.parquet`** has one row per (Project, day, type, key), with columns project_id UINTEGER, day DATE, type VARCHAR (string/number/bool/json), key VARCHAR, logs BIGINT. It is a top-K list per Project and day. Sort: project_id, day, type, key.

**`log_processing_cursors.parquet`** has columns project_id UINTEGER and batch_id VARCHAR (UUID text of the last Log batch stored). Sort: project_id.

**`marker.json`** looks like `{"release":"v0.9.1","prepared_at":"2026-10-02T00:30:00Z"}` (RFC 3339, UTC). It is written last, through a temporary file and a rename. Read it with `storage.ReadClickHouseMarker(dir)`.

### The old Log files 09 reads in place

**Fixed columns:** id UUID, time TIMESTAMP, level, message, service, environment, trace_id, span_id (VARCHAR), batch UUID, and four maps:
- attrs_string MAP(VARCHAR,VARCHAR)
- attrs_number MAP(VARCHAR,DOUBLE)
- attrs_bool MAP(VARCHAR,BOOLEAN)
- attrs_json MAP(VARCHAR,VARCHAR), whose values are JSON text

**Promoted columns** come after the fixed ones, one for each key in the day's set at the time of export, named like `attrs_string.user`.
- A file written before a key was promoted lacks that column, so read it as NULL.
- Files from before the batch column lack `batch`.
- In late files, values of keys promoted that day are pulled into their columns.

**Paths look hive-partitioned** (`project=…/day=…/hour=…`). DuckDB adds `project`, `day` and `hour` columns unless `hive_partitioning = false`. ClickHouse may do the same through `use_hive_partitioning`, so turn it off.

### Fixtures: `event_store/testdata/prepared_clickhouse/` (188 KB)

**Contents:**
- `prepared/`: the 4 Parquet files and the marker. Release `fixture`, prepared_at `2026-10-02T00:30:00Z`, which is the switch time.
- `logs_parquet/`: the local copies, laid out as under logs_parquet_path.
- `bucket/logs/…`: the fake bucket's objects, every file.
- `logs.ndjson`: 12 Logs, one per line, as `{"project_id", "id", "time", "level", "message", "service", "environment", "attributes":{"string","number","bool","json"}}`. Ids are fixed UUIDv7s of each Log's time.

**Project 3 (busy):**
- 2026-10-01 hour 10 has `data-00.parquet` (quarter-hour), `late-1.parquet` (a 10:05 Log that came after it) and `data-15.parquet`.
- That day's set is cached (bool), duration_ms (number) and user (string), plus plan, which was added partway.
- 2026-10-02 hour 00 is `data.parquet`. Its set started from the day before, with nothing partway.
- There is a JSON Attribute `tags`.

**Project 2 (quiet):**
- 2026-10-01 hours 07 (a late Log) and 09 are bucket-only (local=false).
- 2026-10-01 hour 10 and 2026-10-02 hour 00 are local and remote.

**Regenerate** from `event_store/`:

```
go test ./logprocessing -run TestTheCommittedFixtureSetIsWhatPrepareWrites -update-fixtures
```

Without the flag, the same test checks that the committed set still matches what the command writes. It compares columns, types, rows and the marker, but not cursors, which hold random Pebble batch ids.

### For 09

**Fixture details:**
- The fixture Projects are ids 2 and 3. Insert them into Console's test DB as the root tests do.
- Cursors for Projects 2 and 3 are Pebble batch ids, UUIDv7s from 2026-10-01 and 02.

**`deploy/install`:**
- Run the previous image's `./event_store prepare-clickhouse` (WORKDIR `/app`) with event_store's volumes, config and bucket env.
- event_store's compose entry has `pull_policy: never`, so pull the old tag explicitly.
- If the DuckDB release starts again before the switch, the marker goes and install must prepare again.

**Old files:**
- After prepare with a bucket, local copies stay where eviction hasn't removed them, and nothing in the ClickHouse release evicts them. Read the local copy when `local`, else the object.
- Files of Projects deleted from Console are still listed. Project deletion in 09 removes them.

**Failure modes:**
- A local file that was lost before upload makes prepare fail; the error names the file.
- The failing-delete path, where a tombstone is left, has a guard but no test.

### Not done

- `deploy/install`, README and docs are left to 09/12. No root tests were run.
