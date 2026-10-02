# 06: How should the Logs explorer be laid out?

Type: prototype
Status: resolved
Blocked by: 01
Map: [SolidTrace Logs](../map.md)

## Question

What does the Logs page look like and how does it behave: project and time-range selection, the query bar, histogram, results table, log detail, and how a user pivots from a value to a filter? React to a rough shadcn prototype informed by Axiom's and Better Stack's explorers.

## Prototype

Built 2026-10-02. Captured on the throwaway branch `prototype/logs-explorer-layout` (commit `c208313`, off `feature/logging`); removed from `feature/logging`. To view: check out that branch, then open `/:org_slug/logs-prototype?variant=D` in development.

- Route (development only): `/:org_slug/logs-prototype?variant=A|B|C|D`, plus a dev-only "Logs (prototype)" sidebar entry. Real projects, about 6,000 fake logs in memory, including an error burst about 20 minutes ago.
- Code: `console/app/frontend/pages/LogsPrototype/` (variants, fake data, shared widgets), `console/app/frontend/components/PrototypeSwitcher.tsx`, `console/app/controllers/logs_prototype_controller.rb`.
- **A, Field rail workbench** (Axiom-like): multi-line APL editor, full-width histogram, a left rail of every field with top values that filter, exclude or become a column, and a table whose rows expand inline with per-value pivots.
- **B, Stream + inspector** (Better Stack-like): no visible query language (filter chips, search, level toggles, query text behind a toggle), a sparkline strip, terminal-style lines where each `key=value` token is a pivot menu, and a right inspector drawer.
- **C, Split-pane console** (devtools-like): project tabs, segmented time range, a one-line `key:value` omnibox, a large histogram with summary stats, and a table over a resizable detail panel (Fields, JSON, Context = surrounding logs on the same host). Pivots are right-click on any cell.
- All three: drag across the histogram to zoom (or click one bar). They run the same query logic; only presentation differs. Switching variants resets the query.

## Answer

Resolved 2026-10-02 (prototype). The user chose **variant D**: variant B's layout, with C's histogram made compact. In the prototype, `?variant=D`.

- **Top bar:** project and time-range selects, then filter chips, then a free-text search box. Each chip is `field = value` or `field ≠ value`; clicking the operator flips it and × removes the chip. The query text is not shown by default; a button reveals it read-only. How the Builder, APL and SQL modes fit into this bar is left to [How do the Builder, APL and SQL modes relate?](12-query-builder-ux.md).
- **No level toggle buttons.** Level is filtered like any other field, so a hidden level shows up as a `level ≠ …` chip.
- **Histogram:** a slim strip, about 48px of bars, stacked by level, with time labels underneath. Above it, one line of summary numbers (log count, error rate, p95 duration) and per-level counts that filter on click. Drag across the bars to zoom the time range, or click one bar. The user rejected the full-height version (140px plus a stat column) as too tall.
- **Results:** dense terminal-style lines, newest first. Each line reads time, `[service]`, level, message, then a few `key=value` fields. Errors and warnings get a coloured left edge.
- **Value to filter:** every value in a line is a tag, including the level and the service. Clicking one opens a menu with Show only this, Hide this and Copy value.
- **One log's detail:** clicking a line opens an inspector that **overlays** the right side of the stream instead of narrowing it. It has Fields and JSON tabs, and every value in it is a tag. The overlay covering the trailing fields was accepted.

Notes for later tickets:
- The summary line assumes every log has a level and a `duration_ms` field. Which summary numbers a real log supports depends on [What is a Log?](07-log-domain-model.md); a generic fallback is the log count plus per-level counts.
- Rejected parts, kept on the branch for reference: A's field rail and multi-line APL editor, C's project tabs, `key:value` omnibox, right-click pivots, and bottom detail panel with its Context tab (surrounding logs on the same host).
- Implementation trap: in this repo's base-ui shadcn, `DropdownMenuLabel` throws unless it is inside a `DropdownMenuGroup`, which blanked the page in the prototype.
