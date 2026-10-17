# 03: Charts in the Logs explorer

**What was built:** lib/charts/series.ts and QueryChart (line, area, stacked bars, stat), the viz switch in the URL, legend toggles and drag to zoom; a new Summarize defaults to by bin_auto(time). Spec: [spec.md](../spec.md), Phase 1 (Console).

**Status:** resolved

Built and reviewed as unit A1 of the Logs v2 build (implement, review and verify, fixes, recheck). Decisions are in the spec's Implementation decisions under the A1 heading; findings left for later are in [carry-over.md](../carry-over.md).

## Comments

### 2026-10-11 resolved

Merged into `feature/logs-v2`; the full suites passed at the end of its batch.
