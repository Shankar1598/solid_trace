# SolidTrace MCP server

Status: ready-for-agent

**Origin:** the plan the user approved on 2026-10-17. This file is the spec. Departures go in Implementation decisions at the end. Tickets are in [issues/](issues/).

## Context

The user wants SolidTrace to have an MCP server that lets an agent do almost anything, as Sentry's and Better Stack's do. It builds on the logs v2 work on `feature/logs-v2`: Queries, SQL, Charts, Dashboards and Monitors are its core, plus Issues and a few admin actions. Today Console has only cookie sessions. The `api/v1` routes point at controllers that don't exist, and there is no Field list outside the browser. The goal is one MCP URL, added to Claude Code or Cursor with a token, where an agent acts as the token's user, with the rights that user has in the UI.

**Decided with the user (2026-10-17):**
- **Auth:** API tokens made in User settings and sent as `Authorization: Bearer`. No OAuth in v1; it can come later, since only the way a token is obtained would change.
- **Scope:** the logs work and Issues, plus admin actions: create a Project, manage keys, invite members. Membership is per Organization; there are no Project members.
- **Authorization:** a token names a user, not a role. Each tool call reads that user's role in the Organization it acts on and runs the same policy as the UI.
- **SQL:** `run_sql` is listed only while the instance setting allows SQL queries. SQL queries become read-only everywhere, in the UI and the MCP (G2), so `run_sql` is a read tool, not marked destructive.
- **Keys become admin-only** in both the UI and the MCP. Creating a Project stays open to any member.

## Design

**Where it runs:** inside Console, at `POST /mcp`. It uses the official `mcp` gem (1.8.0), with `MCP::Server::Transports::StreamableHTTPTransport.new(server, stateless: true, enable_json_response: true)` driven from a controller via `handle_request`. Console owns users, membership, policies and the EventStore client with its timeouts and the `ChartRuns` cap; EventStore knows only Project keys. This follows the reasoning of ADR 0006. Stateless JSON means no SSE stream holds a Puma thread. Query tools go through `ChartRuns.hold` and `EventStoreProxy::RUN_TIMEOUT_SECONDS` (30 s), as a Dashboard chart does. Record this as ADR 0009 (MCP in Console, API tokens), with the options considered:
- the MCP in EventStore (Go), which knows only Project keys, not users or roles;
- a stdio program each developer installs;
- OAuth sign-in in v1, deferred. It is needed for claude.ai's and ChatGPT's web connectors, and can be added later without changing the tools or their permission checks, since an OAuth token also names a user.

**Tokens:**
- An `api_tokens` table: `user_id`, `name`, `token_digest` (SHA-256 of a high-entropy `st_…` secret, looked up by digest), `last_used_at` and `created_at`.
- Revoking a token deletes its row.
- `last_used_at` is written at most once a minute, so as not to take SQLite's write lock on every call.
- The User settings page makes a token, shows it once with copy-paste snippets (`claude mcp add --transport http solidtrace <console>/mcp --header "Authorization: Bearer …"` and Cursor's JSON), lists tokens by name, creation and last use, and revokes them.

**Each request:**
1. `McpController < ActionController::API` reads the Bearer token. A missing or unknown one gets a `401` with `WWW-Authenticate: Bearer`.
2. It builds an `MCP::Server` with `server_context: { user:, console_url: }` and the tool list. `run_sql` is in the list only while `InstanceSetting.current.sql_queries_enabled?`.
3. It hands the request to the transport.

**Tool base** (`console/app/mcp/`):
- Resolves `organization` (a slug; optional when the user has exactly one Organization, and the error lists the choices otherwise) and `project` (a slug within it).
- Runs Pundit policies.
- Turns failures into tool errors (`isError: true`) the model can act on:
  - `EventStore::Rejected` returns the Query's mistakes with their positions.
  - `ChartRuns::Busy` says "busy, run again in a moment".
  - `TimedOut` suggests a shorter range.
  - `SQLQueriesOff`.
  - `Pundit::NotAuthorizedError` says "only an Organization admin can …".
  - `RecordNotFound` names the tool that lists the valid values.
- Read tools, `run_sql` among them, carry `readOnlyHint`. Deletes and key rotation carry `destructiveHint`.

**Small, linked answers:**
- Objects are compact JSON.
- Logs are one line each: time, level, service, a message cut at about 500 characters, a few Attributes and the id. Default 50, at most 200, with the next cursor. `get_log` returns the whole Log.
- Tables are a header plus at most 100 rows, with "N more: narrow the Query".
- Every answer ends with its Console link: the explorer with that Query and range (built as `lib/logs/explorerState.ts` reads its URL), a Dashboard, a Monitor or an Issue.
- Ranges are `range` (`15m`, `1h`, `24h`, `7d`; default `1h`) or ISO 8601 `from`/`to`.

**Tool catalogue:**

| Area | Tools |
|---|---|
| Context | `whoami` (user, Organizations, role in each), `list_projects` |
| Logs | `query_logs`, `get_log`, `list_fields`, `query_language_reference`, `run_sql` (when SQL is on) |
| Dashboards | `list_dashboards`, `get_dashboard`, `create_dashboard`, `update_dashboard`, `delete_dashboard`, `add_chart`, `update_chart`, `delete_chart`, `get_chart_data` |
| Monitors | `list_monitors` (firing groups, last run), `get_monitor` (states, last runs), `create_monitor`, `update_monitor` (including on/off), `delete_monitor`, `run_monitor`, `list_integrations` (read-only: id, name, provider, enabled) |
| Issues | `list_issues`, `get_issue` (latest or a given Event: exception, top frames, tags, trace id, with a hint to `query_logs where trace_id == …`), `update_issue` (resolve or unresolve, assign by email or `unassigned`), `add_comment` |
| Admin | `create_project`, `get_project_setup` (DSN, Log ingest URL and key, a curl example), `create_key`, `rotate_key`, `delete_key`, `list_members`, `invite_member` |

`query_logs`'s description carries a short cheat sheet: stages, `where`, `summarize count() by bin_auto(time)`, and the rule that a number only matches a number. `query_language_reference` returns `docs/logs/query-language.md`. The Console image is built from `console/` only, so a copy lives in `console/app/mcp/docs/`, and a test fails when the two differ.

**Code that moves out of controllers**, so the MCP and the UI share it rather than copy it:
- `MonitorsController#save`, `#checked_save` and `#integrations_param` move to `LogMonitor`, saving the Monitor with its Integrations and checking the Query before the transaction.
- `ChartRunning#run_chart`'s "answers Logs, not a table" rule moves into a call that returns the table or raises, used by the controller concern and the chart tools.
- The invite flow in `OrganizationUsersController#create` (find or create the user, reactivate, membership, email) moves to a service.
- `ApplicationController#public_ingest_url` moves to a place the MCP controller can reach.
- `IssuesController#resolve` and `#unresolve` use the status enum, not `0` and `1`.

## Tickets

Filed in [issues/](issues/), one ticket per unit: 01 API tokens, 02 the endpoint and tool base, 03 Fields (G1), 04 read-only SQL (G2), 05 to 09 the tool areas, 10 docs and wrap, 11 the whole-effort review. The numbered Console units and G1 and G2 below map onto them.

Set-up: ask the user to commit the open looks-pass work (`.scratch/logs-v2/issues/20-looks-pass.md`, `console/e2e/screens/tour.spec.ts`, `.claude/workflows/`). Then branch `feature/mcp` from `feature/logs-v2`, copy this plan to `.scratch/mcp/spec.md` with an `## Implementation decisions` section, and file `.scratch/mcp/issues/NN-*.md`. Add the effort to `.scratch/STATUS.md`, and add **API token** and **MCP server** to `CONTEXT.md` (domain-modeling skill).

**Console track** (main checkout, in order):
1. **API tokens:** migration, `ApiToken` model with token creation and lookup, User settings section with snippets, revoke. Playwright flow `e2e/flows/api-tokens.spec.ts`. Remove the dead `api/v1` routes.
2. **MCP endpoint and tool base:** the `mcp` gem, `post "mcp"` route, `McpController`, the tool base and error mapping, `whoami`, `list_projects`. ADR 0009.
3. **Logs tools:** `query_logs`, `get_log`, `query_language_reference` with its copy test, `run_sql`. `list_fields` once G1 is merged.
4. **Dashboard and Chart tools:** `add_chart` places charts at `Dashboard#bottom`, as `DashboardChartsController#create` does. Includes the `run_chart` extraction.
5. **Monitor tools:** includes the `LogMonitor` save extraction. `run_monitor` calls `MonitorEvaluation.run`, as Run now does.
6. **Issue tools.** The Events come from `EventStore.query_event_with_context!`, as `IssuesController#load_events` reads them.
7. **Admin tools and key rule:** `ProjectKeysController` authorizes key create, rotate and delete as admin-only through a policy, and the Project page hides those buttons for members (update their Playwright flow). The MCP key tools use the same policy. `invite_member` uses the extracted invite service, admin-only, as today.
8. **Docs and wrap:** `docs/mcp.md` covers connecting, the tools and permissions. It warns that Log text is written by senders and reaches the agent; with SQL on, that text can lead it to read other Organizations' data. Link it from the README. Update STATUS.md.

**Go track** (worktree `../solid_trace-mcp-go` on `feature/mcp-go`):
- **G1: Fields.** `GET /api/:project_id/logs/fields?from&to&limit` answers the Project's Fields in the range: the built-in fields plus the top Attributes by `(key, type, logs)`, summed from `logs.log_key_counts` over the range's UTC days, top 100 by default. Add `EventStore.logs_fields` in `console/app/services/event_store.rb`.
- **G2: SQL queries are read-only.** In `LogStore.RunSQL` (`event_store/storage/logs_sql.go`), before the SQL runs:
  - Parse it with `SELECT json_serialize_sql(?)`, the SQL bound as a parameter, on a connection that runs nothing else. Refuse it unless it parses to exactly one statement, and that statement is a SELECT.
  - Walk the parsed query and refuse calls to the functions that change state from a SELECT: `query`, `query_table`, `enable_logging`, `disable_logging`, `checkpoint`, `force_checkpoint`, `nextval` and `setseed`.
  - Turn off `autoload_known_extensions` and `autoinstall_known_extensions` once EventStore has loaded the extensions it needs.
  - A refusal is a 400 the explorer and the MCP show as they show a Query's mistakes: "SQL queries are read-only: … isn't allowed".
  - Step one is a probe on DuckDB 1.5.6, under a minute. It checks that `json_serialize_sql` still rejects non-SELECTs, PRAGMA included, and lists the statements of a multi-statement string.
  - The tests try each escape and expect a refusal, each as its own case: DROP, INSERT, COPY TO, ATTACH, SET, PRAGMA, CALL, a multi-statement string, `query('DROP …')`, `enable_logging()`, `checkpoint()`.
  - Amend ADR 0003: SQL can't change data any more, but it can still read every Organization's data, server files and the bucket, so the setting stays off by default. Update the setting's description and `docs/logs/sql.md` to match.

**Running it:** as in logs v2. Each unit is built test-first and committed in small slices, then gets one review plus one verify (high and medium only), and is merged `--no-ff`. At most two test-running agents, `go test -p 1`, and the heartbeat script started for background work. A whole-effort review closes the build.

## Critical files

- New: `console/app/controllers/mcp_controller.rb`, `console/app/mcp/**` (tool base and one file per area), `console/app/models/api_token.rb`, a migration, `console/app/frontend/pages/Settings/User.tsx` (tokens section), `event_store/handler/logs.go` and `event_store/storage/logs_*.go` (fields), `docs/adr/0009-*.md`, `docs/mcp.md`.
- Changed: `console/config/routes.rb`, `console/Gemfile`, `monitors_controller.rb` and `log_monitor.rb`, `concerns/chart_running.rb`, `organization_users_controller.rb`, `project_keys_controller.rb` and its policy, `issues_controller.rb`, `event_store/handler/routes.go`.
- Reused: `EventStore.query_logs`, `get_log`, `run_logs_sql` and `validate_logs_query`; `ChartRuns.hold`; `EventStoreProxy::RUN_TIMEOUT_SECONDS`; `MonitorEvaluation.run`; `Dashboard#bottom`; `ProjectPolicy` and `OrganizationUserPolicy`; the serializers (`DashboardSerializer`, `MonitorSerializer`, `IssueSerializer`, `EventSerializer`, `ProjectOptionSerializer`).

## Verification

- `bin/rails test`:
  - New tests in `test/mcp/`, one per tool area, calling tools with a user's context.
  - `test/integration/mcp_test.rb` posts JSON-RPC to `/mcp`: `initialize`, `tools/list` with and without SQL, `tools/call`, `401` without a token, a revoked token, another Organization's Project as not found, and a member's `invite_member` or `rotate_key` refused while an admin's works.
- `go test -p 1 ./...` in `event_store` covers the fields endpoint and every read-only escape case.
- In the explorer's SQL mode, `DROP TABLE logs.log_key_counts` is refused with the read-only message, and a plain SELECT still runs (extend `e2e/flows/logs-v1-sql.spec.ts`).
- vitest, typecheck, and the Playwright flows for tokens and the Project page's keys.
- End to end on the dev stack (`bin/dev`, EventStore on 4000, Console on 3000), each run under a minute:
  1. Make a token in the UI.
  2. Run `bunx @modelcontextprotocol/inspector --cli http://localhost:3000/mcp --transport http --header "Authorization: Bearer st_…" --method tools/list`.
  3. Call `query_logs`, `list_fields`, `create_dashboard`, `add_chart`, `create_monitor` and `run_monitor`, and check the answers and links.
  4. The user then runs `claude mcp add --transport http solidtrace-dev http://localhost:3000/mcp --header "Authorization: Bearer …"` and tries a prompt such as "what errored in the last hour, and chart it on a new dashboard".

## Implementation decisions

- **Worktree, not the main checkout (2026-10-17).** The plan put the Console track in the main checkout. The looks pass (logs v2 ticket 20) was in progress there, with uncommitted changes. So the whole effort runs in `../solid_trace-mcp` on `feature/mcp`, branched from `feature/logs-v2` at `4139293`. The Go track gets its own worktree from `feature/mcp` when it starts.
