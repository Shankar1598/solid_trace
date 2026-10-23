# Logs and Events in ClickHouse

Supersedes [ADR 0004](0004-logs-hot-table-and-hourly-parquet.md), [ADR 0005](0005-logs-per-project-hot-tables-promote-at-ingest.md), [ADR 0007](0007-log-files-listed-in-a-manifest.md) and [ADR 0013](0013-busy-projects-logs-exported-every-quarter-hour.md), and the Log file and Event archive parts of [ADR 0008](0008-log-parquet-in-a-bucket.md). [ADR 0003](0003-unrestricted-sql-queries-behind-an-instance-setting.md) was amended for it on 2026-10-23.

EventStore keeps Logs and Events in a ClickHouse server, 26.8 LTS, that runs beside it in the compose stack, and DuckDB leaves EventStore. Pebble stays in front of the `200`. Log processing and Event processing insert into ClickHouse about once a second. Before each insert they record in Pebble the range of Log batches or Events it holds, and the insert carries a dedup token made from that range, so a range sent again after a crash is stored once.

- **Logs** are one shared `logs` table, partitioned by Project and UTC day and sorted by Project, `service`, the `file` Attribute and time. Attributes are one `JSON` column, each value keeping its type (string, number or bool). Arrays and objects are JSON text in a map beside it, and every string value is also in `search_text`, which `search` reads.
- **Events** are one `events` table, sorted by Project, Issue fingerprint and time.
- **The bucket:** with an `object_store`, a part moves to the install's bucket once its newest Log is `local_cache_hours` old, through a ClickHouse disk on the same bucket and key.
- **Deleting:** Retention, Project deletion and the disk limit drop whole (Project, day) partitions.

We chose this because ClickHouse answered the same Queries with less CPU, and because each speed-up on DuckDB had added machinery that ClickHouse doesn't need:
- the hot tables, which filled in arrival order and couldn't be sorted;
- the Log export with its late Logs and late files;
- the promoted columns, re-ranked daily;
- the manifest with its pins and tombstones;
- the uploads and eviction;
- the per-Project Log file order;
- the Event archive.

A Log is in its final place once inserted, whatever its time.

## The numbers

**The [research](../../.scratch/eventstore-performance/research/clickhouse-vs-duckdb.md):** ClickHouse 25.8 against DuckDB 1.5.6 on a production quarter-hour of `tagalys` (3.16 M Logs, 108 columns), 2 CPUs.
- The 14 slowest Queries of a week used 7.5 s of CPU on ClickHouse against 15.8 s on DuckDB. They used 1.0 s once repeated, with ClickHouse's query condition cache, which a Dashboard or Monitor repeating its Query gets.
- A Dashboard refresh used 0.20 s against 0.61 s.
- A filter combining the sort columns with `or` took 4 ms against 780 ms.
- `message` searches were 1.5 to 3.5 times faster.

**The [probe](../../.scratch/clickhouse/issues/01-probe-clickhouse-26-8.md):** 26.8.34.13, the same quarter-hour.
- **Reads:** the JSON column used 0.95× the CPU of the 108 promoted columns on the 14 slow reads, about the same on a Dashboard's 16 Charts, and 1.1 to 1.2× on reads of Attributes alone. Every read gave the same answer on both.
- **Inserts:** one-second inserts kept up with 3,580 Logs a second on 8% of 2 CPUs, about 33 µs a Log (26 µs with promoted columns), with at most 8 active parts. Reads during ingest stayed in tens of milliseconds.
- **The newest page:** 64 to 70 ms in one query with lazy materialization, against 72 to 165 ms on DuckDB.
- **`search`:** 390 to 576 ms through `search_text`, against 760 to 980 ms as an `or` over every path.
- **Memory:** under a container memory limit, 26.8 returned freed pages at once and scans ran about 1.9× slower.

## SQL queries

SQL queries are ClickHouse SQL, run as written, with the isolation ADR 0003 said DuckDB couldn't give:
- **Each Project has a SQL user,** a read-only ClickHouse user that EventStore makes the first time a process runs the Project's SQL. Its random password lives only in that process's memory, and EventStore connects as it. Credentials were chosen over impersonation (`EXECUTE AS`), which would mean writing the user's SQL into a statement of EventStore's own.
- **Row policies** on `logs` and `events` show it only its Project's rows, in the explorer's range, which they read from two settings a query may widen within its Project.
- **A settings profile** with constraints caps its time, memory, threads and result size.
- **Its only grants** are SELECT on `logs` and `events`: no other table, system table or table function that reads.

Project deletion drops the user and its policies with the Project's Logs. ADR 0003's amendment of 2026-10-23 has the details.

## Logs from before the switch

The Log files the DuckDB releases wrote stay where they are, on disk or in the bucket. The last DuckDB release's `prepare-clickhouse` exports every Log still in DuckDB to Log files. It then writes the manifest, the promoted sets, the key counts and the Processing cursors beside `logs.duckdb`, with a marker. `deploy/install` runs it from the previous release's image.

At its first start, EventStore loads them into ClickHouse and reads the files in place with `file()` and `s3()`; nothing is imported. It refuses to start on a `logs.duckdb` with no marker. All of this is in one module, `storage/logs_old_files.go`, which goes once the last old day has passed Retention.

## Considered options

- **Keep the Log files, with ClickHouse as their query engine:** the hot table, the export, late files, the manifest and promotion would all stay, with their edges.
- **ClickHouse for the hot table only, DuckDB's files for the rest:** it would sort recent Logs, but keep two engines, the export and the files, with every read a union of both.
- **chDB, ClickHouse embedded in EventStore:** no second container, but merges would run in EventStore's process, and SQL queries would have no users to isolate them.
- **A table per Project with promoted columns:** the probe's fallback had the JSON column been clearly slower. It brings back promotion, its re-ranking and an `ALTER TABLE` per new key, for no gain at 0.95× the CPU. In one shared table, each part holds one Project's day, so a busy Project's keys never push a quiet Project's out of the JSON column's typed paths.
- **Typed `Map`s, as DuckDB's tables had:** no schema to change, but reading one key reads the whole map, so an Attribute filter reads every Attribute of its type.

## Consequences

- **ClickHouse is a second server to run.**
  - It is pinned to an exact patch. 26.8.34.13 skips a boundary second when `time` is compared with a DateTime or an integer, so the Query compiler compares it only with `toDateTime64(…)`.
  - It listens on loopback only, with EventStore's password from `SOLIDTRACE_CLICKHOUSE_PASSWORD`, which `deploy/install` generates.
  - Its memory is half of RAM, set by `max_server_memory_usage` from `deploy/install`, never by a container memory limit.
- **While ClickHouse is down,** ingest still answers `200`, since Pebble is in front. `/health` says ClickHouse is unreachable, and Event reads answer 503.
- **clickhouse-go 2.49** can't decode a `JSON` column that 26.8 sends after a sort merges several parts. EventStore's connection and the SQL users' profile both ask for it flattened (`output_format_native_use_flattened_dynamic_and_json_serialization`).
- **A disk snapshot must cover ClickHouse's data directory,** which holds the metadata of the parts in the bucket. The bucket alone can't be restored from.
- **The Log file order is gone:** every Project is sorted by `service` and `file`. Generic sort columns filled per Project could bring it back without changing how Logs are stored.
- **Old Events aren't migrated.** `events.duckdb` and `logs.duckdb` are left on disk for the operator to delete. A DuckDB release's config keys (`duckdb_*`, `logs_duckdb_path`, `parquet_storage_path`) load with a warning.

Source: [Logs and Events in ClickHouse](../../.scratch/clickhouse/spec.md).
