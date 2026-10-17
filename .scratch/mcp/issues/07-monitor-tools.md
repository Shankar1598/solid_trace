# 07: Monitor tools

**Status:** ready-for-agent
**Track:** Console
**Blocked by:** 02

## What to build

The Monitors row of the spec's Tool catalogue.

**Extraction first.** `MonitorsController#save`, `#checked_save` and `#integrations_param` move into `LogMonitor`: a save that takes the Monitor's attributes and its Integration ids, refuses another Organization's Integrations, and checks the Query before the transaction (see the comment on `checked_save`). The controller then calls it.

**Tools:**
- `update_monitor` takes `enabled` for on and off. Turning a Monitor on checks its Query again, as `#enable` does.
- `run_monitor` calls `MonitorEvaluation.run`, as Run now does, and reports what the run found, as `#found` words it.
- `list_integrations` is read-only: id, name, provider and enabled.
- Every answer carries the Monitor's link.

## Acceptance

- `console/test/mcp/monitor_tools_test.rb`:
  - CRUD with Integrations;
  - a foreign Integration refused;
  - on and off;
  - Run now, both while the Monitor is held and when it is free.
- `console/test/controllers/monitors_controller_test.rb` passes unchanged.

## Comments
