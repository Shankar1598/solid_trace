# Dashboards

A Dashboard is an Organization's page of Charts and Text blocks. Each Chart saves a Query on one of the Organization's Projects, and runs over the Dashboard's time range with its Dashboard filters. Any member can make a Dashboard, add Charts from the Logs explorer or from the chart editor, change the range, move and resize Charts, and delete Charts and the Dashboard.

## Sub-features

- `dash-create`: `New dashboard`, then `Create dashboard`, opens the new Dashboard with its name as the heading.
- `dash-add-from-explorer`: in the explorer, the `Chart` group's `Bars`, then `Add to dashboard`, then the dialog's `Dashboard` combobox and `Add chart`.
- `dash-add-in-editor`: `Add chart` on the Dashboard opens the editor dialog with a `[data-chart-preview]` figure.
- `dash-range`: a new range runs each Chart again.
- `dash-layout`: a moved Chart is still moved after a reload.
- `dash-delete`: a deleted Chart and a deleted Dashboard are gone after a reload.
- `dash-filters`, `dash-sections`, `dash-text`: Dashboard filters, Sections, and Text blocks with Threshold lines (`dashboard-filters.spec.ts`, `dashboard-sections.spec.ts`, `dashboard-text.spec.ts`).

## How to get to it (user POV)

- Sidebar navigation `Main`, link `Dashboards`, which opens `/<org>/dashboards`.
- The Logs explorer's `Add to dashboard` button, after a Query that a Chart can draw.

## Driving it with verify

Preconditions:

- The baseline stack is up, and `"$V" doctor` is all `ok`.
- The seed Project has Logs (the first flow sends them).

- **Make, chart, range, lay out, delete.** `"$V" flow -- e2e/flows/dashboards.spec.ts`. Pass: the test passes. The trace shows `[data-chart-card="Logs by level"]` with a figure labeled `Bar chart of ... over time`.
- **Filters, Sections, Text blocks.** `"$V" flow -- e2e/flows/dashboard-filters.spec.ts e2e/flows/dashboard-sections.spec.ts e2e/flows/dashboard-text.spec.ts`. Pass: each test passes.
- **Stored state.** After you change a Dashboard by hand, read it back: `"$V" run -- bin/rails runner 'p Dashboard.last.attributes.slice("name", "updated_at")'`.
- **Proof.** The traces in `<run>/evidence/flow-<time>/playwright/`. For one picture, `"$V" snap --path /solid-trace-org/dashboards --name dashboards`.

## Gotchas

- A Chart runs its Query when it shows. Wait until the status `Running the chart's Query` is gone before you assert on a figure.
- Charts need Logs in the range. On a fresh stack with no flow run yet, each Chart is empty. That is not a fault of the Dashboard.
- The `Dashboards` link has `bg-secondary` when it is the current page. Assert that, not the URL alone, when you prove the navigation.
