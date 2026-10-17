# Logs v2: review findings carried over

Findings that a unit's review confirmed but left for later, and where each one goes. Each unit's full finding, with its failure scenario, is in the orchestrator's workflow results; the summary here is enough to act on.

Status: ready-for-agent

## Scheduled into a later unit

| From | Finding | Severity | Goes to |
|---|---|---|---|
| B3 | A Match monitor whose runs keep timing out never catches up; Console must re-arm it with after_batch "" after repeated timeouts and say that Logs were skipped (B3-verify-2-1) | low | A4 |
| A2a | A typed range too long for a JavaScript Date (parseRange takes any count, lib/timeRange.ts:134) blanks the whole page (A2a-review-1-2, A2a-verify-1-3) | low | A2c |
| A2a | On touch, the picker's initialFocus returns true, so base-ui focuses the typed box and opens the keyboard, against its own comment (A2a-review-1-3, A2a-verify-1-4) | low | A2c |
| A2b | Chart this and Group by from the value menu keep the stream's `take` (setSummarize keeps every other key, lib/logs/builder.ts:174) (A2b-review-1-1) | low | A2c |
| A2b | Closing the palette with Cmd/Ctrl+K skips PaletteDialog's close(), so its search text is not cleared (A2b-review-1-2) | low | A2c |
| A2b | Recent Queries are keyed `solidtrace.recentQueries.<org>/<project>` with no user, so two people on one browser share them (A2b-review-1-3) | low | A2c |
| A3 | The explorer's run (LogsController#query) still sends EventStore no timeout and is not counted in ChartRuns; it predates A3 (noted, not fixed, in A3's round 2) | medium | A2c |
| B4a | The object_store block is re-marshalled and decoded on its own, so YAML error line numbers count from the block, not the file (B4a-spec-1-3, B4a-correctness-1-4) | low | B5 |
| B4a | Nothing checks that the image's httpfs version (Dockerfile ARG DUCKDB_VERSION) matches the DuckDB in go.mod (B4a-spec-1-4) | low | B5 |
| B4a | The comment "DuckDB never downloads extensions itself" holds only with an object_store block (B4a-correctness-1-5) | low | B5 |
| B4b | With the object_store block removed after hours were evicted, location() hands DuckDB missing local paths and those hours fail; no startup warning (B4b-verify-1-2) | low | B5 |
| B4b | deleteTombstonedObjects counts a pass that attempted no Delete (every tombstone skipped, pinned) as a pass that got through (B4b-recheck-2-1) | low | B5 |
| B4a | ADR 0008 covers Log Parquet and the Event archive but describes only the Log files' lifecycle (B4a-spec-1-5) | low | B6 |

Done in batch 4: the A0 SQL→Builder bug (A2a, 5085a80), the A1 bin_auto(time) left behind (A2b), and the B1 timeout test and B3b settleNewDay test (B4a, dc5ea14 and 37f33d2).

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
