# Project status

Updated: 2026-09-27

SolidTrace: self-hosted error tracking with two services, Console (Rails) and EventStore (Go), and embedded databases. Terms are in [CONTEXT.md](../CONTEXT.md).

## Where things stand

- **Logs**: planning only, no code yet. 4 research tickets are done. The decision tickets start now. The goal is a v1 spec plus first-milestone tickets.
- **Prod-readiness**: [review.md](prod-readiness/review.md) lists about 9 gaps. 3 are filed as tickets. Query API auth (§2), the concurrent first-event 500 (§5) and Sentry protocol correctness (§6) are fixed.
- **Integration notification**: the spec is ready for an agent. It has no tickets yet.

## Next

1. [Logs 05: prod-readiness gaps for logs](logs/issues/05-prod-readiness-gaps-for-logs.md) (grilling)
2. [Prod-readiness 011: Pebble memtable size](prod-readiness/issues/011-pebble-memtable-size.md) (benchmark task)
3. [Prod-readiness 012: Sentry compatibility tests](prod-readiness/issues/012-sentry-compatibility-tests.md) (task)
4. [Integration notification spec](integration-notification/spec.md): split it into tickets

## Efforts

| Effort | Done / open | Start here |
|---|---|---|
| Logs | 4 / 9 | [map.md](logs/map.md) |
| Prod-readiness | 1 / 2 filed | [review.md](prod-readiness/review.md) |
| Integration notification | spec only | [spec.md](integration-notification/spec.md) |
| Logging-old | closed (wontfix) | replaced by the logs map; reference only |

**Logs path** (a ticket opens when the ones before it are done):
05, 06, 07 (open now) → 08, 09, 11 → 10, 12 → 13 (write the v1 spec)

## Last session (2026-09-27)

Mostly housekeeping. Tickets stay as local Markdown, and this file is the status page. Closed logging-old: its spec and all 17 tickets are now `wontfix`, and each one points to the logs map. No logs ticket was claimed or resolved. Then fixed review §6 (Sentry protocol correctness) in `event_store/ingest`. Filed 012 for Sentry compatibility tests. Found that SDKs retry no HTTP status and pause 60 s on a 429, so a full ingest channel now returns 503. Fixed §5, the concurrent first-event race.
