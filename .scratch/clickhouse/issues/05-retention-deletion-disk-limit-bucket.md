# 05: Retention, Project deletion, the disk limit and the bucket tier

**What to build:** Logs in ClickHouse are deleted by Retention and Project deletion, kept within the disk limit, and moved to the bucket after `local_cache_hours`. See the [spec](../spec.md)'s "Retention, Project deletion and the disk limit" and the bucket tier under "The `logs` table".

**Blocked by:** 04

**Status:** ready-for-agent

- [x] **Retention's daily pass** drops each Project's `logs`, MV and key-count partitions of days past its Retention (the Project's, or the instance default), and bumps those hours' generations.
- [x] **Cleanup's pass for deleted Projects** drops all of their Log partitions and bumps their generations. Events follow in 06.
- [x] **The table's TTL** moves parts to the bucket volume once their newest Log is `local_cache_hours` old, and `move_factor` moves them earlier when the local disk runs low.
- [x] **The disk limit** (`logs_disk_limit_gb`) measures ClickHouse's local data. Over the limit after moves, or with no bucket, it drops the oldest (Project, day) partitions, and logs an error when a bucket is configured.
- [x] **Log file uploads, eviction, tombstones and pins** are no longer used for new Logs. Their code stays only where 09 needs it for old Log files.
- [x] **Root tests:**
  - `logs_retention_test.go` and the Project deletion tests pass against ClickHouse;
  - with a bucket (the MinIO or S3 stand-in), parts older than `local_cache_hours` are read from it and still answer Queries;
  - the disk limit drops the oldest days with no bucket.
- [x] Targeted tests only, `go test -p 1`.

## Answer

Built on `clickhouse/05-retention-deletion-disk-bucket` from `feature/clickhouse` at 996c3b7, which hadn't moved when it finished. Notes for 09 and 10 (how Cleanup drives Retention and deletion, where old Log files hook in, the test bucket) are in `~/.cache/solid-trace/clickhouse-notes/05-notes.md`.

**What was built**
- **Retention and Project deletion** run as before, through Cleanup's daily pass, now on `ClickHouseLogs`.
  - Each drops whole (Project, day) partitions of `logs`, `log_minute_counts` and `log_key_counts`, found in `system.parts`.
  - It then bumps the generations of every hour of each day it touched, and forgets those days' key counts held in memory.
  - Drops are exempt from ClickHouse's 50 GB guard on partition drops, which a busy Project's day could reach.
- **The bucket tier.**
  - With an `object_store`, EventStore gives `logs` a TTL at start: a part moves to the `bucket` volume once its newest Log is `local_cache_hours` old. A part already that old at insert is written straight to the bucket. Without an `object_store`, the TTL is removed.
  - The table is altered only when its TTL differs, so a restart changes nothing.
  - The policy file EventStore writes now has `move_factor` 0.1, ClickHouse's default with two volumes, so it can be seen.
  - Only `logs` moves. The histogram's counts, the key counts and `events` stay on local disk.
- **The disk limit** counts `logs`' parts on ClickHouse's local disk, by (Project, day).
  - Past the limit, with a bucket, it moves the oldest days to the bucket and measures again. Any day it must still delete is logged as an error.
  - Without a bucket, it deletes the oldest days of any Project, logged as info.
  - Its default is half of ClickHouse's local disk, read from `system.disks`. It used to be half of the disk holding the Log file store.
- **Uploads and eviction are gone for new Logs.** `main` no longer starts the uploads worker, and `logprocessing/upload.go` is deleted. Cleanup no longer evicts.
  - DuckDB's upload, eviction, tombstone, pin and file-Retention code stays in `storage` until 10 deletes DuckDB. Nothing in Cleanup calls it.
  - Old Log files' Retention is 09's. The notes say where it hooks in.
- **Tests.**
  - `logs_retention_test.go` is rewritten against ClickHouse: each Project's own Retention, a deleted Project, an empty `projects` table, the disk limit by day and the default limit. Each checks pages, Queries, the histogram and Fields.
  - `logs_bucket_test.go` is new, and runs against the dev MinIO:
    - Logs older than `local_cache_hours` are off local disk. Pages, a table, a Log by id, the histogram and Fields still answer from the bucket, after a restart too. Retention then deletes the oldest day from the bucket.
    - The disk limit moves the older days to the bucket and deletes nothing.
  - New: a cached bin misses once Retention deletes its day. It fails without the generation bump.
  - Deleted: `logs_manifest_test.go`, the old `logs_bucket_test.go` (uploads, eviction, pins, httpfs) and the test of the disk limit waiting for a read.

**Differs from the ticket or spec**
- **The disk limit moves days to the bucket itself.** It doesn't leave that to `move_factor`, which only acts once the local disk is 90% full. With the default limit at half the disk, `move_factor` alone would leave the limit deleting days a working bucket could hold.
- **It measures Logs only:** `logs`' local parts. The counts tables are small and never move. `events` isn't counted, so Events never cost Logs.
- **The tests' bucket is a storage policy of their own.** `clickhousetest.Bucket` writes `solidtrace_test_bucket` into the dev ClickHouse's conf.d, on MinIO's `solidtrace-test`. Its key is written inline, and its `move_factor` is 0. A test makes its tables on that policy through `config.ClickHouse.StoragePolicy`, which isn't in the yml.
  - Adding the bucket volume to the shared `solidtrace` policy would let `move_factor` move the dev stack's tables to MinIO, since the dev disk is over 90% full. The volume couldn't be removed later without a restart.
- **`logs_reads_test.go`'s long read** is now `SELECT count(*) FROM range(…)` in DuckDB's SQL mode, since its `logs` view is empty. 07 moves it with SQL mode.

**Left for later**
- **09:** old Log files' Retention and deletion, and their generations. Old local files aren't counted by the disk limit.
- **10:** delete DuckDB's `logs_retention.go`, `logs_upload.go`, `logs_disk_limit.go` (with `NewLogDiskLimit` and its test) and the manifest. `LogDiskLimit` already lives in `logs_clickhouse_cleanup.go`. `docs/logs/object-store.md` still describes uploads and eviction.
- **Not tested:** the disk limit's error path with a failing bucket, and `move_factor` itself.
- **Dev box:** the main checkout's `storage/clickhouse/conf.d/probe05-storage.xml` is a probe's policy and is unused. Delete it only together with a restart of the dev ClickHouse, since ClickHouse can't drop a policy while it runs (02 found the same for a volume).
