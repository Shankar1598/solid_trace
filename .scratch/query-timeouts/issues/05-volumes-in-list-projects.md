# 05: Live volumes in list_projects

**What to build:** MCP `list_projects` gives each Project's volume from the counts, so an agent can size a range before writing a Query:

- Logs per hour and bytes per hour over the last 24 hours;
- the number of files over the last 24 hours.

Agents call `list_projects` first, so this is where they look. The Project description stays the user's own text, because a hand-written volume would go stale. See the [spec](../spec.md).

**Blocked by:** None (can start immediately)

**Status:** resolved

- [x] Each Project in `list_projects` has `logs_per_hour`, `bytes_per_hour` and `files`, from the last 24 hours of the counts tables (`log_source_counts`, `log_minute_counts`). These are cheap reads, never of `logs`.
- [x] The tool's description says what the figures are for: sizing a range before a `search`.
- [x] A Project with no Logs in the last 24 hours shows zeros, not an error.
- [x] A test covers a Project with Logs and one without.

## Comments

**2026-10-30, built on `query-timeouts/05-volumes-in-list-projects`, merged into `feature/query-timeouts` (`3e1ecc8e`).**
- EventStore gains `POST /volumes` (`handler/overview_volumes.go`, `storage/logs_volumes.go`), on `OverviewHandler`. It reuses the `SourceCounts` read and adds `CountedLogs`. The Overview's own endpoint runs about 15 reads, too heavy to reuse here.
- `list_projects` makes one call per Organization.
- How the figures are made:
  - **Logs per hour:** from `log_minute_counts` over the 24 hours ending at the current minute.
  - **Bytes per hour:** those Logs × the bytes per Log in `log_source_counts` on the days touched.
  - **Files:** Sources over those whole UTC days, so a file seen earlier yesterday is counted; no table holds files by minute.
- Rates are rounded. When EventStore can't answer, the figures are null with a `note`. `docs/mcp.md` is updated.
- Tests: `go test -run TestVolumes .` (red, then green), `./handler/`, and `context_tools_test.rb` plus `mcp_test.rb` (15 runs). The EventStore-unavailable test was written after its code.
