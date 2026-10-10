# Logs v1 click-through (Phase 0, item 7)

2026-10-10, unit A0. The Logs v1 explorer, clicked through in Chromium by Playwright flows (`console/e2e/flows/logs-v1-*.spec.ts`) and captured by screens (`console/e2e/screens/logs-v1.spec.ts`) at 1440×900 and 390×844, in dark and light. Every PNG was read by eye.

Screenshots are in `~/.cache/solid-trace/screenshots/A0/`, named `<screen>--<size>-<theme>.png`, such as `v1-builder--phone-dark.png`. Run them again with `SCREENS_SET=A0 bun run e2e:screens` from `console/`.

## What works

Every path in the spec's item 7 works, with no page or console errors:

- **Builder:** a Where filter added in steps (Field, then operator and value) narrows the stream and the histogram; editing it to `!=` flips it, and × removes it. A Summarize (count by service) shows a table. More adds a search, a sort and a limit. A table cell's Show only adds a Where filter. Cancel stops a run, and Run starts it again.
- **Query mode:** Run and Ctrl+Enter (⌘↵ too) run the Query. A mistake shows its line and column under the box, and the last good result stays. Back to the Builder is silent when it can show the Query; otherwise it asks, and Stay and Discard both do what they say.
- **SQL mode:** it opens with the Query's readable SQL, runs only on Run, and answers a table. DuckDB's error shows with its caret under the mistake. Leaving with edited SQL asks first; leaving without edits is silent.
- **Histogram:** dragging across the strip zooms to the bars dragged over, and clicking a bar zooms to its time. Both change the URL's range. A Level count in the summary line filters on that Level, in the Builder and in Query mode.
- **Inspector:** a row opens it. Table and JSON tabs, Fold all and Unfold all, find a Field, j and k, Show only, Hide, has the Field, Add as a column, Count by, Copy and Download work. Esc and the close button close it. Esc in a popover over it closes only the popover.
- **Back and Forward** return to the run before or after, its Query and range, and run it again.
- Older Logs load as the stream scrolls.

## Fixed in this unit

Each is its own commit on `feature/logs-v2`.

- The time range list opened over the bar and the Builder, covering the control that opened it. It opens below now (`363159f`).
- A SQL error's lines were centred and collapsed, so DuckDB's `^` pointed nowhere (`de2b14a`).
- On a phone, a second filter wrapped under the Where label, and a long clause pushed its × off the row (`9f80664`).
- On a phone, the summary line ran off the screen and the strip's time labels overlapped (`50b6d80`).
- On a phone, the inspector cut its heading's time short; values broke mid-word even where a space would do (`2682ce3`).
- A table column's widest value was cut with an ellipsis: the widths left out the tag's padding (`8fb2918`).
- On a phone, the leaving prompt's two buttons wrapped one at a time (`8650d83`).
- The Group by list ended on a bare "Fields" heading, every Field below the fold, as though there were none (`7731c82`).
- A Query-mode link with no Query opened an empty box, and a Level count from it wrote a where with no source (`9fa01ea`).

## Rough edges for Phase 6

Each item gives what, where, a screenshot and a suggested fix.

### Builder and bar

1. **Sort by level is alphabetical.** Level sorts as text, so descending puts `warn` first, and `trace` and `info` ahead of `error` and `fatal`.
   - Where: Builder, More → sort by → level.
   - Screenshot: `v1-builder-search-sort-limit--desktop-dark.png`.
   - Fix: sort level by severity (trace < debug < info < warn < error < fatal) in EventStore's sort, or offer severity as its own sort key.
2. **Sorted Logs lose the stream.** Sorting Logs by a Field shows a plain table: no Level squares or colours, and a row doesn't open the inspector.
   - Where: Builder, More → sort by a Field.
   - Screenshot: `v1-builder-search-sort-limit--desktop-dark.png`.
   - Fix: keep the stream for sorted Logs, or let a table row with an `id` and `time` open the inspector.
3. **The bar crowds a phone.** "Each change in the Builder runs at once." takes two lines on a phone, three with an absolute range, and the rule after the mode switch is left alone at the end of the first line. Hiding the rule alone made it worse: the hint then fits beside the switch as a column of single words and pushes Run to a line of its own (tried and reverted, `fc3034a`, `5794163`).
   - Where: the bar, phone width.
   - Screenshots: `v1-builder--phone-dark.png`, `v1-builder-loading--phone-dark.png`, `v1-sql-table--phone-dark.png`.
   - Fix: below `sm`, hide the hint and the rule together and give Run `ml-auto`, so the bar is the range, the switch and Run.
4. **No custom range.** The range select offers presets only. An absolute range comes only from the histogram, and the select shows it but can't edit it.
   - Where: the time range select.
   - Screenshots: `v1-range-picker--desktop-dark.png`, `v1-histogram-zoomed--desktop-dark.png`.
   - Fix: a from–to picker in the shared `TimeRangePicker` (UX principle 6).
5. **Loading is plain text.** "Counting Logs…" and "Running the Query…" over blank space.
   - Where: summary line and result area while a run is going.
   - Screenshot: `v1-builder-loading--desktop-dark.png`.
   - Fix: skeleton bars and rows (UX principle 4).
6. **An unavailable EventStore says so twice and offers nothing to do.** The summary line and the result area both say "EventStore is unavailable. Try again shortly."
   - Where: summary line and result area when a run answers 502.
   - Screenshot: `v1-builder-unavailable--desktop-dark.png`.
   - Fix: "Not counted." in the summary line, and a Try again button under the notice.
7. **Ligatures change operators.** JetBrains Mono joins `==` into one long sign and shows `!=` as `≠`, in chips, the Query box and menus. A reader copying what they see would type `≠`.
   - Where: everywhere the mono font shows Query text.
   - Screenshot: `v1-builder-filtered--desktop-dark.png` (the Where chips).
   - Fix: `font-variant-ligatures: none` on Query text, chips and the value menu, or app-wide.
8. **An empty summary line keeps its separators.** With no Logs it reads "0 Logs error rate –" and a lone rule.
   - Where: the summary line.
   - Screenshot: `v1-builder-empty--desktop-dark.png`.
   - Fix: hide the error rate and the rule when the count is 0.

### Histogram

9. **Time labels fall on odd times.** Labels sit at bar starts, so a zoomed strip reads 08:30, 09:25, 10:25, 11:20, 12:15.
   - Where: the strip's time axis.
   - Screenshot: `v1-histogram-zoomed--desktop-dark.png`.
   - Fix: ticks at round steps (each 15 min, hour or day) for the range.
10. **A bar's counts are in a native tooltip.** Hovering shows the browser's slow, unstyled `title`.
    - Where: each bar.
    - Fix: a styled tooltip with the bar's time and per-Level counts, shared with `QueryChart`.
11. **No way back out of a zoom but Back.** After a drag or a click, only Back or a preset widens the range again.
    - Where: the strip after a zoom.
    - Screenshot: `v1-histogram-zoomed--desktop-dark.png`.
    - Fix: a "Zoom out" control beside the summary line that doubles the range, or returns to the range before the zoom.

### Query and SQL modes

12. **The hint says Ctrl+Enter on a Mac.** ⌘↵ works, but the bar always says "Ctrl+Enter runs the Query."
    - Where: the bar in Query and SQL modes.
    - Screenshots: `v1-query--desktop-dark.png`, `v1-sql-waiting--desktop-dark.png`.
    - Fix: show ⌘↵ on Apple platforms.
13. **A mistake isn't marked in the text.** The line and column are under the box, but nothing marks the spot in the Query.
    - Where: Query box.
    - Screenshot: `v1-query-error--desktop-dark.png`.
    - Fix: underline the problem's span (the problems carry `end_line` and `end_column`) in the shared `QueryEditor`.
14. **The box can hide a wrapped line.** Its rows count newlines, so on a phone a wrapped line can push the last line out of view.
    - Where: Query and SQL boxes, phone width.
    - Screenshot: `v1-query--phone-dark.png` (it just fits).
    - Fix: grow the box to its `scrollHeight`, up to the same cap.
15. **A projected table of Logs reads oldest first.** `project time, service, message` answers in `ORDER BY ALL` order, the opposite of the stream.
    - Where: Query mode, a Query ending in `project`.
    - Screenshot: `v1-query-leaving--desktop-dark.png`.
    - Fix: order a table with a `time` column newest first when the Query has no sort (EventStore).
16. **The leaving prompt's stages are hard to see in light mode.** The `code` chips are white on amber-50.
    - Where: Query → Builder prompt.
    - Screenshot: `v1-query-leaving--desktop-light.png`.
    - Fix: `bg-amber-100` with a border, or the mono text colour alone.
17. **A SQL error doesn't say what to do next.** It is DuckDB's text alone.
    - Where: SQL mode result area.
    - Screenshot: `v1-sql-error--desktop-dark.png`.
    - Fix: a line under it, such as "`DESCRIBE logs` lists its columns." (UX principle 4).
18. **The note under the SQL box doesn't mark `logs` as code.**
    - Where: SQL mode, under the box.
    - Screenshot: `v1-sql-waiting--desktop-dark.png`.
    - Fix: style `code` there as the leaving prompt does.

### Tables

19. **Numbers are unformatted and left-aligned.** A count reads 9776 in the table and 27,903 in the summary line.
    - Where: Summarize, Query and SQL tables.
    - Screenshots: `v1-builder-summarize--desktop-dark.png`, `v1-sql-table--desktop-dark.png`.
    - Fix: right-align number columns and group their digits; Copy keeps the raw value.
20. **`count()` makes a column named `count_`.** It is KQL's name, but it reads like a typo beside the Builder's `count()`.
    - Where: Summarize tables.
    - Screenshot: `v1-builder-summarize--desktop-dark.png`.
    - Fix: show `count()` as the header for an unnamed aggregate, or name it in `queryOf` (`summarize count = count()`).

### Stream

21. **The message column has a fixed width.** On a wide screen the message is truncated while empty space sits to its right.
    - Where: stream, desktop.
    - Screenshot: `v1-builder--desktop-dark.png`.
    - Fix: let the last column fill the rest of the row.
22. **A phone can't see the message.** Time, service and level fill 390 px; the message is off to the right.
    - Where: stream, phone width.
    - Screenshot: `v1-builder--phone-dark.png`.
    - Fix: on a phone, default to time and message, or show each Log on two lines.
23. **Copy sits far from the text.** A cell's Copy button is at the right end of a 520 px message cell.
    - Where: stream cells on hover.
    - Screenshot: `v1-builder-filtered--desktop-dark.png`.
    - Fix: put it just after the text, or at the row's right edge.

### Inspector

24. **It covers the column picker.** The inspector lies over the stream's right side, including Choose columns and most of the message.
    - Where: inspector open, desktop.
    - Screenshot: `v1-inspector-table--desktop-dark.png`.
    - Fix: narrow the stream while the inspector is open on wide screens, or move the column picker to the header's left.
25. **It is cramped on a phone.** It opens under the Builder and histogram, with room for about four Fields.
    - Where: inspector, phone width.
    - Screenshot: `v1-inspector-table--phone-dark.png`.
    - Fix: open it as a full-screen sheet on a phone.
26. **Line actions need a hover.** They show on hover or focus only, so a touch screen can't reach them, and they cover the value they act on.
    - Where: inspector Table and JSON lines.
    - Screenshot: `v1-inspector-line-actions--phone-dark.png`.
    - Fix: open the value menu on a tap (UX principle 1), and keep the toolbar off the value.
27. **The time's comment wraps to the left edge.** With wrapping on, `// Oct 10, 2026 at 18:20:33.899 GMT+1` breaks onto a line at column 0.
    - Where: JSON tab, the `time` line.
    - Screenshot: `v1-inspector-json--desktop-dark.png`.
    - Fix: keep the comment unbroken, or indent its wrapped part under the value.
28. **The value menu writes `=`.** Its label reads `service = worker`, while Queries write `service == "worker"`.
    - Where: the value menu on any tag.
    - Screenshots: `v1-inspector-value-menu--desktop-dark.png`, `v1-builder-summarize-value-menu--desktop-dark.png`.
    - Fix: label it as the Query reads, `service == "worker"`.
