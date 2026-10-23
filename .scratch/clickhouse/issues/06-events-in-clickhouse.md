# 06: Events in ClickHouse

**What to build:** Event processing inserts Events into ClickHouse's `events` table, and the Issue pages, `get_issue` and integration thresholds read them from there. The nightly archive is gone. See the [spec](../spec.md)'s "Events".

**Blocked by:** 02

**Status:** ready-for-agent

- [ ] **The `events` table:**
  - `ORDER BY (project_id, issue_fingerprint_id, timestamp)`, `PARTITION BY toYYYYMM(timestamp)`;
  - the nine columns, with `tags` a `Map(String, String)`;
  - the storage policy from 02.
- [ ] **Event processing** records each batch's range in Pebble, then inserts it with a dedup token, then moves its cursor. `indexedUpTo`, `HotUUIDsBetween` and the archive's share of `batchLock` go. Issues, `times_seen` and Console messages are unchanged.
- [ ] **The three reads keep their answers:**
  - the list, newest or oldest first with offset;
  - the context, previous and next over the Issue's history, with ClickHouse's window functions;
  - the count over a window.

  While ClickHouse is unreachable they answer 503, as now without the view.
- [ ] **Cleanup's deleted-Project pass** deletes the Project's Events in ClickHouse, then its Issues and Pebble data, as now.
- [ ] **Gone:** `events_hot`, `ArchiveEventsUpTo`, the archive consumer, `event_archive_bucket`, and Console's `Maintenance::EventStoreArchiveJob` with its `recurring.yml` entry and tests. `archive_events` messages still waiting are ignored.
- [ ] **Root tests pass against ClickHouse:** `processing_test.go` (replays, held rows, seen counts), the Event cases of `integration_test.go`, `handler/events_test.go` and `routes_test.go`. `event_archive_bucket_test.go` is deleted. A new root test: a replayed batch of Events is stored once.
- [ ] **Rails:** `event_store_test.rb`, `issues_controller_test.rb`, `mcp/issue_tools_test.rb` and the integration notification tests pass, and the archive job's tests are removed.
- [ ] Targeted tests only.
