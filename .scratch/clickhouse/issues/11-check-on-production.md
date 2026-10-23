# 11: Check on production after the release

**What to do:** After the last DuckDB release (with `prepare-clickhouse`) and the ClickHouse release are out, upgrade production with `deploy/install` and check that the [spec](../spec.md) holds there. The user does the upgrade, and an agent can read the results over MCP.

**Blocked by:** 10

**Status:** ready-for-human

- [ ] **Before upgrading,** take a disk snapshot of the VM (see [zone recovery 01](../../zone-recovery/issues/01-disk-snapshots-and-restore.md)).
- [ ] **During the upgrade,** `deploy/install` prepares the install from the previous image, then starts ClickHouse and EventStore. Record how long each step took.
- [ ] **Logs keep arriving:** `log_commits` lines show inserts keeping up for `tagalys`, with no `Log processing is behind`.
- [ ] **Speed:** Dashboard 1 and the 14 slow Queries of the research, timed through MCP (`kind:"log_read"`), against the week before. Do it once over a range that's all ClickHouse, and once across the switch.
- [ ] **Resources:** CPU and memory of ClickHouse and EventStore over a day. Parts move to the bucket after 24 hours, under the `clickhouse/` prefix.
- [ ] **Retention** drops a day the morning after it's due, and old Log files past it are deleted.

## Comments

**2026-10-23, the upgrade.** Released as [v0.10.0](https://github.com/Shankar1598/solid_trace/releases/tag/v0.10.0) (the last DuckDB release, `2d947da`) and [v0.11.0](https://github.com/Shankar1598/solid_trace/releases/tag/v0.11.0) (ClickHouse, `84f5022`). Production was upgraded to v0.11.0 the same morning.
- **No disk snapshot:** the user skipped it, as losing production's test data is fine.
- **Timings, from EventStore's lines:** v0.10.0's `prepare-clickhouse` finished at 09:11:04 UTC (`prepared_at`), and EventStore had loaded the prepared state and was serving at 09:11:57. The last DuckDB Log export ran at 09:00:16, so the services were down for at most about 11 minutes. When the upgrade started isn't in the Logs.
- **Logs keep arriving:** the first minute caught up 518,568 `tagalys` Logs (8.8 s of inserts). Over the following ten minutes it inserted about 180–220k Logs a minute (3.3k/s), in about 320 inserts, which took 5.2–6.2 s of each minute, with the slowest at 35–70 ms. No `Log processing is behind`.
- **Bug found and fixed** (`fix/old-file-uuids`, `62aaa75`). Reads of old Log files that filter on `id` failed with `code: 117, Statistics have min_value > max_value … for column 'id'`: the newest page across the switch (its id step), and Log by id on old Logs. DuckDB wrote the UUID columns' statistics in byte order, and ClickHouse compares UUIDs in another order, so real UUIDv7 ids look out of order. The fixture's synthetic ids happened to agree in both orders, so the tests missed it. Old files now read `id` and `batch` as `FixedString(16)` and make them UUIDs in a subquery, which still prunes row groups by `time` (9 of 10 pruned on a test file). A fixture id is edited so the tests cover it.
- **Low:** EventStore's `Set when the logs table's parts move to the bucket` line, whose `was` value holds quotes, reaches the `solidtrace` Project split into stray Attributes (`TO`, `VOLUME`, `'bucket'"`). It looks like a logfmt quoting issue between EventStore's logger and Vector.
- **To do:** the speed comparison against the week before, a day of CPU and memory, parts moving to the bucket after 24 hours, and Retention the morning after.
