# 03: Selects of a field's values

**Status:** ready-for-agent
**Spec:** [spec.md](../spec.md)
**Blocked by:** 02

**What to build:** a select whose options are a field's 100 most common values in one Project, over the range the Dashboard shows, as `{from: "field", project_id, field}`.

## Where

- [ ] `Dashboard` takes field options: a field name (at most 200 characters, not `time`) and a Project of the Dashboard's Organization.
- [ ] `GET /:org/dashboards/:id/filters/:name/values?from&to` answers `{values: [{value, count}]}`, most common first. It runs `['<slug>'] | where isnotnull(['<field>']) | summarize count() by ['<field>'] | sort by count_ desc | take 100` as a chart run, inside `ChartRuns`' cap, with the slug and field escaped in their brackets. A value is its text: a number as written, a bool as `true` or `false`.
- [ ] The dropdown loads the values when it opens, and again when it opens after the range changed. It has a box to narrow them as you type, says when it is loading, and shows a run's error. A value in the URL that isn't among the options still shows as chosen.
- [ ] `FiltersDialog.tsx`: options "From a field" with a Project picker and a field name box, beside "Fixed list".

## Tests

- [ ] Rails: field options' validations; the values endpoint's Query text, escaping included, and its answer (EventStore stubbed); another Organization's Project refused.
- [ ] Playwright: add to `dashboard-filters.spec.ts` a filter of the dev Project's `service` values. Its dropdown lists them, and picking one filters a Chart.

## Comments
