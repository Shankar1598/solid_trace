# Logs and Events in ClickHouse

**Status:** ready-for-agent

Planned with the user on 2026-10-23, after the ClickHouse against DuckDB probe ([research](../eventstore-performance/research/clickhouse-vs-duckdb.md)). EventStore keeps Logs and Events in a ClickHouse server, and DuckDB leaves EventStore. Code map at `e900281`: `~/.cache/solid-trace/clickhouse-notes/code-map.md`.

## Problem Statement

A member who opens a Chart, a Dashboard or a search over a busy Project pays for it in CPU on a small VM. On a production quarter-hour of `tagalys`, the 14 slowest Queries of a week used about twice ClickHouse's CPU on DuckDB, and a Dashboard refresh two to three times as much. A filter that combines the sort columns with `or` took 780 ms, against 4 ms. A search of `message` is 1.5 to 3.5 times slower. A Dashboard or Monitor that runs the same Query every 15 seconds pays the full price each time.

Each speed-up so far has added machinery around DuckDB's limits, and every member and operator lives with its edges:
- a hot table per Project that fills in arrival order and can't be sorted;
- a Log export every quarter-hour, with late Logs, late files and a shared late table;
- up to 100 promoted Attribute columns per Project per day, re-ranked daily;
- a manifest of Log files, with pins, tombstones and generations;
- uploads and eviction of Log files;
- a per-Project Log file order.

A late Log is read from maps until the next export, and a member sees the export in Log processing's timing. An SQL query can't be isolated on DuckDB ([ADR 0003](../../docs/adr/0003-unrestricted-sql-queries-behind-an-instance-setting.md)), so it stays off and is only checked by a narrow parse.

Events sit in a second DuckDB database with a nightly archive to Parquet, so EventStore runs two engines' worth of storage code for one store.

## Solution

EventStore keeps Logs and Events in ClickHouse, a server that runs beside it in the compose stack.
- **Logs** go into one table, sorted by Project, Service, the `file` Attribute and time. The engine stores each Attribute as its own typed column, with no promotion.
- **Recent data** stays on the VM's disk; older data moves to the install's bucket.
- **Ingest is unchanged:** a `200` still means the Logs or Event are in Pebble. Log processing and Event processing move them into ClickHouse about once a second.
- **Queries answer the same, faster and with less CPU.** A filter on a Project's Service and file reads only those Logs whatever the range, and a Query repeated by a Dashboard or Monitor skips work it has already done.
- **SQL queries** become ClickHouse SQL, run as a database user that can only see its own Project.
- **Logs stored before the upgrade** stay in their Parquet files, on disk or in the bucket, and are read there until Retention deletes them.
- **Upgrading** is one `deploy/install` run. DuckDB is gone from EventStore afterwards.

## User Stories

1. As a member viewing a Dashboard over a busy Project, I want a Chart filtered on Service and file to read only those Logs over its whole range, so that a 6-hour or 7-day Dashboard loads in seconds, not tens of seconds.
2. As a member, I want a Chart that filters on sort columns joined with `or` (region and file, say) to answer in milliseconds, so that the Queries I write naturally aren't the slow ones.
3. As a member, I want `message contains` and `search` to be at least as fast as now on every range, so that searching isn't slower anywhere.
4. As a member whose Dashboard refreshes every 15 seconds, I want a repeated Query to skip filtering it has already done, so that a Dashboard left open doesn't hold the VM's CPUs.
5. As a member, I want the Logs explorer's histogram over any range with no condition to appear at once, so that opening the explorer on 24 hours or 7 days doesn't scan billions of Logs.
6. As a member, I want a Chart I reopen to read only the bins it hasn't seen, so that a long-range Dashboard opened again is fast.
7. As a member, I want the Logs explorer's newest page to stay about as fast as it is now, so that the change doesn't trade one slow read for another.
8. As a member, I want a Log that arrives late to be found by every filter at once, so that I never miss a Log because it came after its neighbours.
9. As a member, I want every Query to list the same Logs, in the same order, as it does now, so that the change is invisible except for speed.
10. As a member, I want Attribute filters to keep their typed meaning, so that `status == 200` still matches the number and `status == "200"` the string.
11. As a member, I want a Field used by only a few Logs to be as fast to filter on as a common one, so that rare keys aren't second-class.
12. As a member, I want `list_fields` and the Logs explorer's Fields to count every Log, late ones included, so that a Field's popularity is right.
13. As a member, I want a Log opened by its id (`get_log`, a link) to open quickly whether or not I give its time, so that links and agents can fetch Logs cheaply.
14. As a member using a Match monitor, I want each run to see exactly the Logs ingested since the last one, so that nothing is sent twice or missed.
15. As a member using a Threshold monitor, I want its runs to cost little, since they repeat the same Query, so that Monitors don't slow the explorer.
16. As a member, I want Queries over hours from before the upgrade to answer as they do today, so that the switch doesn't open a gap in my Logs.
17. As a member, I want a Dashboard over 7 days to be no slower during the month after the upgrade, while part of its range is still in old Log files, so that the upgrade is never a step back.
18. As a member, I want an Issue's Events, its previous and next Event, and the counts behind integration thresholds to work as now, so that error tracking is unchanged.
19. As an admin on an install with SQL queries turned on, I want an SQL query to see only its own Project's Logs and Events, whatever its text, so that one Organization can't read another's.
20. As an admin, I want an SQL query that tries to read a file, a URL, the bucket, a system table or another Project to be refused by the database, so that SQL needs no text checks to be safe.
21. As an admin, I want a runaway SQL query to stop at a time and memory limit, so that one member's SQL can't stall ingest or other members.
22. As a member writing SQL, I want the `logs` and `events` tables to show the Project and time range chosen outside the text, as the views do now, so that my SQL stays short.
23. As a member writing SQL, I want to read an Attribute by its name (`attributes.http.status`), so that SQL is as direct as a Query.
24. As a member writing SQL, I want a syntax error to point at where it went wrong, as the explorer does now, so that I can fix my SQL quickly.
25. As an agent using `run_sql`, I want its description and the SQL docs to describe ClickHouse SQL and the tables' columns, so that I write SQL that runs.
26. As an operator, I want Log processing never to stop for an export, so that it no longer falls behind every quarter-hour.
27. As an operator, I want ingest to keep answering `200` while ClickHouse restarts or is busy merging, so that shippers never see errors from storage.
28. As an operator, I want a crash at any point to store each Log and Event exactly once, so that counts and Monitors stay right.
29. As an operator, I want recent Logs on local disk and older ones in my bucket, as now, so that VM disk holds about a day and the bucket holds the rest.
30. As an operator, I want the bucket to be the same S3 or GCS bucket, reached with the same key, so that the upgrade needs no new cloud setup.
31. As an operator without a bucket, I want everything on local disk, as now, so that a bucket stays optional.
32. As an operator, I want Retention to delete a Project's days whole and at once, so that it frees disk and bucket space immediately and never lingers.
33. As an operator, I want a deleted Project's Logs and Events gone from ClickHouse, the bucket and the old Log files, so that deletion is complete.
34. As an operator, I want the disk limit to move data to the bucket before it deletes anything, and to log an error when it has to delete, so that a full disk never stops ingest.
35. As an operator, I want ClickHouse's memory set from the VM's RAM by `deploy/install`, as DuckDB's is now, so that it fits a 2 vCPU / 15 GiB VM beside Console and Vector.
36. As an operator, I want ClickHouse to listen only where EventStore can reach it, with a password I never have to type, so that it isn't a new door into the VM.
37. As an operator, I want one `deploy/install` run to upgrade my install, preparing the old data with the previous release when it finds it, so that I don't have to step through releases by hand.
38. As an operator, I want EventStore to refuse to start, saying why, if it finds DuckDB data that was never prepared, so that no Logs are silently left behind.
39. As an operator, I want the upgrade to copy no Logs, so that it takes minutes, not hours, on a 2 vCPU VM.
40. As an operator, I want the old Log files left in place and deleted only by Retention, so that nothing is lost and I don't have to clean up.
41. As an operator, I want EventStore's own Logs to show insert counts and times, as `log_commits` does now, so that I can see how ingest keeps up.
42. As an operator, I want the same `/health` check to tell me when ClickHouse is unreachable, so that monitoring keeps working.
43. As an operator, I want the image to be smaller and simpler, with no DuckDB extension to keep in step, so that releases have fewer ways to break.
44. As an operator, I want `event_store.yml`'s DuckDB keys ignored with a warning, not a failed start, so that an old config still boots.
45. As an admin, I want the Project settings page to drop the Log file order card, since every Project is sorted by Service and file, so that the page doesn't offer a setting that does nothing.
46. As a developer, I want one ClickHouse container to serve the dev stack and the Go tests, with a database per test package and worktree, so that the 11 GB dev box and parallel agents cope.
47. As a developer, I want the existing root tests to be the spec of the new storage, unchanged in what they assert, so that the rewrite is held to today's behaviour.
48. As a developer, I want the Query compiler to emit one dialect, so that a language change touches one place.
49. As a developer, I want the old-Log-file read path to be separate and deletable, so that it can go once the last old day passes Retention.
50. As a developer, I want a probe to settle the JSON type before anything is built on it, so that the effort stops early if it's slower than promoted columns.

## Implementation Decisions

### Order of work

- **A probe comes first and gates the rest** ([01](issues/01-probe-clickhouse-26-8.md)). It runs ClickHouse 26.8 LTS on a production quarter-hour of `tagalys`. If the JSON type is clearly slower than promoted columns for common keys, the user chooses between a table per Project with promoted columns and typed `Map`s, with the numbers, before anything else is built.
- **Branches.** The effort builds on `feature/clickhouse`. The `prepare-clickhouse` command (see Cutover) builds on its own branch from `main`, since it ships in the last DuckDB release. Implementers work in worktrees made from `main` and reset to the ticket's base branch.

### ClickHouse beside EventStore

- **The server:** a ClickHouse server container in the compose stack, pinned to an exact 26.8 LTS patch release. It's not embedded (chDB), so merges run outside EventStore's process and SQL queries get real users. EventStore talks to it with `clickhouse-go` over the native protocol.
- **Networking:**
  - ClickHouse listens on loopback only.
  - The default user is limited to localhost, and EventStore's user has a password from `SOLIDTRACE_CLICKHOUSE_PASSWORD`, which `deploy/install` generates.
  - System tables need a grant (`select_from_system_db_requires_grant`, and the same for `information_schema`).
- **Memory and threads:** `deploy/install` gives ClickHouse the share of RAM DuckDB gets now (half), through `max_server_memory_usage`. Never cap it with a container memory limit: under a cgroup limit, 26.8 returns freed pages at once and scans run about 1.9× slower ([probe](issues/01-probe-clickhouse-26-8.md)). `max_threads` and the merge pool keep 26.8's defaults, which the probe found ample on 2 CPUs.
- **The version:** 26.8.34.13 was probed. It has a bug that skips a boundary second when `time` is compared with a DateTime or an integer (#123999), so the compiler compares `time` only with `toDateTime64(…)`. Re-pin to a 26.8.35 patch once one has been out a few days.
- **System logs:** `query_log` stays on, with a TTL of a few days. The other system log tables are off, as in the probe.
- **Config:** `event_store.yml` gains a `clickhouse` section (address, database, user). `object_store` keeps its keys, and `local_cache_hours` keeps its meaning. The `duckdb_*` keys, `logs_duckdb_path` and `parquet_storage_path` are ignored with a startup warning. `logs_parquet_path` stays while old Log files exist.
- **Dev and tests:** one container started by `bin/clickhouse-dev`, capped at about 2 GB by `max_server_memory_usage`, serves the dev stack and the Go tests. Each test package gets its own database, named after the package and the worktree, and the tests make and drop it. `docs/agents/testing.md` lists `bin/clickhouse-dev` as a prerequisite of the Go suite and the Playwright flows.

### The `logs` table

- **One shared table:**
  - `PARTITION BY (project_id, toDate(time))`, so no part mixes Projects or days;
  - `ORDER BY (project_id, service, file, time)`.
- **Columns:**
  - `id` (the Log's UUIDv7), `time` (microseconds, UTC);
  - `level`, `service` and `environment` as `LowCardinality`;
  - `message` with ZSTD;
  - `trace_id`, `span_id`;
  - `batch`, the Log batch's id, for Match monitors;
  - `file`, the sort column, a `LowCardinality(String)`;
  - `attributes`, a plain `JSON` column with no type hints and the default `max_dynamic_paths`;
  - `attrs_json`, a `Map(LowCardinality(String), String)` of JSON-text values for array and object Attributes, as now;
  - `search_text`, `MATERIALIZED ''` with ZSTD, holding every string Attribute's value joined by `\0`. It serves `search` (see Reads), stays out of `SELECT *`, and costs about 6% of disk.
- **Attributes keep each value's type:** string, number (Float64, as DOUBLE now) or bool. A whole number too large for a Float64 is still a string. Log processing sends typed values through the native JSON column, never JSON text, which would infer Int64 or dates. `max_dynamic_paths` applies per part, and a part holds one Project's day, so a busy Project's keys never push a quiet Project's out into shared data.
- **The `file` column** holds the `file` Attribute's value when it's a string, and an empty string otherwise. The Attribute itself stays in `attributes`. A Query that compares `file` with a string also filters on the column, so the primary key skips other files. (A typed JSON hint can sit in `ORDER BY`, but it turns a number `file` into a string, so the probe kept the column.)
- **A minmax skip index on `batch`** lets a Match monitor's run skip granules ingested before its cursor.
- **No skip index on `message`.** The n-gram index doubled ingest CPU and pruned little, and a token index can't serve substring `contains`.
- **The bucket tier** (when `object_store` is set):
  - The storage policy has a local volume and a bucket volume. The bucket volume is an S3 disk on the same bucket, under a new `clickhouse/` prefix, using the same key, with GCS's quirks set (no batch delete).
  - It has a filesystem cache for reads.
  - Parts move to the bucket once their newest Log is `local_cache_hours` (24) old, and earlier when the local disk runs low (`move_factor`).
- **No promotion.** The JSON type replaces promoted columns, their daily re-rank, `ALTER TABLE`, the `coalesce` for a key promoted partway through a day, and the 01:00 demotion.

### Log processing writes

- **Pebble stays in front of the `200`.** Log processing still reads a Project's Log batches from its Processing cursor, runs the Log transform, and stores the Logs. It now inserts them into `logs`, about once a second per Project, up to today's batch limit.
- **Exactly once:**
  1. Before an insert, Log processing records in Pebble the range of Log batches it is about to insert.
  2. The insert carries a dedup token made from that range. `non_replicated_deduplication_window` is set on `logs`, on every MV's target table and on the key counts table: without it on an MV's target, a re-sent insert is dropped from `logs` but counted twice there. The writer sets `async_insert=0` (26.x defaults it to 1) and `max_insert_threads=1`.
  3. After the insert, the Processing cursor moves, in Pebble, and the batches are deleted.
  4. After a crash, Log processing re-sends the recorded range with the same token, and ClickHouse drops the repeat.
- **The Processing cursor moves to Pebble**, beside Event processing's.
- **Key counts:** Log processing still counts Attribute keys per Project per day as it stores Logs (a top-K of about 1,000). It now inserts the counts into a summing table partitioned like `logs`, which feeds the Fields list. Every Log is counted: there are no late Logs.
- **The histogram's MV:** a materialized view keeps counts by Project, minute and level in a summing table partitioned like `logs`.
- **Bin cache generations:** after each insert, Log processing bumps the in-memory generation of each (Project, hour) it wrote to.
- **No export, no hot window.** A Log is in its final place once inserted, whatever its time. The Log export, its timer, `logs_late`, the hot tables, the export's write lock and its checkpoints all go.
- **The commit counts line stays:** once a minute, per Project, it counts inserts in place of commits.

### Reads

- **The Query compiler emits ClickHouse SQL**, one dialect. The language, its meaning and its answers don't change. In particular:
  - Attribute reads use JSON paths, and comparisons and aggregates use the path's typed subcolumns, never the bare `Dynamic` path, which throws on a path holding mixed types. `== 200` reads `.:Float64`, `== "200"` reads `.:String`, and `== true` reads `.:Bool`. A missing path reads as NULL, so `==`, `!=` and `not` all fail to match, as now.
  - `tostring` is `coalesce(.:String, toString(.:Float64), toString(.:Bool))`, and `tonumber` is `coalesce(.:Float64, toFloat64OrNull(.:String))`. An Attribute is present if its path isn't NULL or `attrs_json` has its key.
  - Text matching uses ClickHouse's `lower`/`position`/`startsWith`/`match` equivalents, keeping today's semantics: `contains` is a case-insensitive substring and `contains_cs` a case-sensitive one.
  - `search` reads `message` and `search_text`, never key names. That took 390–576 ms on the quarter-hour, against 760–980 ms for an `or` over every path.
  - Time bins use `toStartOfInterval` with the same origin.
  - The approximate aggregates use ClickHouse's: `uniq` for `dcount` and `quantileTDigest` for percentiles. The root tests' expected values hold.
  - 26.8's automatic PREWHERE can read all of `message` first when a filter mixes `message` matches with sort-column conditions (610 ms against 79 ms on 25.8). The compiler puts the conditions that don't touch `message` in `PREWHERE` and the `message` matches in `WHERE` where that helps (97 ms).
- **The Readable SQL** a Query shows is ClickHouse SQL against SQL mode's tables, and still runs in SQL mode.
- **The newest page** is one query with lazy materialization, newest first, which 26.8 allows up to 10,000 rows by default. On the quarter-hour it took 64–70 ms, against 74–87 ms for today's two steps. Ticket 03 checks it over a range of several days.
- **Charts and tables** run as one query over `logs`. The bin cache stays:
  - A bin is keyed by the Project, the Query, its size and start, and the generations of the hours it covers.
  - Generations are in-memory counters per (Project, hour). Log processing bumps them on insert, and Retention and Project deletion bump them when they drop data. The cache is in memory too, so both reset together on restart.
  - A bin of an hour that hasn't ended isn't cached, which spares churn.
- **The histogram:** with no condition and bins of a minute or more, it reads the MV. Otherwise it goes through `ScanLogTable` and the bin cache, as now.
- **A Log by id** is looked for in the partitions around its id's time first (the id is a UUIDv7 made at ingest), then across the Project. With its time, only that time's partition is read.
- **Match monitors** read Logs whose `batch` is after their cursor, in Log batch order, as now.
- **Fields** come from the key counts table, as now.
- **Timeouts:** run timeouts become ClickHouse's `max_execution_time` plus cancelling the context. Each statement a read ran is still logged (`log_read_sql`).

### Logs from before the switch

- **The old Log files stay where they are.** ClickHouse reads them in place with `s3()` from the bucket, or `file()` for local files, with the Log file store mounted read-only into the ClickHouse container. Nothing is imported.
- **Loading:** at its first start on a prepared install, EventStore loads the prepared manifest, promoted sets and key counts (see Cutover) into ClickHouse tables of old Log files, old promoted sets and key counts, and records the switch time.
- **Reading:** a read whose range reaches before the switch time also reads that range's old files.
  - Each day's files are read with that day's promoted set. A promoted key reads `coalesce(column, map[key])`, as `LogColumns.Attribute` does now. A file without a column the day's set has reads it as NULL.
  - Old files and `logs` are read together, since a late Log of an old hour inserted after the switch lands in `logs`.
- **Old hours' bins** are cached like any other: their generation changes only when Retention or Project deletion deletes their files.
- **Retention and Project deletion** delete old files' objects and local copies, and their rows.
- **SQL queries don't read old files.**
- **This path is separate and deletable.** A follow-up ticket removes it once the last old day has passed Retention: about 30 days after the release on production.

### Retention, Project deletion and the disk limit

- **Retention** runs daily, as now. For each Project it drops the `logs`, MV and key-count partitions of days past its Retention, and deletes old Log files past it.
- **Project deletion** (a Project gone from Console's `projects`, found by Cleanup as now) drops all of that Project's partitions, deletes its Events, Issues, Pebble data and old Log files, and bumps its generations.
- **The disk limit** (`logs_disk_limit_gb`) keeps its meaning:
  - With a bucket, `move_factor` moves parts to the bucket first. If ClickHouse's local data is still over the limit, the oldest (Project, day) partitions are dropped and an error is logged, since the bucket must be failing.
  - Without a bucket, the oldest partitions are dropped.

### Events

- **One `events` table:**
  - `ORDER BY (project_id, issue_fingerprint_id, timestamp)`, `PARTITION BY toYYYYMM(timestamp)`;
  - the same nine columns, with `tags` a `Map(String, String)`;
  - the same storage policy as `logs`.
- **Event processing** inserts each batch with a dedup token from a recorded range, as Log processing does. This replaces `indexedUpTo` and the shared `batchLock` with the archive.
- **Unchanged:** raw payloads stay in Pebble, and Issues and their counts stay in Console's SQLite ([ADR 0001](../../docs/adr/0001-eventstore-writes-console-tables.md)).
- **Reads keep their answers:**
  - the list, newest or oldest first with offset;
  - the context, previous and next over the Issue's history, with ClickHouse's window functions;
  - the count over a window, for integration thresholds.
  - While ClickHouse is unreachable, Event reads answer 503, as they do while the `events` view is missing now.
- **Gone:** `events_hot`, the nightly archive, the archive consumer, Event archive uploads, and Console's `EventStoreArchiveJob` with its recurring schedule and the `archive_events` message.
- **Old Events aren't migrated.** No install that matters has any. `events.duckdb` and old archive files are left untouched.
- **Event Retention stays deferred.** It is a `DROP PARTITION` whenever it's wanted.

### SQL queries

- **An SQL query is ClickHouse SQL**, still behind the instance setting, still off by default.
- **Isolation is structural:**
  - Each Project has a read-only ClickHouse SQL user. Its row policies on `logs` and `events` limit it to its Project.
  - The explorer's time range is applied too, through settings the row policies read. The range is a convenience, not a boundary: an SQL query may change it only within its own Project.
  - A settings profile with constraints caps execution time, memory, threads and result size, so they can't be raised.
  - The SQL users get no grants for `s3`, `file`, `url`, `remote` or other table functions, nor for system tables.
  - EventStore runs each SQL query as its Project's user. The mechanism, impersonation or credentials only EventStore holds, is the ticket's choice.
- **What SQL sees:** `logs` (built-in fields, `attributes` as JSON, the JSON-text map, `id`) and `events` (its nine columns). Old Log files aren't visible to SQL.
- **The read-only and scope checks** on DuckDB's AST go, along with the slow-text-match notice that read the same AST.
- **Errors:** EventStore passes ClickHouse's error text and position through. Console's parser of DuckDB's `LINE n:` errors becomes one for ClickHouse's position, so the explorer still points at the error.
- **Docs:** `docs/logs/sql.md`, the `run_sql` tool's description, the instance setting's text and the MCP docs describe ClickHouse SQL. ADR 0003 is amended.

### Cutover

- **The last DuckDB release** ships `event_store prepare-clickhouse`, an offline command run with the stack stopped. It:
  1. processes every Project's waiting Log batches;
  2. exports every hot and late Log to Log files, ignoring the busy rule and cutoffs;
  3. finishes pending deletions;
  4. uploads every file when there is a bucket, and fails if it can't;
  5. writes the prepared files and a marker to `prepared/` in the storage directory: Parquet of the manifest rows, the promoted sets, the key counts and the Log Processing cursors.

  Events aren't touched. Run again, it does nothing.
- **The ClickHouse release:**
  - **`deploy/install`**, finding DuckDB data with no marker, runs `prepare-clickhouse` from the previous release's image (pulled from ghcr.io) before starting the new stack.
  - **EventStore**, finding `logs.duckdb` with no marker, refuses to start and names the command. With a marker, it loads the prepared files once (see "Logs from before the switch") and records that in ClickHouse.
  - `logs.duckdb` and `events.duckdb` are left on disk for the operator to delete.
- **The prepared files are the contract between the two releases.** A small set written by the command is committed as test data for the ClickHouse release's tests.

### What goes

- **EventStore:**
  - the hot tables, `logs_late` and promotion;
  - the Log export and its timer;
  - the manifest, pins, tombstones and backfill;
  - Log file uploads and eviction;
  - reading `projects.log_file_order`;
  - httpfs and the DuckDB secret;
  - the SQL AST checks and notice;
  - `events_hot`, the Event archive and its consumer and uploads;
  - duckdb-go;
  - the DuckDB benches (`logslayouts`, `logexport`, `logreads`, `objectstore`);
  - the Dockerfile's extensions stage and `DUCKDB_VERSION`;
  - the 30 s stop grace for checkpoints.

  The S3 adapter stays while old Log files need deleting.
- **Console:**
  - the Log file order card, the `log_file_order` column (by migration) and its MCP field in `list_projects`;
  - `EventStoreArchiveJob` and its schedule;
  - the DuckDB error parser.

### Domain words and records

- **A new ADR** (0016; 0014 belongs to Dashboard filters) records Logs and Events in ClickHouse, with the probe's numbers and the options weighed: Parquet with ClickHouse as the query engine, ClickHouse only for the hot table, chDB, a table per Project, typed `Map`s. It supersedes ADRs 0004, 0005, 0007 and 0013, supersedes ADR 0008's Log file and Event archive parts, and amends ADR 0003.
- **GLOSSARY.md:**
  - **Event processing**, **Log batch**, **Log processing** and **Processing cursor** say ClickHouse and Pebble.
  - **SQL query** says ClickHouse SQL, isolated per Project.
  - **Log export**, **Late Log**, **Late file** and **Log file order** go, kept as _Avoid_ where a word might come back.
  - **Log file store** becomes the store of Log files written before the switch, read in place until Retention deletes them.
  - **Bin cache** says generations per hour.
- **Docs:** `README.md`, `docs/logs/object-store.md` and `deploy/` follow.

## Testing Decisions

- **A good test** posts Logs or Events at a manual clock's times, runs Log processing or Event processing as their timers would, and checks what a member or operator can see: the Logs a Query lists and in what order, a table's rows, a histogram's counts, an SQL query's answer or refusal, Fields' counts, an Issue's Events, what is left after Retention or deletion. It never asserts the SQL the compiler emits, ClickHouse's parts, or how a table is laid out.
- **The seam: EventStore's HTTP API, in the root `main_test` package**, against a real ClickHouse (`bin/clickhouse-dev`), real Pebble and Console's test SQLite.
  - The existing root tests are the spec, and they keep what they assert: the language on stored Logs, the newest page, Charts, rates, text matching, the histogram, `after_batch`, Retention, Project deletion, SQL scope, Event reads and Event processing. Helpers that reach DuckDB directly change to reach ClickHouse.
  - Tests of what goes are deleted with it: the hot window, the export, late files, promotion, the manifest, uploads, eviction, the Log file order and the Event archive. Where the behaviour survives, a root test covers it instead:
    - a late Log is found by an Attribute filter at once;
    - a replayed range of Log batches or Events is stored once;
    - a restart after an insert and before the cursor moves loses and repeats nothing;
    - a deleted Project's Logs and Events are gone.
- **New root tests:**
  - an SQL query can't read another Project, a file, a URL, the bucket or a system table, and stops at its limits;
  - the histogram read from the MV equals one counted from `logs`;
  - the bin cache misses after a Log is inserted into a cached hour, and after Retention;
  - Logs from a committed set of prepared files read across the switch time;
  - Retention deletes old Log files past it;
  - EventStore refuses to start on unprepared DuckDB data.
- **The bucket:** the bucket-tier tests run against the MinIO already used in development (`~/.cache/solid-trace/minio`) or a local S3 stand-in. Old Log files in a bucket are read through the same stand-in.
- **`prepare-clickhouse`** is tested on the DuckDB side, in its own branch: from a store with hot, late and exported Logs, a pending deletion and Log batches waiting in Pebble, to the prepared files and the marker. Running it twice changes nothing. It writes the committed fixture set.
- **Console:** the Rails tests change where the Log file order and the archive job go. vitest covers the ClickHouse error parser. The Playwright flows on the dev stack (`logs-v1-sql`, the Logs explorer flows, `issues`, `project-delete`) pass against the dev stack's ClickHouse.
- **Probes** stay under a minute each and live in `bench/`, outside the suite.
- **Prior art:** `logs_language_test.go`, `logs_sql_scope_test.go`, `logs_bucket_test.go` (the fake bucket), `processing_test.go` (replays), `logs_after_batch_test.go`, `integration_test.go`.

## Out of Scope

- A search index on `message`, a token operator in the language, or ClickHouse's text index: a separate effort.
- Per-Chart materialized views or projections.
- Keeping a per-Project Log file order. Generic sort columns filled per Project could bring it back later without changing how Logs are stored.
- Importing old Log files into ClickHouse, or migrating old Events.
- Event Retention, which stays deferred.
- ClickHouse replication, Keeper, sharding or a cluster.
- Changing the query language, Log ingest, normalisation or the Log transform.
- Turning SQL queries on by default.
- OTLP ingest.
- The deferred hardening items of the [prod-readiness review](../prod-readiness/review.md). Compose host networking stays as it is: ClickHouse listens on loopback, so it doesn't widen it.

## Further Notes

**From the [research](../eventstore-performance/research/clickhouse-vs-duckdb.md)** (2 CPUs):
- Ingest CPU is about the same without an index, about 15 µs a Log. ClickHouse uses both CPUs.
- On a production quarter-hour (3.16 M Logs, 108 columns), the 14 slowest reads took 7.5 s of CPU on ClickHouse against 15.8 s on DuckDB, and 1.0 s with the query condition cache. A Dashboard refresh took 0.20 s against 0.61 s.
- DuckDB still won the newest page: 72–165 ms against 243–250 ms with lazy materialization.

**From the [probe](issues/01-probe-clickhouse-26-8.md)** (26.8.34.13, 2 CPUs, the same quarter-hour):
- JSON Attributes used 0.95× the 108 promoted columns' CPU on the 14 slow reads, about the same on Dashboard 1's Charts, and 1.1–1.2× on reads of Attributes alone.
- Inserts kept up at 3,580 Logs a second in one-second inserts, using 8% of 2 CPUs, with at most 8 active parts. That is about 33 µs a Log with JSON (26 µs with promoted columns), not the research's synthetic 15 µs.
- Reads during ingest stayed in tens of milliseconds.
- DuckDB's old Log files read correctly in place through `file()` and `s3()`.

**Production on 2026-10-23:**
- Two Projects (`tagalys`, `solidtrace`), both with 30-day Retention and Log file order `[service, file]`.
- `tagalys` sends about 12 M Logs an hour. Old Log files hold about 360 GB at 30 days, read in place for up to a month after the switch.
- No Issues or Events.
- 14 SQL reads and 14 `run_sql` calls in 30 days, none on `events`.

**ClickHouse 26.x:**
- From 26.6, x86 builds need AVX2. The dev box has it, and production is arm64.
- From 26.7, S3 credentials are no longer inherited by SQL queries by default, which suits the SQL users.
- 26.4 to 26.8 list 57 breaking changes; the probe reads them for anything that touches this design.

**Zone recovery** ([01](../zone-recovery/issues/01-disk-snapshots-and-restore.md)): ClickHouse keeps the metadata of its bucket parts on local disk. A disk snapshot must cover ClickHouse's data directory, since the bucket alone can't be restored from.

**Dev box:** 9.5 GB of its 88 GB disk is free. The probe and the test database must stay small: one quarter-hour, then deleted.
