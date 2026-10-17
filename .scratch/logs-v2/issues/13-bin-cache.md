# 13: The bin cache

**What was built:** an in-memory LRU of closed chart bins keyed by Query and manifest generation, so a run computes only missing bins and the live span; plus five B4 lows. Spec: [spec.md](../spec.md), Phase 5 step 5.

**Status:** resolved

Built and reviewed as unit B5 of the Logs v2 build (implement, review and verify, fixes, recheck). Decisions are in the spec's Implementation decisions under the B5 heading; findings left for later are in [carry-over.md](../carry-over.md).

## Comments

### 2026-10-17 resolved

Merged into `feature/logs-v2`; the full suites passed at the end of its batch.
