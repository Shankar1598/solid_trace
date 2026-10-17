# Project status

Updated: 2026-10-17

SolidTrace: self-hosted error tracking with two services, Console (Rails) and EventStore (Go), and embedded databases. Terms are in [CONTEXT.md](../CONTEXT.md).

**Stage:** 0.1 prototype, not a production release. The goal is to build it and see the end result. Production hardening is deferred until after the prototype.

**Rules for agents:**
- Don't pick up deferred hardening unprompted: the open items in [prod-readiness review.md](prod-readiness/review.md) (§1, §9, §10: tiered storage, retention, `mode=ro`, the `ValidateKey` cache, compose host networking, DSN, ingest limits, metrics, CI), 011 and 012. Each is marked deferred or `wontfix` there. A change that would make one of them worse is still worth a flag.
- EventStore runs DuckDB 1.5.6, bundled with the driver (upgrade-pebble-duckdb 02, 2026-10-02). There is no alpha library or `duckdb_use_lib` tag any more. A shell opened before that change may still export `GOFLAGS=-tags=duckdb_use_lib`; open a new one.
- Work happens in the main checkout, now on `feature/logs-v2` (the logs v2 integration branch, built on `feature/logs-v1`). One worktree, `../solid_trace-v2-go` on `feature/logs-v2-go`, held the EventStore track; it is level with `feature/logs-v2`.
- Read [ADR 0001](../docs/adr/0001-eventstore-writes-console-tables.md) before touching `event_store/storage/sqlite.go` or a Console enum or table that EventStore writes. A change to one must change the other in the same commit.
- The logs map is the source of truth for logs; trust it over the counts below.
- Benchmark and probe runs must finish in under a minute on the dev box.

## Where things stand

- **Logs v2**: implemented on `feature/logs-v2` (2026-10-10 to 17): charts in the explorer, developer UX, Dashboards, Monitors with Slack, PagerDuty and email, Log Parquet and the Event archive in S3 or GCS (tested on MinIO), and a bin cache. 20 of 24 [tickets](logs-v2/issues/) resolved, including a whole-branch review; Rails, vitest, typecheck, all 69 Playwright flows and the Go suite pass. Open: the looks pass and a merge (`ready-for-human`), a real GCS run (`needs-info`), leftover lows (`ready-for-agent`).
- **Logs**: v1 is implemented on `feature/logs-v1` (2026-10-03): 19 of 20 [tickets](logs-v1/issues/) resolved, code-reviewed, with the review fixes merged (`0eecf81`). It is not yet merged into `feature/logging` or `main`, and not yet clicked through in a browser. M1-10 (load test) is the only open ticket. Choices that depart from the spec are in its [Implementation decisions](logs-v1/spec.md#implementation-decisions); the review's fixes are in its Comments.
- **Prod-readiness**: closed for 0.1. [review.md](prod-readiness/review.md) items are deferred, except §7, filed as [015](prod-readiness/issues/015-issue-list-counts-and-pagination.md), resolved (2026-10-02). §8 is decided in [ADR 0001](../docs/adr/0001-eventstore-writes-console-tables.md). 011 and 012 are `wontfix` for now. The fixed items (§2, §4, §5, §6, 013, 014) stay fixed.
- **Integration notification**: resolved and merged (2026-10-02, `9a488c6`). One Console module sends new-Issue, threshold and assignment notifications through the outbox, with recorded outcomes, bounded retries and cleanup. EventStore's Event count honours the time window. Review findings left open are listed in the [spec](integration-notification/spec.md)'s Comments.
- **Ingest write path**: reopened. 02 and 03 are resolved (2026-09-30). A code review on 2026-10-01 filed 04–06. 04, a lone bad DuckDB row stalling Event processing, is resolved (2026-10-01). 05 is `wontfix` (2026-10-02). 06 is resolved (2026-10-02). Event ingest stores the Event in Pebble before the `200`. Event processing, a single worker, catches up Issues, DuckDB and the Console from per-Project processing cursors in Pebble. Review §4 is fixed. Accepted gap: a crash can lose an `issue_created`.
- **Upgrade Pebble and DuckDB**: EventStore uses Pebble v2.1.7 and DuckDB 1.5.6 (`v2.10506.0`, bundled). [02](upgrade-pebble-duckdb/issues/02-duckdb-2-0-0-release.md) moved it back from the 2.0 alpha on 2026-10-02, on `feature/logging`, not yet on `main`. [03](upgrade-pebble-duckdb/issues/03-pebble-value-separation.md) (Pebble value separation) is open.

## Next

0. **Logs v2** ([spec](logs-v2/spec.md), [tickets](logs-v2/issues/)): built and reviewed; next with the user: [20, the looks pass](logs-v2/issues/20-looks-pass.md), [21, a real GCS run](logs-v2/issues/21-real-gcs-check.md) (needs their bucket and HMAC key) and [23, the merge](logs-v2/issues/23-merge-logs-v2.md). [22](logs-v2/issues/22-leftover-low-findings.md) lists the leftover lows.

1. ~~Click through the Logs explorer in a browser.~~ Done in Logs v2 unit A0 with Playwright; rough edges in [the click-through notes](logs-v2/research/v1-click-through.md).
2. Merge `feature/logs-v1` into `feature/logging` (then `main`) when happy.
3. [M1-10](logs-v1/issues/M1-10-load-test-on-target-vm.md) (load test) on the 4 vCPU / 16 GB VM: `ready-for-human`, every blocker resolved. Things to measure are in the ticket and in M1-04's and M2-01's Comments.
4. Flagged, not fixed: Log ingest reads a Project's Retention from Console's SQLite on every request, uncached (belongs with the deferred `ValidateKey` cache).

## Efforts

| Effort | Done / open | Start here |
|---|---|---|
| Logs | 16 / 0 | [map.md](logs/map.md) |
| Logs v1 | 19 / 1 | [spec.md](logs-v1/spec.md) |
| Logs v2 | 20 / 4 | [20](logs-v2/issues/20-looks-pass.md) |
| Prod-readiness | 6 / 0 | [review.md](prod-readiness/review.md) |
| Integration notification | 5 / 0 | [spec.md](integration-notification/spec.md) |
| Ingest write path | 6 / 0 | [spec.md](ingest-write-path/spec.md) |
| Upgrade Pebble and DuckDB | 2 / 1 | [03](upgrade-pebble-duckdb/issues/03-pebble-value-separation.md) |
| Phone layout | 0 / 1 | [01](phone-layout/issues/01-phone-layout.md) |
| Logging-old | closed (wontfix) | replaced by the logs map; reference only |

**Logs path** (a ticket opens when the ones before it are done):
01–16 (done) → logs v1 tickets M1-01 … M3-04

## Last session (2026-10-10 to 17, logs v2)

Planned Logs v2 with the user (Axiom as the reference) and built it from [the spec](logs-v2/spec.md) with workflow batches: a Console track in the main checkout and an EventStore track in a worktree, each unit implemented, reviewed, verified and fixed, then merged with `--no-ff`. On 2026-10-16 the user asked for less token spend: reviews became one review plus one verify, high and medium findings only, with 1 or 2 screenshots per UI unit and no visual polish (looks come later, with the user). A whole-branch review closed the build. Every unit's decisions are in the spec's Implementation decisions; leftovers in [carry-over.md](logs-v2/carry-over.md). Run notes for the orchestrator live in `~/.cache/solid-trace/logs-v2-notes/runs.md`.

## Earlier session (2026-10-02 to 03, logs v1 implementation)

Ran `/implement-spec` on the logs v1 spec, on integration branch `feature/logs-v1`, with at most two sub-agents at a time (the user's limit for this box). Each ticket was built test-first in its own worktree and merged with `--no-ff`; the worktrees are removed.
- **Resolved:** 19 tickets, including **M1-11 (remove Angie)**, added mid-run at the user's request. Console now serves through Thruster and EventStore ingests on TCP 4000.
- **Code review:** a two-axis review (standards and spec) found 8 spec problems, among them a long read stalling Log processing, `contains` missing arrays, a disk-limit default that could delete every Log file, and a clock step back losing acknowledged batches. All were fixed, along with the standards cleanups, in `0eecf81`.
- **Decided by the user (2026-10-03):** Console listens on port 3000; the compose dependency cycle is fixed; ingest bodies are decoded only after the `ingest_max_waiting` gate; every Project edit is admin-only. They are recorded in the spec's Implementation decisions.
- **Lost:** the overnight restart wiped the shared agent notes in `/tmp`. They were rebuilt in `~/.cache/solid-trace/logs-v1-notes/` (setup, briefs, review), outside the repo.

## Earlier session (2026-10-02, logs v1 tickets)

Wrote the logs v1 tickets with `/to-tickets` and resolved [logs 13](logs/issues/13-write-v1-spec-and-first-milestone-tickets.md). The user asked for all three milestones, not only the first, with tickets named by milestone (`M1-01` … `M3-04`).
- **Changed in the spec:** Project deletion is detected when a Project's `projects` row vanishes, not its keys. Keys are hard-deleted, so the old rule would have wiped a Project's Logs when its last key was deleted. Console has no Project destroy route yet.
- **M1-10** (load test) is `ready-for-human`: it needs the 4 vCPU / 16 GB VM and runs past the dev box's one-minute limit.
- No code changed.

## Earlier session (2026-10-02, logs 10 second pass, 15, 16)

[Logs 10](logs/issues/10-storage-architecture-and-retention.md) resolved again (grilling); [ADR 0005](../docs/adr/0005-logs-per-project-hot-tables-promote-at-ingest.md) supersedes ADR 0004, and ADR 0003 gains a note.
- **Decided:** one hot table per Project with key-named promoted columns (up to 100 (type, key) pairs, at ingest); popularity counted by the Log processor per Project per UTC day of `time`; lazy daily re-rank on the first Log of a day; demoted columns dropped at the 01:00 export; a shared MAP-only `logs_late` for out-of-window Logs; the compiler learns columns only from the promoted-set table.
- **SQL mode's `logs` view** now covers the bar's Project and time range (a convenience, not a boundary). **SQL query** in `CONTEXT.md` is amended; tickets 11 and 12 carry pointer comments.
- Reasoned, not benchmarked. The first-milestone load test covers per-Project tables, the daily switch and checkpoints at full scale.
- The map's last two v1 fog patches became tickets and were resolved the same session: [15](logs/issues/15-how-log-queries-run.md) (no per-query timeouts; routing, cancel and caps left to the implementer) and [16](logs/issues/16-explorer-building-blocks.md) (non-Log results as a table only, URL state with SQL never run on load, stream paging, summary line, local time, libraries left to the implementer). Ticket 13 is now the only open logs ticket.

## Earlier session (2026-10-02, DuckDB 1.5.6 and logs 14)

Worked in the main checkout, now on `feature/logging`; all worktrees were removed (prototype branches remain as branches). `main` was merged into `feature/logging`.
- **[Upgrade-pebble-duckdb 02](upgrade-pebble-duckdb/issues/02-duckdb-2-0-0-release.md) resolved** (`9168867`): EventStore runs DuckDB 1.5.6; the alpha wiring is gone; VARIANT layouts in `logsbench` are parked for 2.0. Dev DuckDB data written by the alpha was moved to the session scratchpad, not deleted.
- **[Logs 14](logs/issues/14-benchmark-storage-design-on-1-5-5.md) resolved:** findings in [research/14](logs/research/14-storage-design-bench.md). MAP still beats JSON; stalls remain, no crash; the hot table plateaus; export is fine. The 5 s day target was missed because the hot table has no promoted columns.
- **Decided with the user:** promote keys at ingest, 100 per Project, re-ranked nightly by popularity; the promoted set lives in `logs.duckdb`; plain Parquet files, no DuckLake (probed with a SQLite catalog).
- **Logs 10 reopened** for the remaining promotion questions and a new ADR superseding 0004.
- These commits are on `feature/logging`, not on `main`.

## Earlier session (2026-10-02, logs 12 query modes)

[Logs 12](logs/issues/12-query-builder-ux.md) is resolved (prototype). The user chose a `Builder | Query | SQL` switch. The Builder is filter chips + search only. Query → Builder works only when the Builder can show the Query, otherwise it offers to discard. SQL starts from the compiled SQL and is one-way. **Builder** added to `CONTEXT.md`.
- Prototype: branch `prototype/logs-query-modes` (`7d19a45`), worktree `.claude/worktrees/logs-query-modes`. Its `storage/sqlite/development` is a symlink to the main checkout's dev databases, which now hold the `db:seed` data (`admin@solidtrace.local` / `password123`).

## Earlier session (2026-10-02, logs 09 benchmark)

[Logs 09](logs/issues/09-benchmark-storage-layouts.md) is resolved. A throwaway harness, `event_store/bench/logslayouts`, and the findings ([research/09](logs/research/09-storage-bench-reduced.md)) are on branch `feature/logging` in the logs worktree, not on `main`.
- **Results:**
  - Typed MAPs rank first, then JSON text, then VARIANT without shredding.
  - Shredded VARIANT is unusable on the 2.0 alpha: slow checkpoints and queries, one heap-corruption crash, and a database-invalidating filter error on default-shredded Parquet.
  - Local Parquet gains nothing from async read-ahead.
- **Run:** cut to about 2 minutes per layout at the user's request, about 10 minutes for the whole benchmark.
- **Next for logs:** [10](logs/issues/10-storage-architecture-and-retention.md) (storage and retention grilling) or [12](logs/issues/12-query-builder-ux.md).
- `main` was merged into `feature/logging` on 2026-10-02. The logs tracker conflicts were resolved in favour of `main`, whose map is the live one. The branch's 2026-09-27 MVP scoping stays in its history only.

## Earlier session (2026-10-02, prod-readiness 015)

[Prod-readiness 015](prod-readiness/issues/015-issue-list-counts-and-pagination.md) is resolved (`8b2af22`). Event processing keeps `times_seen`, `first_seen_at` and `last_seen_at` on `issues`, with a replay guard in `project_seen_cursors`. The Issues list is paginated 25 per page, sorts by last or first seen in SQL, and makes no per-Issue EventStore call. The ticket's comment lists what was added beyond the brief.
- Toolchain: run Ruby and Go through `mise exec --`. Run the Go and Rails suites one after the other with `RAILS_ENV=test bin/rails db:reset` in between, because the Go tests leave messages in the shared test message queue.

## Earlier session (2026-10-01, prod-readiness close)

Decided to treat the project as a 0.1 prototype and closed the rest of prod-readiness, keeping only what would change the direction of development.
- **§8:** accepted EventStore writing Console's issue tables. Recorded in [ADR 0001](../docs/adr/0001-eventstore-writes-console-tables.md), the first ADR in `docs/adr/`. The generated enum constants and the startup schema-version check are not done.
- **§7:** filed as [015](prod-readiness/issues/015-issue-list-counts-and-pagination.md), `ready-for-agent` (2026-10-02). EventStore writes the counts directly, with a replay guard in SQLite; no backfill, the test data is wiped. FTS5 and the dead filter controls are out of scope.
- **Closed for now:** 011 and 012 are `wontfix` with a "reopen after the prototype" comment. Review §1, §9 and §10 are marked deferred, and §9 points at logs 10. The `log` envelope fixtures note stays in 012.
- No code changed.

## Earlier session (2026-10-01, ingest write path 04)

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
