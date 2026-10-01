# Project status

Updated: 2026-10-01

SolidTrace: self-hosted error tracking with two services, Console (Rails) and EventStore (Go), and embedded databases. Terms are in [CONTEXT.md](../CONTEXT.md).

## Where things stand

- **Logs**: planning only, no code yet. 4 research tickets are done. The decision tickets start now. The goal is a v1 spec plus first-milestone tickets.
- **Prod-readiness**: [review.md](prod-readiness/review.md) lists about 9 gaps. 5 are filed as tickets (013 and 014 were added from the ingest design). 013, re-archiving overwrites Parquet, is resolved (2026-09-30). 014, Console messages stuck in `processing`, is resolved (2026-09-30). Query API auth (§2), acknowledged Events vanishing (§4), the concurrent first-event 500 (§5) and Sentry protocol correctness (§6) are fixed.
- **Integration notification**: the spec is ready for an agent. It has no tickets yet.
- **Ingest write path**: reopened. 02 and 03 are resolved (2026-09-30). A code review on 2026-10-01 filed 04–06. 04, a lone bad DuckDB row stalling Event processing, is resolved (2026-10-01). 05 and 06 are open. Event ingest stores the Event in Pebble before the `200`. Event processing, a single worker, catches up Issues, DuckDB and the Console from per-Project processing cursors in Pebble. Review §4 is fixed. Accepted gap: a crash can lose an `issue_created`.
- **Upgrade Pebble and DuckDB**: merged. EventStore now uses Pebble v2.1.7 and the DuckDB 2.0 alpha engine (`v2.0.0-alpha43385`), linked as a shared library. Two follow-ups wait on it.

## Next

1. [Ingest write path 05: backlog log for every Project](ingest-write-path/issues/05-backlog-log-every-project.md), then 06
2. [Logs 05: prod-readiness gaps for logs](logs/issues/05-prod-readiness-gaps-for-logs.md) (grilling)
3. [Prod-readiness 011: Pebble memtable size](prod-readiness/issues/011-pebble-memtable-size.md) (benchmark task)
4. [Prod-readiness 012: Sentry compatibility tests](prod-readiness/issues/012-sentry-compatibility-tests.md) (task)
5. [Integration notification spec](integration-notification/spec.md): split it into tickets

## Efforts

| Effort | Done / open | Start here |
|---|---|---|
| Logs | 4 / 9 | [map.md](logs/map.md) |
| Prod-readiness | 3 / 3 filed | [review.md](prod-readiness/review.md) |
| Integration notification | spec only | [spec.md](integration-notification/spec.md) |
| Ingest write path | 4 / 2 | [05](ingest-write-path/issues/05-backlog-log-every-project.md) |
| Upgrade Pebble and DuckDB | 1 / 2 | [02](upgrade-pebble-duckdb/issues/02-duckdb-2-0-0-release.md) |
| Logging-old | closed (wontfix) | replaced by the logs map; reference only |

**Logs path** (a ticket opens when the ones before it are done):
05, 06, 07 (open now) → 08, 09, 11 → 10, 12 → 13 (write the v1 spec)

## Last session (2026-10-01)

A code review of ingest write path 03 filed 04–06. Implemented [04](ingest-write-path/issues/04-duckdb-row-isolation.md): a failed DuckDB append is now classified by asking DuckDB (`Ping()`), not by comparing rows, so a lone bad row is skipped instead of stalling every Project. Content retries back off. duckdb-go returns no typed error for a rejected value, so an append-only failure such as the memory limit still counts as a content error. The ticket's Comments list the details.

## Earlier session (2026-09-30)

Implemented [ingest write path 03](ingest-write-path/issues/03-event-processing.md): Event processing in the new `event_store/processing` package, replacing the DuckDB ingester. One change to the spec: startup no longer moves the processing cursor to DuckDB's newest row. It records that row as an in-memory mark, so a crash after the DuckDB commit still resends the batch's Console messages (story 27). The ticket's Comments list the details.

## Earlier session (2026-09-27)

Mostly housekeeping. Tickets stay as local Markdown, and this file is the status page. Closed logging-old: its spec and all 17 tickets are now `wontfix`, and each one points to the logs map. No logs ticket was claimed or resolved. Then fixed review §6 (Sentry protocol correctness) in `event_store/ingest`. Filed 012 for Sentry compatibility tests. Found that SDKs retry no HTTP status and pause 60 s on a 429, so a full ingest channel now returns 503. Fixed §5, the concurrent first-event race.

Upgraded EventStore and merged it to `main`, recorded in [ticket 01](upgrade-pebble-duckdb/issues/01-upgrade-pebble-and-duckdb.md):
- Pebble v2.1.7, with the format pinned to `FormatValueSeparation` and value separation off.
- DuckDB 2.0 alpha: the `v2.20000.0-6.preview` driver built with `-tags=duckdb_use_lib`, linked to a pinned `libduckdb` from `event_store/scripts/fetch-duckdb` through the `mise.toml` env. Also `OrderedMap` and tests for the time filters and the engine version.
- The Dockerfile now builds with Go 1.25 and cgo.

Follow-ups:
- [02](upgrade-pebble-duckdb/issues/02-duckdb-2-0-0-release.md): move to DuckDB 2.0.0 (due 2026-10-21).
- [03](upgrade-pebble-duckdb/issues/03-pebble-value-separation.md): benchmark Pebble value separation.

The logs map now records that duckdb-go has no real 2.0 build yet.

Designed the ingest write path, picking up the unfinished 2026-09-26 grilling session.
- **Spec:** [spec.md](ingest-write-path/spec.md), `ready-for-agent`. It fixes review §4.
- **Tickets:** 02 and 03 in that effort, plus prod-readiness 013 and 014 (`needs-triage`). 013 was triaged on 2026-09-30, see below.
- **Decisions:**
  - Event ingest only stores the raw Event in Pebble.
  - Event processing owns the Issue lifecycle.
  - Processing cursors live in Pebble. Startup notes how far DuckDB got past each one (changed in 03 from moving the cursor).
  - Console messages are sent at least once.
- **Accepted gap:** a crash can lose an `issue_created`. The spec lists the fixes considered.

## Triage (2026-09-30)

[Prod-readiness 013](prod-readiness/issues/013-archive-overwrites-parquet.md) is now `ready-for-agent`, with an agent brief.
- The overwrite is reproduced. The first draft's trigger was wrong: today, Late Events are stranded in `events_hot`, not lost.
- **Decisions:**
  - `archive_events` carries a cutoff date. Each run moves every Event dated on or before it.
  - Each run writes new `data-<run id>.parquet` files and never overwrites a file.
  - Files are staged as `.tmp`, then the delete commits, then the files are renamed. A check at startup and at the start of each run resolves leftover `.tmp` files.
  - It ships before ingest ticket 03. No glossary change.

[Prod-readiness 014](prod-readiness/issues/014-stuck-processing-messages.md) is now `ready-for-agent`, with an agent brief.
- Confirmed from the code. Also found that the every-second job has no concurrency limit, so overlapping runs can send a message twice.
- **Decisions:**
  - Drop the `processing` claim. A crash leaves the message `pending`, so it is retried.
  - Add `limits_concurrency to: 1` with `on_conflict: :discard` to `EventStoreMessageProcessorJob`.
  - Making the status change and the handling one transaction is ruled out: the message and Solid Queue use separate SQLite files.

## Implementation (2026-09-30)

[Prod-readiness 013](prod-readiness/issues/013-archive-overwrites-parquet.md) is resolved. `ArchiveEventsUpTo(cutoff)` writes new `data-<run id>.parquet` files through `.tmp` staging, and recovery runs at startup and at the start of each run. The ticket's comments list what was added beyond the brief and the one edge case that was accepted.

[Prod-readiness 014](prod-readiness/issues/014-stuck-processing-messages.md) is resolved (`a7c06b2`). The processor job no longer sets `processing`, so a crash leaves the message processable. `processable` also picks up rows already stuck in `processing`. `limits_concurrency to: 1` with `on_conflict: :discard` stops overlapping runs. The concurrency lock expires after Solid Queue's default of 3 minutes, which is accepted under at-least-once delivery.

[Ingest write path 02](ingest-write-path/issues/02-mutex-event-writer.md) is resolved. `PebbleWriter.WriteEvent` stores each Event under one lock before the `200`. `ingest_max_waiting` caps waiting requests with a `503`. The Pebble channel, the Pebble ingester and their settings are gone. The ticket's comments list the accepted shutdown edge cases.
