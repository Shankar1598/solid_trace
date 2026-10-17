# 10: Docs and wrap

**Status:** ready-for-agent
**Track:** Console
**Blocked by:** 04, 05, 06, 07, 08, 09

## What to build

**`docs/mcp.md`:**
- how to connect from Claude Code and Cursor, with a token;
- every tool in one line each;
- that a tool acts as the token's user with that user's rights;
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
