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
- **v0.11.1** (`89d6e82`, on `main`) carries the fix. Production ran it from 09:35:45 UTC. The newest page over old Logs and Log by id on an old Log answer again. Until then, something on `tagalys` (likely an open Dashboard or a Monitor) had hit the error every 15 seconds.

**2026-10-23, speed.** The research's MCP Queries were timed through Console's `mcp_tool` call-timing lines (`duration_ms`), one at a time. The DuckDB baseline is the research's run on v0.9.0 (2026-10-22 23:34 UTC) over `tagalys`'s 21:15–21:30 quarter-hour (3.16 M Logs). ClickHouse was run over the same quarter, read from the old files ("across the switch"), and over 09:15–09:30 today, which is all ClickHouse (about 3 M Logs). How to recover them: `~/.cache/solid-trace/clickhouse-notes/ticket-11/README.md`.

| Query | DuckDB | ClickHouse, old files | ClickHouse |
|---|---|---|---|
| S3 `file in` 6 files, count by file | 62 | 43 | 18 |
| S6 `file in` 4 files, min/max by region, file | 217 | 76 | 20 |
| S0 newest page (3) | 1,392 | 357 | 78 |
| S1 region and file, `message startswith` | 1,756 | 35 | 28 |
| S8 region, service, level, `not(message contains)` | 2,913 | 212 | 175 |
| S2 `message contains "lounge"` | 3,961 | 1,023 | 407 |
| S5 `search "lounge"`, summarized | 4,856 | 3,260 | 615 |
| S4 `search "burton-na-staging"`, summarized | 5,574 | 3,406 | 587 |
| S9 `file contains "webhook"` | 281 | 201 | 180 |
| S10 file, region and `message contains` | 307 | 38 | 26 |
| S7 `tostring(store_id) in (…)` | 1,008 | 361 | 209 |
| S13 count by 1 minute | 128 | 83 | 40 |
| S11 `tags.host contains "lounge"` | 295 | 127 | 49 |
| S12 `search "timeout"`, page of 3 | 1,895 | 3,740 | 641 |
| **14 slow Queries, total (ms)** | **24,642** | **12,963** | **3,073** |
| **Dashboard 1, 17 Charts, total (ms)** | **2,206** | **1,495** | **875** |

- Over ClickHouse alone, the 14 slow Queries take an eighth of DuckDB's time, and Dashboard 1 about 40%.
- Over the old files, they take about half of DuckDB's time. One is slower: `search` for a newest page (S12), 3.7 s against 1.9 s, as old files have no `search_text` and `search` there is an `or` over every string column. That goes once the old files pass Retention (30 days).
- Differences from the baseline: S8's `take 40` was `take 2`, and the Charts' Queries (C7, C8, C9) got `| take 1`, to keep the answers small; S9 and S4 ran overlapping.

**2026-10-23, resources** (09:41 UTC, ClickHouse up 31 minutes, read-only over SSH):
- ClickHouse used 590 s of CPU in 1,861 s, about 0.32 of a core on average (16% of the 2 vCPUs), in bursts of 5–155% in `docker stats`. Most of it is merges: 7,039 merges of 52.6 M rows for 6.9 M inserted (about 7.6× write amplification), 480 s of merge time. Inserts used about 39 s of CPU per 10 minutes, and `log_key_counts` inserts about a fifth of that. Memory: 1.2–1.5 GiB.
- EventStore: 1–40% CPU, about 210 MiB.
- The `logs` table: 17 active parts, 6.66 M rows in 361 MiB, about 54 bytes a Log. `data/clickhouse` is 2.1 GB, and the root disk is 37% used.
- `system.part_log` isn't on, so merges can only be read from `system.events`. Turn it on if merges need watching.

- **To do:** CPU and memory over a full day, parts moving to the bucket after 24 hours (tomorrow from about 09:12 UTC), and Retention the morning after.
