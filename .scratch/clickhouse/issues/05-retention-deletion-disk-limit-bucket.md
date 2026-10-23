# 05: Retention, Project deletion, the disk limit and the bucket tier

**What to build:** Logs in ClickHouse are deleted by Retention and Project deletion, kept within the disk limit, and moved to the bucket after `local_cache_hours`. See the [spec](../spec.md)'s "Retention, Project deletion and the disk limit" and the bucket tier under "The `logs` table".

**Blocked by:** 04

**Status:** ready-for-agent

- [ ] **Retention's daily pass** drops each Project's `logs`, MV and key-count partitions of days past its Retention (the Project's, or the instance default), and bumps those hours' generations.
- [ ] **Cleanup's pass for deleted Projects** drops all of their Log partitions and bumps their generations. Events follow in 06.
- [ ] **The table's TTL** moves parts to the bucket volume once their newest Log is `local_cache_hours` old, and `move_factor` moves them earlier when the local disk runs low.
- [ ] **The disk limit** (`logs_disk_limit_gb`) measures ClickHouse's local data. Over the limit after moves, or with no bucket, it drops the oldest (Project, day) partitions, and logs an error when a bucket is configured.
- [ ] **Log file uploads, eviction, tombstones and pins** are no longer used for new Logs. Their code stays only where 09 needs it for old Log files.
- [ ] **Root tests:**
  - `logs_retention_test.go` and the Project deletion tests pass against ClickHouse;
  - with a bucket (the MinIO or S3 stand-in), parts older than `local_cache_hours` are read from it and still answer Queries;
  - the disk limit drops the oldest days with no bucket.
- [ ] Targeted tests only, `go test -p 1`.
