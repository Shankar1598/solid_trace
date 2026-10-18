# 10: Docs and wrap

**Status:** resolved
**Track:** Console
**Blocked by:** 04, 05, 06, 07, 08, 09

## What to build

**`docs/mcp.md`:**
- how to connect from Claude Code and Cursor, with a token;
- every tool in one line each;
- that a tool acts as the token's user with that user's rights;
- that a token works until it is revoked, or until its user resets their password, which revokes all of them (bugs 02);
- the warning that Log text is written by whoever sends Logs and reaches the agent. With SQL on, that text can lead the agent to read other Organizations' data and, when a bucket is configured, to send requests from the server to any web address it can reach (ticket 04's review).

Link it from the README and from the User settings tokens section.

**`CONTEXT.md`:** add **API token** and **MCP server** to the glossary, if they weren't added at set-up.

**End to end on the dev stack**, each run under a minute:
1. Make a token in the UI.
2. Run `bunx @modelcontextprotocol/inspector --cli http://localhost:3000/mcp --transport http --header "Authorization: Bearer st_…" --method tools/list`.
3. Call `query_logs`, `list_fields`, `create_dashboard`, `add_chart`, `create_monitor` and `run_monitor`, and check the answers and their links.

Record the commands and what they answered in Comments.

## Acceptance

The docs are written and the end-to-end run is recorded. STATUS.md is updated.

## Comments

### 2026-10-18 resolved

The looks pass was merged in first (`d3de5fa`). The reset pages, the API tokens section and the Project page's key button follow its design, and the flows follow its new wording. The dev stack then ran `feature/mcp` from the worktree (`STACK_ROOT=../solid_trace-mcp stack.sh start`).

**Docs** (`b605b97`, and the lows below):
- `docs/mcp.md` covers connecting from Claude Code and Cursor, whom a tool acts as and with what rights, how long a token works (bugs 02's reset revokes all of them), every tool in a line, and what Log text can steer an agent to do, wider with SQL on and wider again with a bucket.
- The README links to it. The User settings tokens section links to it at `github.com/solidtrace/solid_trace`, the repository the README's clone command names. Console links to no other docs, so there was nothing to follow.
- `CONTEXT.md` already had **API token** and **MCP server** from set-up.

**Playwright:** all 77 flows pass on `feature/mcp` (7.2 min), including api-tokens, user-settings, password-reset, project-keys and logs-v1-sql. Two needed changes first, both in the flows:
- logs-v1-sql: a one-value SELECT shows as a Number since the looks pass, so the flow opens the Table to read the column.
- password-reset: the flow's user had no Organization, so signing in landed on `HomeController`'s plain text and never left `/login`. The user now joins the seed Organization. The dead end itself is [bugs 06](../../bugs/issues/06-no-organization-dead-end.md).

**End to end**, on the dev stack, each call under a few seconds:

1. A token made in User settings, by a Playwright script that fills Token name, presses Make token and reads the shown secret.
2. `bunx @modelcontextprotocol/inspector --cli http://localhost:4001/mcp --transport http --header "Authorization: Bearer st_…" --method tools/list`: 33 tools, without `run_sql`, since SQL queries were off. With them on (`instance_settings:set SQL_QUERIES_ENABLED=true`, set back after), 34, `run_sql` marked read-only. Reads carry `readOnlyHint`, deletes and `rotate_key` `destructiveHint`.
3. `--method tools/call --tool-name …`:
   - `whoami`: Admin User, admin of solid-trace-org.
   - `query_logs` `where level == "error"`, 1h, limit 3: three Log lines (time, level, service, id, message, Attributes), the next page's cursor, and the explorer link with the Query and range.
   - `query_logs` `… | summarize count() by service`, 24h: the table, api 516 and worker 221.
   - `list_fields`: the seven built-in fields, then Attributes by use with types (region, request_id, http.method, duration_ms as number…), and "more" past the limit. Its counts were 8% of the Logs: the dev data was backfilled over 24 hours, and late Logs aren't counted (03's low, below).
   - `create_dashboard` "MCP end to end", 24h: id 3, with its link.
   - `add_chart` twice: "Errors by service" (line) at y 0, then "Errors" (stat) under it at y 4. `get_chart_data` on the first, 1h: one-minute bins by service.
   - `list_integrations`: none. `create_monitor` threshold `… | summarize count() by service` `> 0` over 1440 minutes, by group: on, ok, never run. A broken Query: "Not saved: Query line 1, column 16: expected an expression, got "|"."
   - `run_monitor`: "Ran the Monitor: 2 groups firing." `get_monitor`: api 516 and worker 221 firing, and the run.
   - `run_sql` (SQL on): `SELECT * FROM logs.log_key_counts WHERE key IN (…)` answered the table.
   - The links opened in a browser: the explorer with the Query and 737 Logs, the Dashboard and the Monitor, Firing (2).
   - No token: `401` with `WWW-Authenticate: Bearer`. After the test records were deleted with `delete_monitor` and `delete_dashboard`, the token was revoked, and the inspector's next call was refused.

**Lows routed here, fixed:**
- `list_fields`' description and `docs/mcp.md` say that Logs sent more than about an hour late and JSON Attributes aren't counted (03).
- ADR 0003's Consequences no longer says the SQL "runs unvalidated" (04).
- The server's instructions name the logs, Dashboard, Monitor and Issue tools, and the links (06).
- `create_monitor`'s `kind` says a per-service threshold fires per group only with `notify_by_group` (07).

**New lows**, for 11, in [carry-over.md](../carry-over.md): the inspector warns that `add_chart` and `update_chart` give `options.stacked` and `options.decimals` a `type` array (`["integer","null"]`), legal but less portable than `anyOf`; and a revoked or wrong token's `401` sends clients that support OAuth off to look for it, so the inspector says "Interactive OAuth requires a TTY" rather than that the token was refused.

Rails (575 runs), vitest (391), typecheck and the MCP tests pass after the merge and the lows.
