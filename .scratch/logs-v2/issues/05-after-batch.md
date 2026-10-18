# 05: after_batch for Match monitors

**What was built:** a batch column in the hot tables, logs_late and Parquet, batch_max in the manifest, and logs/query with after_batch returning the Logs ingested after a cursor. Spec: [spec.md](../spec.md), Phase 4 (EventStore).

**Status:** resolved

Built and reviewed as unit B3 of the Logs v2 build (implement, review and verify, fixes, recheck). Decisions are in the spec's Implementation decisions under the B3 heading; findings left for later are in [carry-over.md](../carry-over.md).

## Comments

### 2026-10-11 resolved

Merged into `feature/logs-v2`; the full suites passed at the end of its batch.
