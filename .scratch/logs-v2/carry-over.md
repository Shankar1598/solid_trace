# Logs v2: review findings carried over

Findings that a unit's review confirmed but left for later, and where each one goes. Each unit's full finding, with its failure scenario, is in the orchestrator's workflow results; the summary here is enough to act on.

Status: ready-for-agent

## Scheduled into a later unit

| From | Finding | Severity | Goes to |
|---|---|---|---|
| A4a, A4b | Turning off or deleting a firing Monitor sends no resolve, so a PagerDuty incident it opened stays open (the delete dialog says so) | medium | A5 |
| A4b | Run now skips MonitorEvaluationJob's limits_concurrency, so a scheduled run at the same moment can send a second firing message | medium | A5 |
| A4b | The list and Show badge check last_error before firing groups, so a firing Monitor whose last run failed shows only "Error" (A4b-review-1-1) | low | A5 |
| A4b | The flow checks the page says OK after the higher threshold but not that the Slack stub got "Resolved" (A4b-review-1-2) | low | A5 |
| A4a | Assigning integrations on a persisted Monitor inserts join rows before validation, so only one path enforces the Organization (A4a-review-1-5) | low | A5 |
| A4a | A Threshold run ignores table.truncated; past 10,000 groups each run sees a different subset (A4a-review-1-3) | low | A5 |
| A4a | A Threshold run already in flight can undo the kind-change resolve (after_update :resolve_groups) (A4a-recheck-2-1) | low | A5 |
| A2c | `seeking` isn't tied to the run "Logs around this one" started (A2c-review-1-3); the range shortens whenever more than one page is newer than the Log (A2c-recheck-2-1); puma.rb still says explorer runs have no limit (A2c-review-1-4) | low | A5 |
| A2c | ProjectKey#dsn on the Project page is still the hard-coded localhost:3000 DSN | low | A5 |
| A2c | Six flows (charts ×4, logs-v1-inspector ×2) fail while a probe Log sits in the dev data's last 24 h; flows should not depend on the dev Project holding only the dummy Logs | medium | A5 |
| B6 | With a bucket, a failed events-view rebuild after ArchiveEventsUpTo commits hides that run's Events (200 with fewer rows) until the next daily archive or a restart; nothing retries (B6-verify-1-1, B6-review-1-2) | medium | B7 |
| B6 | With a bucket, startup binds every remote archive file before the ingest listener starts, and a failure is Fatal, against ADR 0008 (B6-review-1-1) | medium | B7 |
| B6 | Removing object_store leaves bucket-only archived Events unread with no warning | low | B7 |
| A4a | alert_on_no_data is not held during a Log processing backlog (spec Risks): EventStore exposes no processing lag | low | later (needs an EventStore field) |

Done in batch 5: the B3 re-arm rule (A4a), the explorer run's timeout and the five A2a/A2b lows (A2c), the five B4a/B4b lows (B5), and ADR 0008's Event archive part (B6).

## Phase 6 polish

- **A0:** a long SQL error line is scrolled to a pixel, not a character, so it opens on a cut glyph with nothing showing it scrolls (A0-ux-2-1, A0-verify-2-1). The Playwright SQL holder files can still race between two runs (A0-ux-2-2, A0-correctness-2-2).
- **A1:**
  - An int 200 and a string "200" split into one bar, so the bars table isn't charted, or two legend entries named "200" (A1-correctness-2-2).
  - 50 lying bars push the value axis below the fold (A1-ux-2-6).
  - The chart focus ring crosses the top tick label (A1-ux-2-9).
  - The histogram strip still jumps while a run of a different shape is in flight (A1-verify-2-2).
  - "N more not shown" counts series, not legend entries, in panel mode (A1-verify-2-3).
  - On a phone, lying bar names that share their first 12 and last 7 characters read the same (A1-ux-2-3).
- The 28 rough edges in `research/v1-click-through.md`.
- **A1, moved from the table on 2026-10-17 (features first):** a series' colour and hidden state are lost when a function of the same unit is added or removed (A1-ux-2-1, medium); count axes step in fractions (A1-ux-2-5, A1-ux-3-1); lines and areas stop one bin short of a range that ends on a bin boundary (A1-ux-2-7); legend notes explain themselves only in a title attribute (A1-ux-2-8).
- **Skeleton loading states** (spec Phase 2, cut from A2b).
- **A2a:** CodeMirror's lint tooltip sits above the squiggle and can cover the line above; the bar says Ctrl+Enter on a Mac; SQL mode completes keywords only (no column names).
- **A2b:** the value menu's buttons on stream cells and legend entries appear only on hover, and the sidebar's Search hint is hidden when the sidebar is collapsed.
- **A3:** a card can be resized taller than the 24 rows DashboardChart allows (no maxH, A3-review-1-2); the 12-column grid is narrow at 390 px; unit and decimals don't reach axis tick labels; no flow for renaming a Dashboard or changing its default range.
- **Compaction, measured again:** the "not now" call rests on a quiet Project and a cold 7-day figure taken right after the 24-hour chart (B4a-spec-1-2, B4a-correctness-1-3). Re-measure with the real GCS check and the 12-chart, 7-day Dashboard.

- **A4b:** a field's server error stays on screen after it is fixed until the next save; the preview has Refresh but no Cancel; two Project pickers on the form (as in the chart editor); no flow or screenshot of a Match monitor's pages.
- **B5:** the explorer's histogram strip doesn't use the bin cache; the MinIO test's all-time range has more bins than the cache takes.
