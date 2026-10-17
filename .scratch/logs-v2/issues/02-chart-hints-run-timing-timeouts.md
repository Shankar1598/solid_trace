# 02: Chart hints, run timing and timeouts in EventStore

**What was built:** table answers carry agg and bin_seconds per column and fill_gaps, from and to; every query answer carries elapsed_ms; timeout_seconds cancels a scan on the server (504). Spec: [spec.md](../spec.md), Phase 1 (EventStore).

**Status:** resolved

Built and reviewed as unit B1 of the Logs v2 build (implement, review and verify, fixes, recheck). Decisions are in the spec's Implementation decisions under the B1 heading; findings left for later are in [carry-over.md](../carry-over.md).

## Comments

### 2026-10-11 resolved

Merged into `feature/logs-v2`; the full suites passed at the end of its batch.
