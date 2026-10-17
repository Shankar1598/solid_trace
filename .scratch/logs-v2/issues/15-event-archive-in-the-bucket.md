# 15: The Event archive in the bucket

**What was built:** archive files uploaded after ArchiveEventsUpTo and their local copies removed; the events view reads an explicit file list. Spec: [spec.md](../spec.md), Phase 5 step 6.

**Status:** resolved

Built and reviewed as unit B6 of the Logs v2 build (implement, review and verify, fixes, recheck). Decisions are in the spec's Implementation decisions under the B6 heading; findings left for later are in [carry-over.md](../carry-over.md).

## Comments

### 2026-10-17 resolved

Merged into `feature/logs-v2`; the full suites passed at the end of its batch.
