# Project status

Updated: 2026-09-27

SolidTrace: self-hosted error tracking with two services, Console (Rails) and EventStore (Go), and embedded databases. Terms are in [CONTEXT.md](../CONTEXT.md).

## Where things stand

- **Logs**: planning only, no code yet. Scoped down to an MVP on 2026-09-27: JSON-over-HTTP ingest, one global retention window, SQL mode plus a filter bar, a one-page explorer. The rest is on the map's Future list. SQL mode's safety rules are decided. The goal is an MVP spec plus its tickets.
- **Prod-readiness**: [review.md](prod-readiness/review.md) lists about 9 gaps. 3 are filed as tickets. Query API auth (§2), the concurrent first-event 500 (§5) and Sentry protocol correctness (§6) are fixed.
- **Integration notification**: the spec is ready for an agent. It has no tickets yet.
- **Upgrade Pebble and DuckDB**: merged. EventStore now uses Pebble v2.1.7 and the DuckDB 2.0 alpha engine (`v2.0.0-alpha43385`), linked as a shared library. Two follow-ups wait on it.

## Next

1. [Logs 07: What is a Log?](logs/issues/07-log-domain-model.md) (grilling; the only unblocked logs ticket; it unblocks 08, 09 and 11)
2. [Ingest write path 01: write the spec](ingest-write-path/issues/01-write-the-spec.md) (review §4; must land before logs MVP code starts)
3. [Prod-readiness 011: Pebble memtable size](prod-readiness/issues/011-pebble-memtable-size.md) (benchmark task)
4. [Prod-readiness 012: Sentry compatibility tests](prod-readiness/issues/012-sentry-compatibility-tests.md) (task)
5. [Integration notification spec](integration-notification/spec.md): split it into tickets

## Efforts

| Effort | Done / open | Start here |
|---|---|---|
| Logs | 6 done, 1 closed / 6 open | [map.md](logs/map.md) |
| Prod-readiness | 1 / 2 filed | [review.md](prod-readiness/review.md) |
| Integration notification | spec only | [spec.md](integration-notification/spec.md) |
| Upgrade Pebble and DuckDB | 1 / 2 | [02](upgrade-pebble-duckdb/issues/02-duckdb-2-0-0-release.md) |
| Logging-old | closed (wontfix) | replaced by the logs map; reference only |

**Logs path** (a ticket opens when the ones before it are done):
07 (open now) → 08, 09, 11 → 10 → 13 (write the MVP spec)

## Last session (2026-09-27)

Mostly housekeeping. Tickets stay as local Markdown, and this file is the status page. Closed logging-old: its spec and all 17 tickets are now `wontfix`, and each one points to the logs map. No logs ticket was claimed or resolved. Then fixed review §6 (Sentry protocol correctness) in `event_store/ingest`. Filed 012 for Sentry compatibility tests. Found that SDKs retry no HTTP status and pause 60 s on a 429, so a full ingest channel now returns 503. Fixed §5, the concurrent first-event race.

Upgraded EventStore and merged it to `main`, recorded in [ticket 01](upgrade-pebble-duckdb/issues/01-upgrade-pebble-and-duckdb.md):
- Pebble v2.1.7, with the format pinned to `FormatValueSeparation` and value separation off.
- DuckDB 2.0 alpha: the `v2.20000.0-6.preview` driver built with `-tags=duckdb_use_lib`, linked to a pinned `libduckdb` from `event_store/scripts/fetch-duckdb` through the `mise.toml` env. Also `OrderedMap` and tests for the time filters and the engine version.
- The Dockerfile now builds with Go 1.25 and cgo.

Follow-ups:
- [02](upgrade-pebble-duckdb/issues/02-duckdb-2-0-0-release.md): move to DuckDB 2.0.0 (due 2026-10-21).
- [03](upgrade-pebble-duckdb/issues/03-pebble-value-separation.md): benchmark Pebble value separation.

The logs map now records that duckdb-go has no real 2.0 build yet.

Then, on the `feature/logging` worktree: logs map scoped down to an MVP. Resolved 06 (explorer layout) by decision, closed 12 (query builder) as Future, and narrowed 05, 08, 09, 10, 11 and 13 to the MVP. Decided SQL mode's safety rules on 11: read-only, one SELECT, its own DuckDB instance; a large query may take down EventStore, and host-file reads are allowed, both accepted for the MVP. Logs durability waits for the Event ingest change to write each Event to Pebble first. Resolved 05: only the §4 Pebble-first write path must land before MVP code; §8 is a constraint on log ingest.
