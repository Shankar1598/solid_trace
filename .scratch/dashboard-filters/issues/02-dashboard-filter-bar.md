# 02: A Dashboard's filter bar

**Status:** resolved
**Spec:** [spec.md](../spec.md)
**Blocked by:** 01

**What to build:** filters saved with a Dashboard, a bar of them, their values in the URL, and each Chart run with the values of the filters it names. The filter kinds are a select from a fixed list (one value or several) and a search box. Selects of a field's values are 03, and the Builder's filter chips are 04: here a Chart names a filter in Query mode.

## Where

- [ ] Migration: `dashboards.filters`, JSON, default `[]`, not null.
- [ ] `Dashboard` validates the list as the spec's "A filter" says: at most 20 filters, names unique, label at most 100 characters, kind `select` or `search`, `multiple` only on selects. A list's options are at most 200 `{label, value}` strings of at most 200 characters, values unique and not empty. Field options (03) are refused until 03 adds them. Errors name the filter, as `filters[1].name`.
- [ ] The Dashboard's props carry `filters`, and `DashboardsController#update` takes the whole list.
- [ ] `pages/Dashboards/components/FilterBar.tsx` under the header, and `FiltersDialog.tsx` for adding, removing, reordering and editing filters. A saved filter's name is shown but can't be edited. Removing one that Charts name asks first, naming them.
- [ ] `lib/dashboards/dashboard.ts`: read and write `v_<name>` values beside the range (several values repeat the key). `Show.tsx` keeps them in the URL with `router.replace`, as `pickRange` does.
- [ ] `ChartCard.tsx`: send the values of the filters the Chart's Query names (`parametersOf`, 01) with `fetchChartData`, and re-run when those change, not when others do. A card whose Query names a filter the Dashboard lacks says so above its body, and runs it as All.
- [ ] `DashboardChartsController#data` reads `v_*` from the query string (`Rack::Utils.parse_query`, so repeated keys stay lists), and `#preview` reads `parameters` from its body. Both go through `ChartRunning#run_chart` to `ChartRuns.run_chart(…, parameters:)`. Redirects after a save keep the `v_*` values, in both Dashboard controllers.
- [ ] The chart editor's preview runs with the bar's values.

## Tests

- [ ] Rails: the model's validations; `update` saving a list; `data` and `preview` passing `parameters` to EventStore (stubbed), repeated keys as lists; redirects keeping `v_*`.
- [ ] vitest: reading and writing `v_*` beside the range; the values a Chart sends.
- [ ] Playwright, `e2e/flows/dashboard-filters.spec.ts`:
  - add a list filter `level` (error, warn, info) and a Chart `where level == $level | summarize count()`;
  - picking `error` changes the number, puts `v_level=error` in the URL, and a reload keeps it;
  - a Chart that doesn't name `$level` isn't fetched again;
  - All brings the first number back;
  - a multiple select of two levels;
  - a search filter with `search $q`.

## Comments

- 2026-10-22 (orchestrator): built on `dashboard-filters/02-dashboard-filter-bar`, merged at `c69751f` (migration `20261022120000_add_filters_to_dashboards`). Decisions the code took:
  - A select's options are stored as `{from: "list", items: [{label, value}]}`, leaving room for 03's `{from: "field", project_id, field}`. A select needs 1 to 200 options. A search filter has no `multiple` or `options`; giving them is an error. Error keys count from 0 (`filters[0].options[2].value`).
  - `app/controllers/concerns/dashboard_view.rb` (both Dashboard controllers) keeps `range`/`from`/`to` and every `v_*` key on redirects, reads `v_*` for `#data` and `parameters` for `#preview`. The server drops names the Dashboard lacks and empty values, so they run as All.
  - The browser keeps only the first value of a single-value filter. `searchOfRange(range, values)` writes `v_*`; `parametersOfChart`, `unknownFilters` and the bar's "api, web" / "3 values" text live in `lib/dashboards/dashboard.ts`.
  - The Filters dialog takes options one per line, as `value` or `Label = value`. Removing a filter that Charts name asks first, inline, listing them.
  - A filter change re-runs only the affected Charts, over the times the Dashboard last ran. The bar shows even with no filters, with "Filters" at its end.
  - Flow: `e2e/flows/dashboard-filters.spec.ts`, "a member filters a Dashboard's charts from its filter bar, and the URL keeps the values".
