# 02: MCP endpoint and tool base

**Status:** resolved
**Track:** Console
**Blocked by:** 01

## What to build

`POST /mcp` in Console on the official `mcp` gem (`~> 1.8`), as in the spec's Design, under Each request and Tool base.

1. Check the gem's API before coding:
   - `MCP::Server::Transports::StreamableHTTPTransport.new(server, stateless: true, enable_json_response: true)`;
   - `#handle_request(Rack::Request)`;
   - `server_context`;
   - tool annotations (`read_only_hint`, `destructive_hint`);
   - how a tool returns an error result (`isError`).
   Record what you find in the spec's Implementation decisions.
2. `McpController < ActionController::API`, with a route `post "mcp"`. GET and DELETE answer `405` as the stateless transport does.
   - It reads the Bearer token through `ApiToken`. A missing or unknown token gets a `401` with `WWW-Authenticate: Bearer`.
   - It builds one `MCP::Server` per request, with `server_context: { user:, console_url: }`.
   - The tool list holds `run_sql` only while `InstanceSetting.current.sql_queries_enabled?`; 05 adds `run_sql` itself.
3. Tool base in `console/app/mcp/`:
   - resolve `organization` (a slug; optional when the user has exactly one Organization, otherwise the error lists the slugs) and `project` (a slug in that Organization);
   - Pundit authorization;
   - turn failures into tool error results, as listed in the spec;
   - compact JSON helpers;
   - Console links;
   - `range` / `from` / `to` parsing (`15m`, `1h`, `24h`, `7d`; default `1h`).
4. Move `ApplicationController#public_ingest_url` somewhere `McpController` can use it, such as a small concern or module.
5. Tools: `whoami` (the user, their Organizations and their role in each) and `list_projects`.
6. ADR `docs/adr/0009-mcp-server-in-console-with-api-tokens.md`, with the considered options from the spec.

## Acceptance

`console/test/integration/mcp_test.rb` covers:
- `initialize`;
- `tools/list`, with and without SQL on;
- `tools/call whoami`;
- `401` without a token and with a revoked one;
- another Organization's slug answered as not found.

`bin/rails test` passes.

## Comments

### 2026-10-17 resolved

Built on `feature/mcp` in workflow `wf_7acd8b1c-eef` (implement, one review and one verify, a skeptic on high and medium findings, fixes, recheck). Head `1186291`. One finding confirmed and fixed: MCP tool failures went to `Rails.error.report`, which nothing subscribes to, so they reached neither the log nor Sentry. The initializer now logs and sends them itself (`1186291`); the recheck found nothing. Decisions are under 02 in the spec's Implementation decisions; findings left for later are in [carry-over.md](../carry-over.md).
