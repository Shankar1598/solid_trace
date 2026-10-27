# The MCP server

SolidTrace has an [MCP](https://modelcontextprotocol.io) server, so an agent such as Claude Code or Cursor can read a Project's Logs and Issues, build Dashboards and Monitors, and set up Projects for you. It is part of Console, at `/mcp` on Console's address, and an agent reaches it with an API token. Why it is built this way is in [ADR 0009](adr/0009-mcp-server-in-console-with-api-tokens.md).

## Connecting

1. In Console, open **Settings → API tokens & MCP** and make a token with a name that says where it is used, such as "Laptop, Claude Code".
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
- **Only an instance admin** can read and change the install's email settings and its other settings, which apply to every Organization, and send a test email. `whoami` says whether you are one.
- Other Organizations' records are not found.

The **API tokens & MCP** settings page lists your tokens with when each was last used. A token works until you revoke it there. Resetting your password revokes all of them, as well as signing you out everywhere. There are no scopes and no expiry: a token can do whatever you can do, in every Organization you belong to, so keep it as you would a password.

## Tools

Most tools take `organization`, an Organization's slug, which can be left out when you belong to only one, and `project`, a Project's slug. Tools that read Logs take `range`, such as `15m`, `1h`, `24h` or `7d` (`1h` when left out), or ISO 8601 `from` and `to`. Answers are compact, and end with a link to the Console page that shows the same thing.

**Context**

- `whoami`: you, the Organizations you belong to, and your role in each.
- `list_projects`: an Organization's Projects, with their slugs, descriptions of what each holds, and how many days their Logs and their traces' Spans are kept.
- `get_overview`: an Organization's Overview, as its Overview page shows it, over `today` (the default), `7d` or `30d`, in whole UTC days: the Logs and bytes received, kept and stored, with the compression ratio (kept over the stored bytes of only the days with Source counts, and when those begin), unreadable Logs, Logs the Log transform dropped and requests turned away, and Logs, bytes and requests a second, each an average and the busiest second; each day's Logs by Project, bytes received and stored, and Logs a second; and each Project's Logs, sizes, ratio, share, rates, Retention, oldest day and when its last Log arrived; the Spans of traces received and stored, in total and for each Project that has any, with its trace Retention; and the biggest sources, the Services and files that sent the most Logs and kept the most bytes, with their Project, share and Logs dropped by the Log transform; and Health: each Project's oldest Log batch waiting for Log processing and its Log reads' times over the last 24 hours, the slowest reads, Monitors whose last run failed, Firing Monitors, and new Issues and Events; and Compared with other tools: what the range's volume, scaled to 30 days from the seconds it has had so far, would cost a month at Datadog's, Better Stack's and Axiom's list prices at each Project's Retention, what its Spans would cost at Datadog APM's (one APM host taken per Project that sent Spans, every Span indexed at its trace Retention), and GCP's internet egress for the bytes on the wire, with each pricing page and the date the prices were checked. For an instance admin only, it also answers Storage, the whole install's, every Organization's: ClickHouse's local disk use against the disk limit, the disk's free space, the bytes in the bucket, the local disk at steady state (each Project's last 7 days' average a day kept for its Retention, or local_cache_hours with a bucket), whether that stays under the limit, and if not about how many days until the limit at the last 7 days' growth, and the old Parquet files kept, with the day Retention deletes the last of them. It answers what EventStore couldn't as null.

**Logs**

- `query_logs`: runs a [Query](logs/query-language.md) on a Project's Logs. Logs come back one line each, 50 a page by default and at most 200, with a cursor for the next page; a summarize comes back as a table of at most 100 rows.
- `get_log`: one Log, whole, with every Attribute.
- `list_fields`: the names a Query can read: the built-in fields, then the Project's Attributes, most used first, with their types. It counts every Log as it is stored, but leaves out Attributes whose values are JSON, such as arrays. A Query still reads them.
- `query_language_reference`: the whole Query language reference.
- `run_sql`: runs a [ClickHouse SQL](logs/sql.md) SELECT on a Project's `logs`, `events` and `spans` tables, as the Project's read-only database user. It is listed only while the instance's SQL queries are on.
- `get_log_transform`: a Project's Log transform, the [VRL](https://vector.dev/docs/reference/vrl/functions/) program Log processing runs on each of its Logs before storing them: the current program or an older version's, who saved it and when, the last 10 versions, how many Logs the current version has processed, dropped and failed since EventStore started, and how a program is written. `preview_log_transform` and `update_log_transform` point agents at that last part rather than repeating it.
- `preview_log_transform`: runs a draft program on the Project's recent arrivals, or on up to 200 Logs you give, and answers how many it kept, dropped and failed, with the first 20 Logs before and after. Nothing is stored.
- `update_log_transform`: saves a program as the Project's Log transform, or answers VRL's errors when it doesn't compile. It applies to the Logs processed after it; Logs already stored don't change. Restore an older version by saving its program. Admins only.

**Performance**

These read the Transactions a Project's Sentry SDKs trace ([turning tracing on](traces.md)), over `range` (`24h` when left out) or `from` and `to`, in one `environment` or every one.

- `list_transactions`: a Project's Transactions, one row per name, the most time consuming first, as its Performance page lists them: throughput a minute and time consumed, both estimates that count each sampled request as 1 / its sample rate; p50, p95 and p99 of the sampled durations; the error rate, the weighted share of requests whose status was sent and isn't `ok`; and each number's change from the period of the same length before. Transactions named by their raw URL are one row, Unparameterised.
- `get_transaction`: one Transaction, as its page shows it: its p50 and p95 by release, its Spans' self time by op and by group (op and description with literals replaced), the groups a request runs more than once with their median and most repeats, and its slowest traces.

**Dashboards**

- `list_dashboards`, `get_dashboard`: an Organization's Dashboards, and one with its filters and Charts.
- `create_dashboard`, `update_dashboard`, `delete_dashboard`: make, change and delete a Dashboard. `filters` is the Dashboard's whole list of [filters](logs/query-language.md#dashboard-filters), in the bar's order, and giving it replaces the list. Each filter is `{"name", "label", "kind", "multiple", "options"}`: a `search` has only a name and a label, and a `select` takes `multiple` and its `options`, either a fixed list, `{"from": "list", "items": [{"label": "Errors", "value": "error"}]}`, or a field's most common values in one of the Organization's Projects, `{"from": "field", "project_id": 3, "field": "service"}`. They're checked as the Filters dialog checks them.
- `add_chart`, `update_chart`, `delete_chart`: a Dashboard's Charts. A new Chart goes under the others. A Chart's Query uses a filter by naming it as `$name`, and a Query naming a filter the Dashboard lacks isn't saved.
- `get_chart_data`: runs a Chart's Query and answers the table it draws. `filters` gives the filters' values by name, a string or a list, such as `{"service": ["api", "web"], "q": "timeout"}`, and a filter left out runs as All. The answer says the values it ran with, and links to the Dashboard showing them.

**Monitors**

- `list_monitors`, `get_monitor`: an Organization's Monitors with their status and firing groups, and one with its groups' states and last runs.
- `create_monitor`, `update_monitor`, `delete_monitor`: make, change, turn on or off, and delete a [Monitor](logs/monitors.md). A Monitor's Query can't name a Dashboard filter.
- `run_monitor`: runs a Monitor now, as its schedule would, telling its Integrations what changed.
- `list_integrations`: the Integrations Monitors can tell.

**Issues**

- `list_issues`: an Organization's Issues, the most recently seen first, narrowed by Project, status and title.
- `get_issue`: an Issue and one of its Events: the exception, its top frames, its tags and trace id, and how to find the trace's Logs.
- `get_trace`: a trace across the Organization's Projects, summarised to fit: its Transactions, the critical path, and its Spans with repeats collapsed, the most self time first, the three sharing a budget of 200 rows with a count of what was left out; its Issues; and the `query_logs` arguments for its Logs. A trace that wasn't sampled still answers its Issues and Logs.
- `update_issue`: resolves or unresolves an Issue, or assigns it to a member by email.
- `add_comment`: comments on an Issue as you.

**Admin**

- `create_project`: makes a Project with its first key, and optionally a description.
- `update_project`: changes a Project's description or how many days its Logs or its traces' Spans are kept. Admins only.
- `get_project_setup`: how to send a Project its Events and Logs: the DSN, the Log ingest URL and key, and a curl example.
- `create_key`, `rotate_key`, `delete_key`: a Project's keys. Admins only.
- `list_members`: an Organization's members, with their emails and roles.
- `invite_member`: invites someone by email. Admins only.

**Install**

- `get_email_settings`: the SMTP server and sender the install sends every Organization's email with, whether a password is saved and readable, and whether email is set up or what's missing. The password is never answered. Instance admins only.
- `update_email_settings`: changes the settings given, and sets or clears the password. A password given here passes through the agent's context; typing it on Console's Email settings page keeps it out. Instance admins only.
- `send_test_email`: sends a test email at once, to you or an address given, and answers the SMTP server's error when it fails. Instance admins only.
- `get_instance_settings`: the install's settings, as Settings → Instance shows them: the ones that apply at once (SQL queries, the default Retention of Logs and of traces, log-all-reads, log-MCP-reads), whether Sign-up is open and the command that changes it, and what Console and EventStore read at start, each with its env var or `event_store.yml` key. Secrets answer only whether they're set. Instance admins only.
- `update_instance_settings`: changes SQL queries, the default Retention of Logs or of traces, log-all-reads or log-MCP-reads, at once. It can't change Sign-up, which takes a shell on the server so a stolen session or API token can't open the install, or anything that needs a restart. Instance admins only.

Reads are marked read-only, and deletes, key rotation and Log transform saves destructive, so a client can ask you before it runs them.

## What Log text can do

Log text is written by whoever sends Logs: your services, and anything that reaches them, such as a user's input that a service logs. When an agent reads Logs, that text reaches the agent, and it may follow instructions written in it.

With your rights, an agent steered this way can change or delete Dashboards and Monitors, and, if you are an admin, rotate keys, invite someone, or save a Log transform that drops or changes the Logs that arrive after it. Clients that ask before a tool runs are your check: read what the agent wants to do before you allow it, especially after it has read Logs.

**With SQL queries on**, `run_sql` reads only the Project it runs on: the database user it runs as sees that Project's `logs`, `events` and `spans` and nothing else, never other Projects, tables, files or URLs, whatever the SQL says ([SQL queries](logs/sql.md)). An agent steered by Log text can read what you can read there, and each of its slow queries can slow down ingest for up to 10 seconds, when the database stops it. SQL queries are on by default; the operator can turn them off ([ADR 0003](adr/0003-unrestricted-sql-queries-behind-an-instance-setting.md)).
