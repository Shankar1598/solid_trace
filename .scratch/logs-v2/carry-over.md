# Logs v2: review findings carried over

Findings that a unit's review confirmed but left for later, and where each one goes. Each unit's full finding, with its failure scenario, is in the orchestrator's workflow results; the summary here is enough to act on.

Status: ready-for-agent

## Scheduled into a later unit

| From | Finding | Severity | Goes to |
|---|---|---|---|
| A5 | MonitorEvaluation.run works on the Monitor its caller loaded before the lease, and the version check compares only updated_at, which the run's own update_columns leave alone (A5-review-1-2, A5-verify-1-1) | low | whole-branch review |
| A5 | A TABLE_CUT error repeats every minute whatever the interval, and Save doesn't refuse a Query whose dry run is cut (A5-review-1-3) | low | whole-branch review |
| A5 | Deleting an Integration (dependent: :delete_all) sends no resolve for its firing Monitors (A5-review-1-1) | low | whole-branch review |
| A5 | Deleting or turning off a Match monitor leaves the PagerDuty incident its matched messages opened (monitors.md says resolve by hand) | low | later |
| A5 | Organization#destroy may fail on the notifications→integrations foreign key once an Integration has Notification rows (predates Logs v2; no UI deletes an Organization) | low | later |
| B7 | Without a bucket, a failed glob() count builds a view over the hot table only and reports success (B7-review-1-2) | low | whole-branch review |
| B7 | EventStore's new 503 for a stale events view is turned into an empty answer by Console's EventStore.make_request, so an Issue shows no Events instead of an error (B7-review-1-1) | low | whole-branch review |
| A4a | alert_on_no_data is not held during a Log processing backlog (spec Risks): EventStore exposes no processing lag | low | later (needs an EventStore field) |

Done in batch 6: every A5 and B7 row from batch 5, the Console docs (query-language Charts, monitors.md, ADR 0006, glossary) and docs/logs/object-store.md.

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
- **Compaction, measured again:** the "not now" call rests on a quiet Project and a cold 7-day figure taken right after the 24-hour chart (B4a-spec-1-2, B4a-correctness-1-3). ~~Re-measure with the 12-chart, 7-day Dashboard.~~ Done locally on 2026-10-18 ([research/dashboard-and-stream.md](research/dashboard-and-stream.md)): still not now. The bucket side waits for the real GCS check, [21](issues/21-real-gcs-check.md).
- **A4b:** a field's server error stays on screen after it is fixed until the next save; the preview has Refresh but no Cancel; two Project pickers on the form (as in the chart editor); no flow or screenshot of a Match monitor's pages.
- **B5:** the explorer's histogram strip doesn't use the bin cache; the MinIO test's all-time range has more bins than the cache takes.
