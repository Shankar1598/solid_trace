# 24: A time chart's groups under it, with their totals

**What was built:** from the user's testing on 2026-10-17. Under a time chart, the explorer's table listed one row per bin and group; it now lists one row per group, with its totals over the range, as Axiom does. EventStore answers an opt-in `totals` table: the same Query with the time bin dropped from the last summarize's `by`, so avg, dcount and percentiles are exact rather than combined from bins (`event_store/logquery/totals.go`). Only the explorer's run asks for it; Dashboards and Monitors don't. Each row carries its series' colour, so the table reads as the legend, and the Table form still shows the per-bin rows. Docs: the Charts section of `docs/logs/query-language.md`.

**Status:** resolved

Built by one implementer agent, without the review loop; Go suite, Rails (379 runs), vitest (375), typecheck and the charts, value-menu, dashboards, Builder, monitors and query-editor flows pass. Choices: EventStore decides from the time-axis hint (`bin_seconds`); a summarize by the bin alone with no aggregate gets no totals; the two scans run one after the other.

## Comments

### 2026-10-17 resolved

Commits e144543, 21ab3b8, af99dbb, 2be86de.
