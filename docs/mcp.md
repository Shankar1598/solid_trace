# The MCP server

SolidTrace has an [MCP](https://modelcontextprotocol.io) server, so an agent such as Claude Code or Cursor can read a Project's Logs and Issues, build Dashboards and Monitors, and set up Projects for you. It is part of Console, at `/mcp` on Console's address, and an agent reaches it with an API token. Why it is built this way is in [ADR 0009](adr/0009-mcp-server-in-console-with-api-tokens.md).

## Connecting

1. In Console, open **User settings**, and under **API tokens** make a token with a name that says where it is used, such as "Laptop, Claude Code".
2. Copy the token when it is shown. SolidTrace keeps only its digest, so it is shown this once.
3. Add the server to your client. The page shows both snippets with your token and Console's address filled in.

**Claude Code**, in a terminal:

```bash
claude mcp add --transport http solidtrace https://console.example.com/mcp \
  --header "Authorization: Bearer st_…"
```

**Cursor**, in `~/.cursor/mcp.json`, or `.cursor/mcp.json` in a project:

```json
{
  "mcpServers": {
    "solidtrace": {
      "url": "https://console.example.com/mcp",
      "headers": { "Authorization": "Bearer st_…" }
    }
  }
}
```

Any client that speaks MCP's Streamable HTTP transport and can send a header works the same way. The server answers each request with one JSON response, and keeps no session.

Web connectors that sign in with OAuth, such as claude.ai's and ChatGPT's, can't connect yet: the server takes only API tokens.

## Who the agent acts as

A tool acts as the token's user, with that user's rights. In each Organization it acts on, it has the role the user has there, and it can do what that role can do in Console:

- **Any member** can read Logs and Issues, make and change Dashboards, Charts and Monitors, resolve, assign and comment on Issues, and make Projects.
- **Only an Organization admin** can create, rotate and delete a Project's keys, change a Project's Log transform, and invite members. A member's agent is told so.
- Other Organizations' records are not found.

The User settings page lists your tokens with when each was last used. A token works until you revoke it there. Resetting your password revokes all of them, as well as signing you out everywhere. There are no scopes and no expiry: a token can do whatever you can do, in every Organization you belong to, so keep it as you would a password.

## Tools

Most tools take `organization`, an Organization's slug, which can be left out when you belong to only one, and `project`, a Project's slug. Tools that read Logs take `range`, such as `15m`, `1h`, `24h` or `7d` (`1h` when left out), or ISO 8601 `from` and `to`. Answers are compact, and end with a link to the Console page that shows the same thing.

**Context**

- `whoami`: you, the Organizations you belong to, and your role in each.
- `list_projects`: an Organization's Projects, with their slugs and how many days their Logs are kept.

**Logs**

- `query_logs`: runs a [Query](logs/query-language.md) on a Project's Logs. Logs come back one line each, 50 a page by default and at most 200, with a cursor for the next page; a summarize comes back as a table of at most 100 rows.
- `get_log`: one Log, whole, with every Attribute.
- `list_fields`: the names a Query can read: the built-in fields, then the Project's Attributes, most used first, with their types. It counts Logs as they arrive, so it leaves out Attributes that only Logs sent more than about an hour late carry, and Attributes whose values are JSON, such as arrays. A Query still reads them.
- `query_language_reference`: the whole Query language reference.
- `run_sql`: runs a read-only [DuckDB SQL](logs/sql.md) SELECT on a Project's Logs and Events. It is listed only while the instance's SQL queries are on. SQL that matches `message` with `ILIKE` still runs, and its answer carries a `notice` with the faster SQL to write ([Matching text](logs/sql.md#matching-text)).
- `get_log_transform`: a Project's Log transform, the [VRL](https://vector.dev/docs/reference/vrl/functions/) program Log processing runs on each of its Logs before storing them: the current program or an older version's, who saved it and when, the last 10 versions, and how many Logs the current version has processed, dropped and failed since EventStore started.
- `preview_log_transform`: runs a draft program on the Project's recent arrivals, or on up to 200 Logs you give, and answers how many it kept, dropped and failed, with the first 20 Logs before and after. Nothing is stored.
- `update_log_transform`: saves a program as the Project's Log transform, or answers VRL's errors when it doesn't compile. It applies to the Logs processed after it; Logs already stored don't change. Restore an older version by saving its program. Admins only.

**Dashboards**

- `list_dashboards`, `get_dashboard`: an Organization's Dashboards, and one with its Charts.
- `create_dashboard`, `update_dashboard`, `delete_dashboard`: make, change and delete a Dashboard.
- `add_chart`, `update_chart`, `delete_chart`: a Dashboard's Charts. A new Chart goes under the others.
- `get_chart_data`: runs a Chart's Query and answers the table it draws.

**Monitors**

- `list_monitors`, `get_monitor`: an Organization's Monitors with their status and firing groups, and one with its groups' states and last runs.
- `create_monitor`, `update_monitor`, `delete_monitor`: make, change, turn on or off, and delete a [Monitor](logs/monitors.md).
- `run_monitor`: runs a Monitor now, as its schedule would, telling its Integrations what changed.
- `list_integrations`: the Integrations Monitors can tell.

**Issues**

- `list_issues`: an Organization's Issues, the most recently seen first, narrowed by Project, status and title.
- `get_issue`: an Issue and one of its Events: the exception, its top frames, its tags and trace id, and how to find the trace's Logs.
- `update_issue`: resolves or unresolves an Issue, or assigns it to a member by email.
- `add_comment`: comments on an Issue as you.

**Admin**

- `create_project`: makes a Project with its first key.
- `get_project_setup`: how to send a Project its Events and Logs: the DSN, the Log ingest URL and key, and a curl example.
- `create_key`, `rotate_key`, `delete_key`: a Project's keys. Admins only.
- `list_members`: an Organization's members, with their emails and roles.
- `invite_member`: invites someone by email. Admins only.

Reads are marked read-only, and deletes, key rotation and Log transform saves destructive, so a client can ask you before it runs them.

## What Log text can do

Log text is written by whoever sends Logs: your services, and anything that reaches them, such as a user's input that a service logs. When an agent reads Logs, that text reaches the agent, and it may follow instructions written in it.

With your rights, an agent steered this way can change or delete Dashboards and Monitors, and, if you are an admin, rotate keys, invite someone, or save a Log transform that drops or changes the Logs that arrive after it. Clients that ask before a tool runs are your check: read what the agent wants to do before you allow it, especially after it has read Logs.

**With SQL queries on**, `run_sql` reads only the Project it runs on: its `logs` and `events` views, never other tables, files or URLs ([SQL queries](logs/sql.md)). An agent steered by Log text can read what you can read there, and each of its slow queries can slow down ingest for up to 10 seconds, when EventStore stops it. SQL queries are on by default; the operator can turn them off ([ADR 0003](adr/0003-unrestricted-sql-queries-behind-an-instance-setting.md)).
