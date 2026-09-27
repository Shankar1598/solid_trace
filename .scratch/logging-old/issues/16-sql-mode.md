# 16: Raw DuckDB SQL mode

**What to build:** An advanced mode on the Logs page where a user runs a DuckDB SQL `SELECT` over a `logs` relation that is already scoped to the selected Project and time range, with the same row cap and timeout as KQL. Anything other than a single SELECT over allowed relations and functions is rejected before execution.

**Blocked by:** 05

**Status:** wontfix

- [ ] Statement validated with `json_serialize_sql`: exactly one SELECT, only the scoped `logs` relation, a function allow-list, no file/extension/config functions
- [ ] The user SQL is wrapped in a CTE that scopes Project and time range; results share the KQL response shape
- [ ] Mode toggle and editor in the Console; SQL mode disabled for Monitors and Dashboards in this ticket
- [ ] Tests include a set of malicious statements that must be rejected

## Comments

2026-09-27: Closed as wontfix. This plan was superseded by the [logs map](../../logs/map.md). The file stays here as reference only.
