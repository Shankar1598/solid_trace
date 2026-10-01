# 04: A bad DuckDB row must not stall Event processing, and a brief DuckDB error must not skip a row

Status: resolved

**Spec:** [spec.md](../spec.md), "Event processing" (the DuckDB isolation line) and user stories 15 and 17
**Found in:** code review of `960127e...a04a685` (2026-10-01)
**Area:**
- `event_store/processing/processor.go` (`indexRows`, `attempt`)
- `event_store/processing_test.go`

## Problem

1. **A bad row alone in its batch stalls every Project.** Spec story 15: "one bad payload doesn't block the rest of its Project".
   - `indexRows` decides whether a row is bad by comparing it with the other rows in its batch. A row that fails while others succeed is bad. If every row fails, it's treated as a store error (`len(failed) == len(rows)`).
   - A bad row alone in its batch therefore counts as a store error. This is the normal case at low traffic.
   - `retry` loops in place and never re-reads Pebble, so the batch never grows. Ticket 03's comment says it waits "until another Event arrives", but that isn't true.
   - There is one worker, so every Project stops being processed.
2. **A brief DuckDB error can skip a row.** Spec story 17: transient errors are "retried without skipping any Event".
   - On the per-row path, a row error while `Ping()` succeeds counts as a content error.
   - `attempt` then runs its 3 retries back to back with no backoff.
   - A short-lived error can therefore exhaust the retries, and the row is skipped for good.

## What to build

1. **Classify every append failure by asking DuckDB, not by comparing rows.** One helper does this for both the batch append and the per-row appends: if the append fails and `Ping()` succeeds, it's a content error; otherwise it's a store error.
   ```go
   func (p *Processor) writeRows(rows []models.Event) error {
   	err := p.index.WriteBatch(rows)
   	if err != nil && p.index.Ping() == nil {
   		return contentError{err}
   	}
   	return err
   }
   ```
2. **Rewrite `indexRows` around the helper:**
   - Try the batch under `retry`.
   - On a content error, append the rows one at a time, each under `attempt`.
   - Store errors keep retrying in place, as today.
3. **Back off between content retries in `attempt`.** Use the same doubling from `minBackoff` as `retry`, and return `ErrStopped` on `Stop`. Three retries take about 0.7 s, so one poison Event never holds up the worker for long.
4. **Fix the doc comments** on `indexRows` and `attempt` to match the new behaviour.

Committing one row at a time keeps UUID order, so the startup "indexed up to" mark (`NewestHotUUIDAfter`) still works.

**Known limit, decide during implementation:** `Ping()` can't tell a bad row from an error that only hits real appends, such as DuckDB running out of its memory limit while `SELECT 1` still works. The stronger classifier is DuckDB's error type: conversion or out-of-range means a bad row; out-of-memory or IO means a store error.
- First check whether duckdb-go `v2.20000.0-6.preview` returns a typed `*duckdb.Error` from the Appender for the far-future timestamp case.
- If it does, classify by type instead of by `Ping()`.
- If it doesn't, keep `Ping()` and record the limit in this ticket's Comments.

## Tests

- **New:** an Event with a timestamp past DuckDB's range (as in `TestEventWhoseRowDuckDBRejectsIsSkippedAndTheRestIndexed`) is posted alone and processed.
  - Processing returns. Give the test a deadline: today it hangs.
  - The Event is skipped and logged.
  - A good Event of the same Project posted afterwards is indexed.
- **New:** the same lone bad row in Project A doesn't stop Project B's Events from being indexed.
- **Keep** the existing case of a bad row with good neighbours.
- **Not testable at this seam:** a transient DuckDB error during the per-row path. `Processor` takes a concrete `*storage.DuckDBWriter`, and the spec's only fake is `IssueRepository`. Record this in Comments rather than adding a seam just for it.

## Acceptance

- The new tests pass under `go test ./...` without sleeps.
- No code path treats "every row failed" as a store error.
- `attempt` waits between content retries and stops promptly on `Stop`.

## Comments

**2026-10-01, resolved.**

- **Classifier: `Ping()` is kept.** duckdb-go `v2.20000.0-6.preview` doesn't return a typed `*duckdb.Error` for the far-future timestamp. The Appender fails on the Go side before DuckDB sees the value: `could not append row: failed to set value: ... conversion error: cannot convert 285200616, minimum: -290307, maximum: 294246` (a `*fmt.wrapErrors`). **Known limit:** an error that hits only appends, such as DuckDB reaching its memory limit while `SELECT 1` still works, counts as a content error. Each row is then retried 3 times and skipped. The `writeRows` doc comment records this.
- **Built as asked:** `writeRows` classifies both the batch append and the per-row appends. `indexRows` tries the batch under `retry`, and on a content error appends each row under `attempt`. Nothing compares rows any more.
- **`attempt` backs off between content retries** (100 ms doubling) through a new `wait` helper, which it shares with `retry`.
- **Changed from the brief, found in code review:** `attempt` is also used for Issue work. A plain `ErrStopped` there would abandon a batch whose earlier Issues were already created, so their `issue_created` would be lost. Stop during a content backoff now returns `errStoppedBeforeRetry`, which wraps `ErrStopped`. Issue work treats it like the existing stop check: the batch finishes with the Events before it. During the per-row DuckDB appends it still abandons the batch, as the brief says. The rows already committed are in UUID order, so the startup mark still works. The `processBatch` doc comment now says so.
- **Side effect:** a poison Event in Issue work (such as `[]` or `null`) now takes about 0.7 s before it is skipped, instead of no time at all.
- **Tests:**
  - Added a lone far-future Event: it is skipped and logged, and a later good Event is indexed.
  - Added Project 456's Event being indexed while Project 123 has a lone bad row.
  - Both run under a 10 s timeout. Before the fix both hung.
  - Test helpers: `postEventTo`, `storeStatusTo` and `seedProjectKey` take a Project, and `captureLogs` swaps the global logger for the test.
- **Not tested at this seam:** a transient DuckDB error during the per-row appends. `Processor` takes a concrete `*storage.DuckDBWriter`, and the spec's only fake is `IssueRepository`. Stop during a content backoff is also untested, since it needs timing.
