# M3-02: Query mode in the explorer

**What to build:** a member switches from Builder to Query, edits the Query text, sees tables for tabular Queries, pivots from them, and switches back without losing work by surprise. Spec: [Logs explorer](../spec.md#logs-explorer-console-frontend) (Bar, Query mode, Non-Log results, Errors); user stories 56, 57, 61–65. Prototype: branch `prototype/logs-query-modes` (variant B).

**Blocked by:** M2-05, M3-01.

**Status:** resolved

- [x] The bar gains a `Builder | Query` switch after the Project and time range, opening in Builder.
- [x] Query mode opens with the Builder's Query as editable text.
- [x] Query → Builder switches silently when validate returns chips; otherwise it names the stages the Builder can't show and offers **Discard and switch** or **Stay in Query**.
- [x] Show only this and Hide this in Query mode add a `where` stage after the leading filters, before any `summarize`, and re-run.
- [x] A tabular result shows as a table in place of the stream; the histogram stays and counts the filtered Logs. No charts.
- [x] Compile errors show under the Query box with line and column, keeping the last good result.
- [x] vitest: the mode-switch rules (silent switch with chips, Discard/Stay otherwise); pivot insertion before `summarize`.

## Comments

### 2026-10-03 built (logs-v1/m3-02)

What was built:
- **Validate places each stage** (EventStore, `logquery/chips.go`, `handler/logs.go`). Validate's answer gains `stages`: for a Query without mistakes, one `{line, column, end_line, end_column, filter, builder}` per stage, from its keyword (the pipe is left out) to its end, in characters as `errors` are. `filter` marks the leading `where`/`search` stages (the head); `builder` marks those the Builder can show (a chip `where`, or the first `search`). A Query with mistakes has `stages: []`. `(*logquery.Query).Stages()` writes it. Tested in `logs_query_test.go` (`TestValidatePlacesEachStageAndSaysWhichTheBuilderCanShow`).
- **Plain modules** (`app/frontend/lib/logs/`, vitest):
  - `queryMode.ts`: `toBuilder(query, validation)` gives `{kind: 'switch', builder}` when validate gave chips, else `{kind: 'ask', stages, problems}` with the text of each stage the Builder can't show (sliced by validate's positions) or the Query's mistakes. `pivotQuery(query, stages, chip)` puts `\n| where …` after the last leading filter; with none, `where …\n| ` before the first stage; with no stages, the whole Query (after a bare `logs`).
  - `explorerState.ts`: `MODES` is exported (the switch's order) and `switchMode(explorer, mode)` gives `{explorer, replace}`: the mode changes in both `current` and `draft`, nothing runs, and `replace` is the search to put in place of the current history entry.
  - `builder.ts`: `filterText(chip)` is one chip's `where` stage (shared by `queryOf` and pivots).
  - `table.ts`: `LogTable`, `TableColumn`, `ColumnType`, and `pivotsOn(column)`.
  - `api.ts`: `fetchRun` answers `{kind: 'logs', page}` or `{kind: 'table', table}`; `fetchLogPage` is now only for older pages. `Validation` has `stages: QueryStage[]`.
- **Components** (`pages/Logs/components/`): `ModeSwitch` (`modes`, `mode`, `onSwitch`; labels in a `Record<Mode, string>`), `QueryBox` (the text, Ctrl+Enter runs, the mistakes under it as "Line L, column C: …", and the Discard and switch / Stay in Query prompt), `LogTableView` (the table, virtualised with `@tanstack/react-virtual` like the stream, column widths from the name and the first 200 rows, datetimes in local time with the UTC value in the hover title, a note when `truncated`). `Tag` takes `filterable` (default true).
- **Page flow** (`Logs/Index.tsx`):
  - Builder → Query keeps the draft's Query text (so a typed but unrun search comes along) and runs nothing.
  - Query → Builder validates the draft text. With chips it switches silently, running the Builder's Query only when the text differs from what ran. Otherwise the prompt names the stages; **Discard and switch** returns to the Builder's last chips (empty if there were none) and runs them if they differ from what ran.
  - A run in Query mode (Run, Ctrl+Enter, range, zoom, pivot) validates first: mistakes show under the box and nothing runs, so the result, histogram and URL stay. A Query with mistakes from a URL fails its run as before, and its mistakes also show under the box.
  - Pivots in Query mode (tags in the stream or the table, and the summary line's Level counts) validate the Query that ran, add the `where` with `pivotQuery`, and run.
- **Rails:** no code change; request tests show a table answer and validate's `stages` pass through.

Choices the ticket left open:
- **Naming stages without parsing.** Validate places the stages, and the browser slices the Query text by those positions. The alternative, a stage list as text from EventStore, would lose the user's formatting for pivots.
- **Switching mode is not a run**, so it pushes no history entry; it replaces the current entry's `mode`. Back then reopens a run in the mode it was left in.
- **M2-04's read-only "Query the Builder can't show"** is gone. A URL in Builder mode whose Query the Builder can't show (or that validate can't check) opens in Query mode, with the URL's mode replaced, and an empty Builder behind it.
- **Pivots apply to the Query that ran**, not to unrun edits in the box, because the clicked value came from that result. The edited text is replaced by the pivoted Query.
- **Which table cells pivot:** string, bool and dynamic columns (by, project and distinct columns of Fields). Number columns don't (mostly aggregates, such as `count_`, which no Log has: a `where count_ == 3` would compile and match nothing), nor datetime and timespan columns. A computed string column from `extend` still offers a pivot that matches nothing; telling those apart would need EventStore to report each column's source.
- **The bar** is Project, range, the switch, then the mode's controls (chips and search, or a "Ctrl+Enter runs the Query." hint), then Run/Cancel. The Query box is a row under the bar, as in the prototype.

Deviations: a small EventStore change (validate's `stages`) in a Console ticket, because the Builder's reasons and the pivot's place need the Query's structure and the browser never parses Queries.

Not verified in a browser: this box has no browser. tsc passes, and a throwaway server render of the Query box, prompt, mode switch and table ran without errors.

Follow-ups:
- **M3-04:** add `'sql'` to `Mode` and `MODES` (filter it out when the setting is off) and a label in `ModeSwitch`'s `LABELS`. `switchTo` in `Index.tsx` is where SQL's one-way discard prompt goes; `QueryBox` can hold the SQL text, or a sibling can. `commit` validates first only in Query mode.
