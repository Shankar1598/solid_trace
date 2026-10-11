# Logs v2: review findings carried over

Findings that a unit's review confirmed but left for later, and where each one goes. Each unit's full finding, with its failure scenario, is in the orchestrator's workflow results; the summary here is enough to act on.

Status: ready-for-agent

## Scheduled into a later unit

| From | Finding | Severity | Goes to |
|---|---|---|---|
| A0 | Leaving SQL mode for the Builder keeps a Query typed in Query mode that never ran; the next Run runs it under a Builder that doesn't show it (A0-ux-3-1, A0-correctness-3-1, A0-verify-3-1) | medium | A2a, first commit |
| A1 | A series' colour and hidden state are lost when a function of the same unit is added or removed (count → countif, avg → p95), because the series id gains the column name (A1-ux-2-1) | medium | A2b |
| A1 | Count axes step in fractions (0, 0.2 … 1; 0, 0.5 … 2), including the flat zero chart (A1-ux-2-5, A1-ux-3-1) | low | A2b |
| A1 | Lines and areas stop one bin short of the right edge on a range that ends on a bin boundary (A1-ux-2-7) | low | A2b |
| A1 | Removing the last function keeps the bin_auto(time) the click added when the user also grouped by a Field; the same for the inspector's Count by (A1-correctness-2-3, A1-verify-2-1) | low | A2b |
| A1 | Legend notes ("4 more not shown", "2 with no values") explain themselves only in a title attribute, out of reach on a phone or keyboard (A1-ux-2-8) | low | A2b |
| B1 | The storage timeout test accepts any error within 5 s, not the deadline (B1-correctness-2-1) | low | B4 |
| B3 | A Match monitor whose runs keep timing out never catches up; Console must re-arm it with after_batch "" after repeated timeouts and say that Logs were skipped (B3-verify-2-1) | low | A4 |
| B3b | No test reaches settleNewDay's hot-table branch (a set-less day whose hot Log keeps an Attribute in its map) (B3b-correctness-2-2, B3b-correctness-3-1) | low | B4 |

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
