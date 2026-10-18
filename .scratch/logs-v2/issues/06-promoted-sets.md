# 06: Promoted sets fix

**What was built:** a new day's keys are marked partway when the day already has Logs; promoted sets are loaded inside begin() instead of an in-memory cache. Spec: [spec.md](../spec.md), Phase 5 (follow-up).

**Status:** resolved

Built and reviewed as unit B3b of the Logs v2 build (implement, review and verify, fixes, recheck). Decisions are in the spec's Implementation decisions under the B3b heading; findings left for later are in [carry-over.md](../carry-over.md).

## Comments

### 2026-10-11 resolved

Merged into `feature/logs-v2`; the full suites passed at the end of its batch.
