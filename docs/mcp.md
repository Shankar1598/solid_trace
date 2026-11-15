# The MCP server

SolidTrace has an [MCP](https://modelcontextprotocol.io) server, so an agent such as Claude Code or Cursor can read a Project's Logs and Issues, build Dashboards and Monitors, and set up Projects for you. It is part of Console, at `/mcp` on Console's address. An agent signs in through the browser, or a script uses an API token. Why the server is built this way is in [ADR 0009](adr/0009-mcp-server-in-console-with-api-tokens.md), and why sign-in works this way is in [ADR 0021](adr/0021-mcp-clients-sign-in-with-oauth-in-console.md).

## Connecting

### Sign in through the browser

Add the server with only its URL. You copy no secret.

**Claude Code**, in a terminal:

```bash
claude mcp add --transport http solidtrace https://console.example.com/mcp
```

Then sign in: type `/mcp` in Claude Code and pick `solidtrace`, or run `claude mcp login solidtrace`. On a machine where Claude Code can't open a browser, `claude mcp login solidtrace --no-browser` prints the URL to open. Open it in a browser on any computer and approve. If the browser then can't reach Claude Code's `localhost` address, paste the address that the browser shows into Claude Code.

**Cursor**, in `~/.cursor/mcp.json`, or `.cursor/mcp.json` in a project, then sign in from Cursor's MCP settings:

```json
{
  "mcpServers": {
    "solidtrace": { "url": "https://console.example.com/mcp" }
  }
}
```

Settings → API tokens & MCP shows both snippets with Console's address filled in. Web connectors that sign in with OAuth, such as claude.ai's and ChatGPT's, connect the same way, with the same URL, when they can reach Console's URL from the internet.

What happens:

1. The client calls `POST /mcp` with no token. The 401 tells it where Console's OAuth metadata is (RFC 9728), and from there it finds how to sign in.
2. The client registers itself, and opens Console's **approval page** in your browser. You sign in if you have no Session, with Two-factor where you have it.
3. The approval page shows the client's name, where the name comes from, and the host that it sends you back to. "Checked: published at *host*" means that the client publishes its description at that host. "Not checked: the client gave this name" means that any program can give that name. A client that runs on your computer gets the warning "This program runs on a computer. Approve only if you started it."
4. You pick one Organization, and approve. The client acts as you only in that Organization.
5. The client gets an access token that works for one hour, and a refresh token. It gets new tokens by itself while you use it. A client that you don't use for 30 days must sign in again.

Two rules for the URL:

- **The Console URL must be HTTPS** (or `http://localhost`, for development). It is the Console URL setting on Settings → Instance, or `CONSOLE_URL` in `deploy/.env`. On an install with no Console URL, or with an `http://` one, sign-in through the browser is off: Settings → API tokens & MCP says so, Console's OAuth metadata is not found, the 401 from `/mcp` tells the client to send an API token, and the approval page and the `/oauth` endpoints say to set an HTTPS Console URL or to use an API token.
- **The MCP URL in the client must use the Console URL's host.** Console names `<Console URL>/mcp` as the server in its metadata and in each token, and a client refuses a server that has another host. If you open Console at `https://console.example.com`, add `https://console.example.com/mcp`, not an IP address or another name for the same machine.

A change of the Console URL signs out every Signed-in client: each must sign in again at the new URL. Settings → Instance says so beside the field.

An Organization with **Require two-factor** asks you to set up Two-factor before you can approve a client for it, and then brings you back to the approval page.

### On a machine with no browser: the device flow

A program on a machine with no browser can sign in with a short code (RFC 8628). The program shows a code such as `BCDF-GHJK` and the URL `<Console URL>/device`. Open that URL on your phone or laptop, sign in, and type the code. The device page shows where the program publishes its description, and when and from which IP address it asked for the code. Then you approve as on the approval page. A code works for 10 minutes.

Type a code only from a program that you started yourself. A code that someone sends you can sign their program in as you. No link fills the code in.

Only a client that publishes its description at an HTTPS URL (a Client ID Metadata Document) can use the device flow. A client that registers itself can't, because any program can register with any name. Claude Code doesn't use the device flow: `claude mcp login --no-browser` gives a URL to open.

### Signed-in clients

**Settings → User** lists your Signed-in clients: each client, its host, the Organization, whether you approved it in the browser or with a device code, the browser that approved it, when, and when it was last used. Each sign-in is its own row, also for the same client on two computers. **Revoke** stops a client's tokens at once, and your other clients keep working.

`claude mcp logout solidtrace` revokes the client's sign-in at Console too. `claude mcp remove` only removes the server from Claude Code's configuration: revoke the client on Settings → User after it.

Console also revokes Signed-in clients by itself:

- all of yours when you reset your password, or when the operator resets your Two-factor;
- yours for an Organization when you are removed from it. They stay revoked if you are added again;
- a client's sign-in when a refresh token is used again more than a minute after it was replaced, which tells Console that someone copied it. Inside that minute, the same new tokens are given again, so two copies of one client that refresh together stay signed in.

### API tokens, for scripts

A script, or a client that can't sign in through the browser, sends an API token.

1. In Console, open **Settings → API tokens & MCP** and make a token with a name that says where it is used, such as "Nightly report script".
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

The **API tokens & MCP** settings page lists your tokens with when each was last used. A token works until you revoke it there. Resetting your password revokes all of them, as well as signing you out everywhere. There are no scopes and no expiry: a token can do whatever you can do, in every Organization you belong to, so keep it as you would a password.

### Who a token is: introspection

A service that holds an API token, and gets a token from a user, can ask Console who that token is (RFC 7662):

```bash
curl -X POST https://console.example.com/oauth/introspect \
  -H "Authorization: Bearer st_…" \
  -d "token=<the user's token>"
```

An API token or an OAuth access token that works answers `active: true`, with `token_type` (`api_token` or `access_token`), `sub` (the user's id), `username` (their email), `scope`, `aud` (`<Console URL>/mcp`), `iat`, `organizations` (each with `id`, `slug`, `name` and the user's `role` now; one for an OAuth token), `limits`, and for an OAuth token also `client_id` and `exp`. Every other token, such as an expired, revoked or refresh token, answers only `active: false`. The caller must send its own API token; an OAuth access token gets a 401, because it works only at the MCP server. Introspection works also with sign-in through the browser off, and takes 120 calls a minute for each caller token.

## Who the agent acts as

A tool acts as the signed-in user, with that user's rights. In each Organization it acts on, it has the role the user has there, and it can do what that role can do in Console:

- **Any member** can read Logs and Issues, make and change Dashboards, Charts and Monitors, resolve, assign and comment on Issues, and make Projects.
- **Only an Organization admin** can create, rotate and delete a Project's keys, change a Project's Log transform, and invite members. A member's agent is told so.
- **Only an instance admin** can read and change the install's email settings and its other settings, which apply to every Organization, and send a test email. `whoami` says whether you are one.
- Other Organizations' records are not found.
- An Organization with **Require two-factor** refuses every tool on it from a user without Two-factor, telling the agent to have them set it up in Console. Their other Organizations answer as before. No tool reads or changes Require two-factor: only an admin who has Two-factor changes it, on the Organization's settings page.

The **API tokens & MCP** settings page lists your tokens with their scopes, Organization, limits and expiry, and when each was last used. A token works until its expiry or until you revoke it there. Resetting your password revokes all of them, as well as signing you out everywhere.

- **A token with no scope** can do whatever you can do over the MCP server. With no Organization, it acts in every Organization you belong to, so keep it as you would a password.
- **A token with scopes** can do only what they give, in its one Organization. If you leave that Organization, Console deletes the token (401), and it does not come back if you are invited again. The MCP server needs `mcp` or `mcp:read`; a token with neither gets 403 with `error="insufficient_scope"`.
- **`mcp:read`** gives only the read tools: `tools/list` leaves out each tool that makes or changes things, and a call of one gets 403 with `error="insufficient_scope"`. `mcp` holds `mcp:read`. The MCP server does not yet read with an `mcp:read` Project limit: such a token gets 403.
- The other scopes are for the factory: `sessions:write`, `sessions:read` (always with a list of Agent session ids), `run_events:write`, `factory` and `issues:comment`. The page says what each gives.
- **The Dispatcher's token** is for the factory's Dispatcher. Only an Admin of the Organization makes it, on the same page, and it acts as that Admin with `run_events:write`, `issues:comment` and `tokens:create`. The Dispatcher makes a child token for each Run with `POST /api/child_tokens` and revokes it with `DELETE /api/child_tokens/:id`. A child token has some of the scopes `run_events:write`, `issues:comment`, `sessions:write` and `sessions:read` (with a list of Agent session ids) and `mcp:read` (with one Project that is not the Agent Project), and an expiry no later than its parent's. Console refuses a child that is wider. The page shows the count of live child tokens on the Dispatcher's token. Revoking the Dispatcher's token stops each child at once, and if its Admin leaves the Organization, the token and its children stop working.

A client signed in through the browser acts only in the Organization you approved it for: `whoami` lists only it, a tool needs no `organization`, and another Organization's slug is a tool error that says to sign in again. The install's tools (`get_instance_settings`, `update_instance_settings`, `get_email_settings`, `update_email_settings`, `send_test_email`) are not offered to it: an instance admin uses an API token for them. An API token with no Organization acts in every Organization you belong to.

## Tools

Most tools take `organization`, an Organization's slug, which can be left out when you belong to only one, and `project`, a Project's slug. Tools that read Logs take `range`, such as `15m`, `1h`, `24h` or `7d` (`1h` when left out), or ISO 8601 `from` and `to`. Answers are compact, and end with a link to the Console page that shows the same thing.

**Context**

- `whoami`: you, the Organizations you belong to, and your role in each.
- `list_projects`: an Organization's Projects, with their slugs, descriptions of what each holds, how many days their Logs and their traces' Spans are kept, and each one's volume over the last 24 hours from EventStore's counts: Logs and bytes an hour, and how many files sent Logs, for sizing a Query's range before a `search`. The volume is null when EventStore can't answer.
- `get_overview`: an Organization's Overview, as its Overview page shows it, over `today` (the default), `7d` or `30d`, in whole UTC days: the Logs and bytes received, kept and stored, with the compression ratio (kept over the stored bytes of only the days with Source counts, and when those begin), unreadable Logs, Logs the Log transform dropped and requests turned away, and Logs, bytes and requests a second, each an average and the busiest second; each day's Logs by Project, bytes received and stored, and Logs a second; and each Project's Logs, sizes, ratio, share, rates, Retention, oldest day and when its last Log arrived; the Spans of traces received and stored, in total and for each Project that has any, with its trace Retention; and the biggest sources, the Services and files that sent the most Logs and kept the most bytes, with their Project, share and Logs dropped by the Log transform; and Health: each Project's oldest Log batch waiting for Log processing and its Log reads' times over the last 24 hours, the slowest reads, Monitors whose last run failed, Firing Monitors, and new Issues and Events; and Compared with other tools: what the range's volume, scaled to 30 days from the seconds it has had so far, would cost a month at Datadog's, Better Stack's and Axiom's list prices at each Project's Retention, what its Spans would cost at Datadog APM's (one APM host taken per Project that sent Spans, every Span indexed at its trace Retention), and GCP's internet egress for the bytes on the wire, with each pricing page and the date the prices were checked. For an instance admin only, it also answers Storage, the whole install's, every Organization's: ClickHouse's local disk use against the disk limit, the disk's free space, the bytes in the bucket, the local disk at steady state (each Project's last 7 days' average a day of Logs and of Spans, each kept for its Retention, or local_cache_hours with a bucket), whether that stays under the limit, and if not about how many days until the limit at the last 7 days' growth, and the old Parquet files kept, with the day Retention deletes the last of them. It answers what EventStore couldn't as null.

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
- `create_monitor`, `update_monitor`, `delete_monitor`: make, change, turn on or off, and delete a [Monitor](logs/monitors.md). A Monitor's Query can't name a Dashboard filter. A Threshold monitor takes `compare`, `value` or `change`: comparing the change, it takes `baseline` (`previous`, the range just before, or `day` or `week`, the same range 1 day or 1 week earlier), an optional `minimum`, `rises`, `falls` or `changes` as its operator, and a threshold percent greater than 0. Saving refuses `day` with a range over 1 day and a Baseline past the Project's Log Retention, as a tool error saying why. Switching `compare` resolves the Monitor's firing groups. `get_monitor` gives each group's `last_baseline` beside its `last_value`.
- `run_monitor`: runs a Monitor now, as its schedule would, telling its Integrations what changed. For a Monitor comparing the change, it lists each group's number, Baseline and change.
- `list_integrations`: the Integrations Monitors can tell.

**Issues**

- `list_issues`: an Organization's Issues, the most recently seen first, narrowed by Project, status and title.
- `get_issue`: an Issue and one of its Events: the exception, its top frames, its tags and trace id, and how to find the trace's Logs.
- `get_trace`: a trace across the Organization's Projects, summarised to fit: its Transactions, the critical path, and its Spans with repeats collapsed, the most self time first, the three sharing a budget of 200 rows with a count of what was left out; its Issues; and the `query_logs` arguments for its Logs. A trace that wasn't sampled still answers its Issues and Logs.
- `update_issue`: resolves or unresolves an Issue, or assigns it to a member by email.
- `add_comment`: comments on an Issue as you.

**Runs**

These read the run events in the Organization's Agent Project, so they need Agent sessions turned on. They only read: no MCP tool starts, messages or stops a Run.

- `list_runs`: an Organization's Runs with a run event in `range` (`30d` when left out) or `from` and `to`, as the Runs page lists them: Active Runs first, then the latest. Each Run has its state and reason, Project, repository, folder, trigger, title, actor, PR and outcome, its count of Executions, and a link to its Run page. `project` (a Project's slug) and `state` filter, as the page's filters do, and the answer links to the page with them. `limit` is 100 when left out, and 500 at most.
- `get_run`: one Run, as its Run page shows it: its fields with the base branch, each Execution (start, end, duration, the reason it ended, exit code, account, VM), the last report with its checks (pass or fail, with the end of their output) and Evidence paths, and each run event with its actor and details.

**Admin**

- `create_project`: makes a Project with its first key, and optionally a description and how many days its Logs and its traces' Spans are kept.
- `update_project`: changes a Project's description or how many days its Logs or its traces' Spans are kept. Admins only.
- `get_project_setup`: how to send a Project its Events and Logs: the DSN, the Log ingest URL and key, and a curl example.
- `create_key`, `rotate_key`, `delete_key`: a Project's keys. Admins only.
- `list_members`: an Organization's members, with their emails and roles.
- `invite_member`: invites someone by email. Admins only.

**Install**

- `get_email_settings`: the SMTP server and sender the install sends every Organization's email with, whether a password is saved and readable, and whether email is set up or what's missing. The password is never answered. Instance admins only.
- `update_email_settings`: changes the settings given, and sets or clears the password. A password given here passes through the agent's context; typing it on Console's Email settings page keeps it out. Instance admins only.
- `send_test_email`: sends a test email at once, to you or an address given, and answers the SMTP server's error when it fails. Instance admins only.
- `get_instance_settings`: the install's settings, as Settings → Instance shows them: the ones that apply at once (SQL queries, the default Retention of Logs and of traces, log-all-reads, log-MCP-reads, whether Console sends its own Issues and traces to the Meta Project, the text-match budget, the most a Query that matches text may read before `query_logs` refuses it, the Log read threads, ClickHouse's threads for each Log read, the hours on local disk before Logs and Spans move to the bucket, and the Console URL and Ingest URL that links and DSNs use, each with the fallback a blank one uses), whether Sign-up is open and the command that changes it, whether Passkeys are offered and at which host, or why not (they need the Console URL on `https://` with a domain, or `http://localhost`), and what Console and EventStore read at start, each with its env var or `event_store.yml` key. Secrets answer only whether they're set. Instance admins only.
- `update_instance_settings`: changes SQL queries, the default Retention of Logs or of traces, log-all-reads, log-MCP-reads, whether Console sends its own Issues and traces, the text-match budget, the Log read threads, the hours on local disk, the Console URL and Ingest URL (an empty string clears one), or the extra Redaction patterns (the whole list; EventStore refuses a pattern that is not valid or can match a quote or a backslash), at once; its description of `local_cache_hours` says what a change does, for the agent to tell the user first. A Console URL whose scheme differs from the HTTPS mode Console started in is refused: HTTPS changes only with `CONSOLE_URL` in `deploy/.env` and a restart. A Console URL on another host while Passkeys are made for the current one is refused with the count of Passkeys and of their users, as they'd stop working, unless called with `confirm_passkeys_stop_working: true`, for the agent to tell the user first; `bin/rails instance_settings:set CONSOLE_URL_SETTING=<url>` doesn't ask. It can't change Sign-up, which takes a shell on the server so a stolen session or API token can't open the install, or anything that needs a restart. Instance admins only.

Reads are marked read-only, and deletes, key rotation and Log transform saves destructive, so a client can ask you before it runs them.

## What Log text can do

Log text is written by whoever sends Logs: your services, and anything that reaches them, such as a user's input that a service logs. When an agent reads Logs, that text reaches the agent, and it may follow instructions written in it.

With your rights, an agent steered this way can change or delete Dashboards and Monitors, and, if you are an admin, rotate keys, invite someone, or save a Log transform that drops or changes the Logs that arrive after it. Clients that ask before a tool runs are your check: read what the agent wants to do before you allow it, especially after it has read Logs.

**With SQL queries on**, `run_sql` reads only the Project it runs on: the database user it runs as sees that Project's `logs`, `events` and `spans` and nothing else, never other Projects, tables, files or URLs, whatever the SQL says ([SQL queries](logs/sql.md)). An agent steered by Log text can read what you can read there, and each of its slow queries can slow down ingest for up to 10 seconds, when the database stops it. SQL queries are on by default; the operator can turn them off ([ADR 0003](adr/0003-unrestricted-sql-queries-behind-an-instance-setting.md)).
