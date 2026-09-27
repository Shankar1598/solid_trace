# 12: Dashboards

**What to build:** Users create Dashboards and add Elements (time series, statistic, table, log stream, note), each backed by a Query for one Project, laid out on a 12-column grid with a Dashboard time range and optional per-Element override. "Add to dashboard" on the Logs page pre-fills an Element from the current Query.

**Blocked by:** 06

**Status:** wontfix

- [ ] `dashboards` and `dashboard_elements` models, CRUD routes and pages under the Organization scope; Sidebar entry
- [ ] Show page runs each Element's Query through the Console with the Dashboard range and renders by kind; refresh interval optional
- [ ] Element editor dialog (title, Project, KQL with validation, kind, width and position presets)
- [ ] "Add to dashboard" flow from the Logs page
- [ ] Controller, model and serializer tests

## Comments

2026-09-27: Closed as wontfix. This plan was superseded by the [logs map](../../logs/map.md). The file stays here as reference only.
