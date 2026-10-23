# 05: MCP tools, and Monitors

**Status:** resolved
**Spec:** [spec.md](../spec.md)
**Blocked by:** 03

**What to build:** the MCP server reads, makes and uses a Dashboard's filters, and a Monitor's Query can't name a filter.

## Where

- [ ] `get_dashboard` lists the filters: name, label, kind, multiple, options.
- [ ] `create_dashboard` and `update_dashboard` take `filters`, the whole list, validated as the Console's are.
- [ ] `get_chart_data` takes `filters`, an object of name to a string or a list of strings, and passes them as `parameters`. Its answer says which values it ran with.
- [ ] `add_chart` and `update_chart` refuse a Query naming a filter the Dashboard lacks, from validate's `parameters`, naming the Dashboard's filters.
- [ ] `MonitorQueryCheck` refuses a Query that names a filter: "Filters ($service) work only on Dashboard Charts." This covers the Monitor form and the MCP Monitor tools.
- [ ] Tool descriptions, and `docs/mcp.md`.

## Tests

- [ ] Rails: `test/mcp/dashboard_tools_test.rb` for each tool above (EventStore stubbed where it already is); the Monitor check.

## Comments

- 2026-10-22 (orchestrator): built on `dashboard-filters/05-mcp-tools-and-monitors`, merged at `f3bdfde`. Decisions the code took:
  - `MonitorQueryCheck` calls validate first for both kinds (Match monitors too) and refuses any `parameters`, using EventStore's lexer rather than a Ruby copy. The error on `:query` reads "Query can't name a filter. Filters ($service) work only on Dashboard Charts.", since Rails puts "Query " in front of every `:query` error.
  - `get_chart_data` refuses a filter name the Dashboard lacks (likely a typo from an agent) rather than running it as All. Several values for a single-value filter pass through as given. Its answer has `filters: {name: [values]}`, and its link carries them as `v_<name>`.
  - The MCP tools take the Console's list shape, field options by `project_id`. `Dashboard#parameters(given)` is the one rule for which values a run gets, used by the controllers and `get_chart_data`.
  - `add_chart`/`update_chart` say "Not saved: Query names $nope, which isn't one of this Dashboard's filters (level, q)."
  - Docs: `docs/mcp.md`, `docs/logs/monitors.md`, and a line in the Query language reference (both copies).
