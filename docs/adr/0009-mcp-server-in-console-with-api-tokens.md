# The MCP server runs in Console, and agents sign in with API tokens

An agent such as Claude Code or Cursor reaches SolidTrace through one MCP URL, `POST /mcp` on Console, with an API token in `Authorization: Bearer`. A user makes the token in User settings, and the agent acts as that user. Console keeps only the token's SHA-256 digest and finds the token by it. The token names a user, not a role: each tool call reads the user's role in the Organization it acts on and runs the same Pundit policy as the UI, and another Organization's records are not found.

`McpController` builds one `MCP::Server` per request, on the official `mcp` gem's Streamable HTTP transport in stateless JSON mode (`stateless: true, enable_json_response: true`). Each POST is answered with one JSON response: there are no sessions and no SSE stream, so no request holds a Puma thread open. Tools that run Queries go through `ChartRuns` and `EventStoreProxy::RUN_TIMEOUT_SECONDS`, as a Dashboard's charts do.

We chose Console for the same reasons as [ADR 0006](0006-monitors-evaluated-by-console-through-the-query-api.md). Console already owns users, membership and policies, the Dashboards, Monitors and Issues the tools change, and the EventStore client with its timeouts and its cap on chart runs. EventStore knows only Project keys.

## Considered options

- **The MCP server in EventStore (Go):** it would sit beside the Logs, but EventStore knows only Project keys, not users or roles. It would need Console's users, memberships and policies, and its Dashboard, Monitor and Issue tables, in Go.
- **A stdio program each developer installs:** a local process that calls Console. Every developer would install and update it, and it would still need a way to sign in to Console, so it adds a program without removing the token.
- **OAuth sign-in in v1:** claude.ai's and ChatGPT's web connectors need it, and it saves copying a token. It is deferred. An OAuth access token also names a user, so adding it later changes only how the agent gets its token, not the tools or their permission checks.

## Consequences

- **A token is as strong as its user.** It can do whatever the user can do in every Organization they belong to, until it is revoked. Revoking deletes its row. There are no scopes or expiry in v1.
- **`last_used_at` is written at most once a minute,** so a busy agent doesn't take SQLite's write lock on every call.
- **No server-to-client messages.** Without sessions or a stream, the server can't send progress or notifications or ask the client anything. Every tool answers in its one response.
- **The transport's DNS rebinding check is off.** It accepts only loopback hosts, which would turn away Console's own host. Every request needs a token, and Rails checks the Host.
- **Answers are small and linked.** Tools answer compact JSON or one line per Log, and end with a link to the Console page that shows the same thing, so a person can follow what the agent saw.
- **`run_sql` is listed only while SQL queries are on** ([ADR 0003](0003-unrestricted-sql-queries-behind-an-instance-setting.md)). With them on, Log text written by senders reaches the agent and can lead it to read other Organizations' data.
