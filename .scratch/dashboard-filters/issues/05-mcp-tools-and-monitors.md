# 05: MCP tools, and Monitors

**Status:** ready-for-agent
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
