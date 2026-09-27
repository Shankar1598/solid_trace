# Project status

Updated: 2026-09-27

SolidTrace: self-hosted error tracking with two services, Console (Rails) and EventStore (Go), and embedded databases. Terms are in [CONTEXT.md](../CONTEXT.md).

## Where things stand

- **Logs**: planning only, no code yet. Scoped down to an MVP on 2026-09-27: JSON-over-HTTP ingest, one global retention window, SQL mode plus a filter bar, a one-page explorer. The rest is on the map's Future list. SQL mode's safety rules are decided. The goal is an MVP spec plus its tickets.
- **Prod-readiness**: [review.md](prod-readiness/review.md) lists about 9 gaps. 3 are filed as tickets. Query API auth (§2) and Sentry protocol correctness (§6) are fixed.
- **Integration notification**: the spec is ready for an agent. It has no tickets yet.

## Next

1. [Logs 05: prod-readiness gaps for logs](logs/issues/05-prod-readiness-gaps-for-logs.md) (grilling, claimed; round 1 needs redoing for the MVP scope)
2. [Prod-readiness 011: Pebble memtable size](prod-readiness/issues/011-pebble-memtable-size.md) (benchmark task)
3. [Prod-readiness 012: Sentry compatibility tests](prod-readiness/issues/012-sentry-compatibility-tests.md) (task)
4. [Integration notification spec](integration-notification/spec.md): split it into tickets

## Efforts

| Effort | Done / open | Start here |
|---|---|---|
| Logs | 5 done, 1 closed / 7 open | [map.md](logs/map.md) |
| Prod-readiness | 1 / 2 filed | [review.md](prod-readiness/review.md) |
| Integration notification | spec only | [spec.md](integration-notification/spec.md) |
| Logging-old | closed (wontfix) | replaced by the logs map; reference only |

**Logs path** (a ticket opens when the ones before it are done):
05, 07 (open now) → 08, 09, 11 → 10 → 13 (write the MVP spec)

## Last session (2026-09-27, `feature/logging` worktree)

Logs map scoped down to an MVP. Resolved 06 (explorer layout) by decision, closed 12 (query builder) as Future, and narrowed 05, 08, 09, 10, 11 and 13 to the MVP. Decided SQL mode's safety rules on 11: read-only, one SELECT, its own DuckDB instance; a large query may take down EventStore, and host-file reads are allowed, both accepted for the MVP. Logs durability waits for the Event ingest change to write each Event to Pebble first. 05 is claimed; its round-1 questions predate the MVP scope.
