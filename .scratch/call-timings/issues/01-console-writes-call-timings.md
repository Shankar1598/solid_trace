# Console writes call timings

Status: resolved

Spec: [spec.md](../spec.md).

Build `CallTimings` in Console: the MCP tool line from `McpTools::Base.call`, the Log read line from `EventStore`'s four read calls, the origin from controllers and jobs, and the output (stdout in production, `log/call_timings.log` in development). Tests for both lines and their outcomes.

## Comments

2026-10-21: built on `feature/call-timings`. `CallTimings` (`console/app/services/call_timings.rb`) writes the lines; `McpTools::Base.call` times each tool; `EventStore.query_logs`, `logs_ingested_after`, `logs_histogram` and `run_logs_sql` time each read; `TimesCalls` (controllers) and `ApplicationJob` set the origin and request id. Output is set in `config/initializers/call_timings.rb`. Tests: `test/services/call_timings_test.rb` and one in `logs_controller_test.rb`; 247 targeted runs pass (services, MCP, Logs, Dashboards, Monitors, Monitor jobs). Not checked: a real VM, where Vector ships the lines to the Meta Project.

2026-10-21: every line also carries `version` and `revision`, baked into Console's image by `release.yml` (tag and commit) and `deploy/install --build` (commit, via `SOLIDTRACE_REVISION` in `deploy/.env`). Checked: the tests, `bash -n deploy/install`, `docker compose config`. Not checked: an image build, a release run.

2026-10-21: the commit is gone again (see the spec's Decisions); `version` stays. EventStore now logs the SQL each run ran, joined to Console's line by `request_id` (`X-Request-Id`), on `feature/log-read-sql`. Checked: the new root test, the related root tests, `storage` and `handler`, Vector's tests in Docker (`timberio/vector:0.58.0-alpine`), 271 Console runs.

2026-10-21: only the MCP server's reads are timed by default; `log_all_reads` (migration `AddLogAllReadsToInstanceSettings`, `instance_settings:set LOG_ALL_READS=true`) times the rest. EventStore logs SQL only for a request with `X-Log-SQL: 1`. On `feature/log-all-reads`: 496 Console runs and the EventStore run tests, `handler` and `storage` pass.
