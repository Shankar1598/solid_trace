# 02: EventStore's config under "Restart to change"

Spec: [../spec.md](../spec.md)

**What to build:** Settings → Instance shows EventStore's configuration as EventStore loaded it at start, so it matches what's running, not the file on disk now. That's the ingest and query ports, ClickHouse's address, database and user, the storage paths (Pebble, message queue, Console's SQLite, the Log file store), and the disk limit, both configured and the bytes in effect. It also shows the Bin cache size, the most ingest requests that may wait, the bucket (kind, name, prefix, region, endpoint, local cache hours) or none, the CPU profile directory and debug. Each row names its `event_store.yml` key. The ClickHouse password and the bucket's keys show only as set or not set. It comes end to end from a new `GET /config` on EventStore's loopback-only query API, through Console's EventStore service, into 01's answer builder. When EventStore can't be reached, the group says so and the rest of the page works.

**Blocked by:** 01

**Status:** resolved (merged into `feature/instance-settings` at fb543a2; Go handler/config tests, 15 Rails runs and 2 Playwright flows pass)

- [x] `GET /config` answers the loaded config, with the disk limit's configured and effective bytes, and never a secret's value (Go test on the query app)
- [x] The page shows the EventStore group with each row's `event_store.yml` key
- [x] With EventStore unreachable, the page renders and the group says it's unavailable (Rails test)
- [x] 01's Playwright flow also sees the EventStore group on the dev stack
