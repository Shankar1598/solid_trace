# 04: Filters in the Chart editor's Builder

**Status:** claimed
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
