# 06: Reject a non-positive `ingest_max_waiting` at startup

Status: ready-for-agent

**Spec:** [spec.md](../spec.md), "Waiting-request limit" and user story 19
**Found in:** code review of `960127e...a04a685` (2026-10-01)
**Area:**
- `event_store/config/config.go` (`applyFileConfig`)
- `event_store/config/config_test.go` (new: the package has no tests yet)

## Problem

`ingest_max_waiting` is copied from the YAML file without a check. A value of `0` or less makes Event ingest answer `503` to every request. EventStore starts normally, and the only symptom is SDKs losing every Event.

## What to build

In `applyFileConfig`, return an error when `ingest_max_waiting` is set to `0` or less, for example `ingest_max_waiting must be positive, got 0`. `Load` already stops the process on any error from `applyFileConfig`, so a bad value fails at startup.

## Tests

- `0` and `-1` return an error.
- A positive value is applied.
- Leaving the key out keeps the default of 10,000.

## Acceptance

- The tests pass under `go test ./...`.
