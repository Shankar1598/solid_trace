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
