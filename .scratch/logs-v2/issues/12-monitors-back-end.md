# 12: Monitors: data, evaluation and notifications

**What was built:** LogMonitor and its states and runs, MonitorSchedulerJob and MonitorEvaluation (Threshold and Match, re-arm after timeouts), Monitor rows in the outbox with ordered chains, Slack, PagerDuty trigger and resolve, email. Spec: [spec.md](../spec.md), Phase 4 (Console back end).

**Status:** resolved

Built and reviewed as unit A4a of the Logs v2 build (implement, review and verify, fixes, recheck). Decisions are in the spec's Implementation decisions under the A4a heading; findings left for later are in [carry-over.md](../carry-over.md).

## Comments

### 2026-10-17 resolved

Merged into `feature/logs-v2`; the full suites passed at the end of its batch.
