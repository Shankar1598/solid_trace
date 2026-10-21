# Console writes call timings

Status: resolved

Spec: [spec.md](../spec.md).

Build `CallTimings` in Console: the MCP tool line from `McpTools::Base.call`, the Log read line from `EventStore`'s four read calls, the origin from controllers and jobs, and the output (stdout in production, `log/call_timings.log` in development). Tests for both lines and their outcomes.

## Comments

2026-10-21: built on `feature/call-timings`. `CallTimings` (`console/app/services/call_timings.rb`) writes the lines; `McpTools::Base.call` times each tool; `EventStore.query_logs`, `logs_ingested_after`, `logs_histogram` and `run_logs_sql` time each read; `TimesCalls` (controllers) and `ApplicationJob` set the origin and request id. Output is set in `config/initializers/call_timings.rb`. Tests: `test/services/call_timings_test.rb` and one in `logs_controller_test.rb`; 247 targeted runs pass (services, MCP, Logs, Dashboards, Monitors, Monitor jobs). Not checked: a real VM, where Vector ships the lines to the Meta Project.

2026-10-21: every line also carries `version` and `revision`, baked into Console's image by `release.yml` (tag and commit) and `deploy/install --build` (commit, via `SOLIDTRACE_REVISION` in `deploy/.env`). Checked: the tests, `bash -n deploy/install`, `docker compose config`. Not checked: an image build, a release run.
