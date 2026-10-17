# 06: Dashboard and Chart tools

**Status:** ready-for-agent
**Track:** Console
**Blocked by:** 02

## What to build

The Dashboards row of the spec's Tool catalogue.

**Extraction first.** `ChartRunning#run_chart`'s rule ("this Query answers Logs, not a table") moves into a call that returns the table or raises, such as a `ChartRuns.run_chart(project, query, from:, to:)`. `ChartRunning` and the tools both use it.

**Tools:**
- `add_chart` places a chart at `Dashboard#bottom`, as `DashboardChartsController#create` does. Its viz is one of `DashboardChart::VIZES`. Its validation errors come back as tool errors with the model's messages.
- `get_chart_data` runs a chart's saved Query over a range.
- Every answer carries the Dashboard's link.

## Acceptance

- `console/test/mcp/dashboard_tools_test.rb`:
  - CRUD;
  - placement;
  - another Organization's Dashboard or Project is not found;
  - a Logs Query refused as a chart.
- The existing dashboard controller tests still pass.

## Comments
