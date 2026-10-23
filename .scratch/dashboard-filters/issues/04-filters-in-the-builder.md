# 04: Filters in the Chart editor's Builder

**Status:** resolved
**Spec:** [spec.md](../spec.md)
**Blocked by:** 02

**What to build:** in the Chart editor, a Builder chip's value can be one of the Dashboard's filters (`level == $level`), and the search box can be a search filter (`search $q`). The editor won't save a Query that names a filter the Dashboard lacks.

## Where

- [ ] `lib/logs/builder.ts`: `ChipValue` gains `{ filter: string }`, written as `$name` by `filterText`. The Builder's search can be a filter, written `search $q`. Chips read validate's `{filter}` values and `search_filter` (01).
- [ ] `BuilderPanel` takes an optional list of the Dashboard's filters. With one, a chip's value picker offers them, for `==`, `!=` and the text operators only. The search box offers the search filters. The explorer passes none and doesn't change.
- [ ] `ChartEditor.tsx` passes the Dashboard's filters. A name in validate's `parameters` that isn't one of them is a problem at that name, and Save stays off.
- [ ] A filter chip looks like a filter, not a value typed in, such as `level == $level` in the filter's colour.

## Tests

- [ ] vitest: `queryOf` with filter values and a search filter; chips read back from validate's shape.
- [ ] Playwright: in `dashboard-filters.spec.ts`, make a Chart in the Builder with a chip `level == $level`, save it, and see it follow the bar. A Query naming `$nope` can't be saved.

## Comments

- 2026-10-22 (orchestrator): built on `dashboard-filters/04-filters-in-the-builder`, merged at `e11478d`; its flow passed after 03's merge (`b4f9147`). Decisions the code took:
  - `ChipValue` is unchanged, since it is also the type of values seen in Logs; a filter is `FilterValue` (`{filter}`) in `Chip['value']`. `Builder.searchFilter` holds a search filter.
  - In the editors, a value typed as `$name` of one of the Dashboard's filters means that filter; a quoted `"$name"` is text. An operator that can't take a filter turns Apply off, with a note.
  - Chips offer all the Dashboard's filters (search ones too, as `message contains $q` is allowed); the search box offers only search filters.
  - `unknownFilterProblems` (`lib/dashboards/dashboard.ts`) puts "$nope isn't one of this Dashboard's filters (level, q)." at each `$nope`. Save is off for unknown filters only; other mistakes keep their behaviour (Save re-checks and switches to Query mode).
  - Flow: "a member makes a chart in the Builder with a filter as a chip's value, and can't save a Query naming one the Dashboard lacks".
