# 05: Backlog log must cover every Project with unprocessed Events

Status: ready-for-agent

**Spec:** [spec.md](../spec.md), "Backlog" ("logs each Project whose oldest unprocessed Event is older than a threshold") and user story 13
**Found in:** code review of `960127e...a04a685` (2026-10-01)
**Area:**
- `event_store/processing/processor.go` (`logBacklog`, the `known` field)
- `event_store/processing_test.go`

## Problem

`logBacklog` only walks `Processor.known`, and a Project joins `known` only when the worker takes it from `pending`. After a restart, the worker can be stuck retrying its first Project, for example while DuckDB is down. In that case every other backlogged Project never appears in the backlog log. The same happens to a Project whose first Event arrives while the worker is stuck. The backlog log exists for exactly these situations.

## What to build

1. In `logBacklog`, loop over `p.events.ProjectsWithEvents()` instead of `known`. It reads one key per Project and runs at most once a minute.
   - If it fails, log `Backlog check failed` once and return.
2. Delete the `known` field, its initialisation in `New`, and the line in `ProcessUntilCaughtUp` that fills it.

## Tests

Export a hook to run the backlog check directly, or move `backlogThreshold` into a field that tests can lower. Pick whichever is smaller. Cover:
- Two Projects have Events older than the threshold, and the worker has taken neither from `pending`. Both are logged.
- A Project whose Events are all processed is not logged.

## Acceptance

- The tests pass under `go test ./...`.
- `known` is gone.
