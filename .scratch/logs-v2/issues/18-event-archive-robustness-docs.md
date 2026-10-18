# 18: Event archive robustness and object store docs

**What was built:** a failed view rebuild retries and reads answer 503 instead of dropping days; ingest starts before the bucket is read; a warning when object_store is removed; docs/logs/object-store.md. Spec: [spec.md](../spec.md), Phases 5 and 6.

**Status:** resolved

Built and reviewed as unit B7 of the Logs v2 build (implement, review and verify, fixes, recheck). Decisions are in the spec's Implementation decisions under the B7 heading; findings left for later are in [carry-over.md](../carry-over.md).

## Comments

### 2026-10-17 resolved

Merged into `feature/logs-v2`; the full suites passed at the end of its batch.
