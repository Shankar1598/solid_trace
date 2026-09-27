# Project status

Updated: 2026-09-27

SolidTrace: self-hosted error tracking with two services, Console (Rails) and EventStore (Go), and embedded databases. Terms are in [CONTEXT.md](../CONTEXT.md).

## Where things stand

- **Logs**: planning only, no code yet. 4 research tickets are done. The decision tickets start now. The goal is a v1 spec plus first-milestone tickets.
- **Prod-readiness**: [review.md](prod-readiness/review.md) lists about 9 gaps. 2 are filed as tickets; query API auth is fixed.
- **Integration notification**: the spec is ready for an agent. It has no tickets yet.
- **Upgrade Pebble and DuckDB**: on branch `upgrade-pebble-duckdb`, not yet merged. EventStore now uses Pebble v2.1.7 and the DuckDB 2.0 alpha engine (`v2.0.0-alpha43385`), linked as a shared library. Two follow-ups wait on it.

## Next

1. [Logs 05: prod-readiness gaps for logs](logs/issues/05-prod-readiness-gaps-for-logs.md) (grilling)
2. [Prod-readiness 011: Pebble memtable size](prod-readiness/issues/011-pebble-memtable-size.md) (benchmark task)
3. [Integration notification spec](integration-notification/spec.md): split it into tickets

## Efforts

| Effort | Done / open | Start here |
|---|---|---|
| Logs | 4 / 9 | [map.md](logs/map.md) |
| Prod-readiness | 1 / 1 filed | [review.md](prod-readiness/review.md) |
| Integration notification | spec only | [spec.md](integration-notification/spec.md) |
| Upgrade Pebble and DuckDB | 0 / 3 | [01](upgrade-pebble-duckdb/issues/01-upgrade-pebble-and-duckdb.md) |
| Logging-old | closed (wontfix) | replaced by the logs map; reference only |

**Logs path** (a ticket opens when the ones before it are done):
05, 06, 07 (open now) → 08, 09, 11 → 10, 12 → 13 (write the v1 spec)

## Last session (2026-09-27)

Upgraded EventStore on the `upgrade-pebble-duckdb` branch, recorded in [ticket 01](upgrade-pebble-duckdb/issues/01-upgrade-pebble-and-duckdb.md):
- Pebble v2.1.7, with the format pinned to `FormatValueSeparation` and value separation off.
- DuckDB 2.0 alpha: the `v2.20000.0-6.preview` driver built with `-tags=duckdb_use_lib`, linked to a pinned `libduckdb` from `event_store/scripts/fetch-duckdb` through the `mise.toml` env. Also `OrderedMap` and tests for the time filters and the engine version.
- The Dockerfile now builds with Go 1.25 and cgo.

Follow-ups:
- [02](upgrade-pebble-duckdb/issues/02-duckdb-2-0-0-release.md): move to DuckDB 2.0.0 (due 2026-10-21).
- [03](upgrade-pebble-duckdb/issues/03-pebble-value-separation.md): benchmark Pebble value separation.

The logs map now records that duckdb-go has no real 2.0 build yet.
