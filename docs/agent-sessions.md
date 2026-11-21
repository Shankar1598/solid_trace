# Agent sessions

SolidTrace can record the Claude Code Agent sessions of an Organization's engineers, as transcripts in the Organization's Agent Project. Only the Agent sessions in a captured repository upload. An Agent session in another repository, or in no repository, never leaves the engineer's machine. Each member of the Organization can read the Agent sessions that upload.

## For Admins: turn on Agent sessions

1. Tell the engineers whose Agent sessions will upload: which repositories, to which Organization, and that each member of the Organization can read them.
2. In Console, open **Settings → Agent sessions**. Add one captured repository on each line, as `host/workspace/name` (for example `github.com/acme/shop`), or `host/workspace/*` for each repository of a workspace.
3. Tick the confirm, then click **Turn on Agent sessions**. This makes the Agent Project, which keeps the Agent sessions for 180 days. Change its Retention in its settings.

Each repository that you add later is new capture, so it needs the confirm too. Removing a repository needs no confirm: its uploads stop, and what is stored stays until Retention. Console records who confirmed, when, and for which repositories. A later change to the list sends no new notice to engineers, so tell them yourself.

### Push the plugin with managed settings

If your team uses Claude Code's [managed settings](https://docs.claude.com/en/docs/claude-code/settings), add the snippet from **Settings → Agent sessions → Capture setup** to them. It registers the SolidTrace marketplace of the instance's release (a `url` source, so no machine clones the repository) and force-enables the `solidtrace@solidtrace` plugin, so an engineer cannot turn it off:

```json
{
  "extraKnownMarketplaces": {
    "solidtrace": {
      "source": {
        "source": "url",
        "url": "https://github.com/Shankar1598/solid_trace/releases/download/v0.19.0/marketplace.json"
      }
    }
  },
  "enabledPlugins": {
    "solidtrace@solidtrace": true
  }
}
```

Each engineer must still install the CLI and sign in (steps 1 and 3 below).

## For engineers: turn on capture

**Settings → Agent sessions → Capture setup** in Console gives each command with your instance's version and addresses filled in. Each member of the Organization can open it.

1. Install the `solidtrace` CLI. The script checks the SHA-256 of the file before it installs it in `~/.local/bin`:

   ```bash
   curl -fsSL https://github.com/Shankar1598/solid_trace/releases/download/v0.19.0/install.sh | sh
   ```

2. Install the Claude Code plugin, unless your Admin pushes it with managed settings:

   ```bash
   claude plugin marketplace add https://github.com/Shankar1598/solid_trace/releases/download/v0.19.0/marketplace.json
   claude plugin install solidtrace@solidtrace
   ```

3. Sign in. The command opens Console in your browser; on a machine with no browser it gives a code for Console's device page. Use the command from the Capture setup page, which has your instance's Ingest URL:

   ```bash
   solidtrace login --ingest-url https://solidtrace.example.com:4443 https://solidtrace.example.com
   ```

   The approval page (or the device page) tells you which repositories upload, to which Organization, and that each member of it can read them. Approve only if you agree. The CLI prints the same text after the sign-in.

4. Check:

   ```bash
   solidtrace capture status
   ```

   It shows your Organization, its captured repositories, and each Agent session on this machine as "uploaded", "not in the list" or "failed".

### What uploads, and when

- The plugin's hooks upload the new part of an Agent session when a turn ends (`Stop`, `SubagentStop`) and when the Agent session ends (`SessionEnd`). `SessionStart` uploads the Agent sessions that a crash or a closed terminal left. So an Agent session shows in Console after its first turn ends.
- A hook never makes Claude Code wait: it starts the upload in the background. A failure goes to `capture.log` in the solidtrace config directory only.
- An Agent session uploads when the folder where Claude Code started is in a git repository (or a worktree of one) with a remote that matches a captured repository. An Agent session that starts in a folder that is not a repository does not upload, also when it works in a captured repository below that folder.
- The CLI gets the list from SolidTrace and keeps it for at most 10 minutes. With no list, it uploads nothing.
- SolidTrace removes the credentials that its Redaction patterns match before it stores an Agent session.

## Read the Agent sessions

Each member opens **Agent sessions** in the Organization's navigation. The page lists each Agent session with activity in the time range (7 days if you set no range), the newest activity first: the start, the last activity, the person or the Run, the repository, the model, the tokens, the estimated cost, and the counts of messages and tool calls.

- Filter by time, person, repository, **Project (Runs)**, and Run or no Run. The Project filter selects the Agent sessions of Runs only, because an engineer's own Agent session has no Project. Use the repository filter to find those. The filters are in the page's URL, so you can share a filtered view.
- The totals of tokens and estimated cost are for the Agent sessions that the filters select. The page shows the newest 500 of them; the totals include all of them.
- The cost is an estimate at list prices, not an invoice. It is Claude Code's own estimate when the transcript has one after its last reply, and else the tokens at the list price of each model. An Agent session whose model has no list price has no cost, and the totals say how many Agent sessions they do not include.

### Read one Agent session

Click the person or the Run in the list to open the Agent session page.

- The header shows the person or the Run, the repository, the folder, the parent Agent session (a link, for a fork or a `/clear`), the model, the first and last activity, the tokens by kind, the estimated cost and the flags.
- The timeline shows the person messages, the agent replies and the tool calls in time order. Each tool call shows its input, with its output below it.
- A message that a person sent while the agent worked shows at the time when the agent got it, not at the time when the person wrote it.
- Hook feedback, subagent prompts and the notices of background tasks show as "Not from a person". In an engineer's own Agent session, each person message is from that engineer.
- A subagent shows inside the tool call that started it. Click it to open its steps.
- A long Agent session loads in pages: click **Load more steps** at the end of the timeline.
- A **cut** or **partial** flag means that the record is not complete, so a resume of this Agent session can fail. The page says so above the header. A value marked **cut** was cut at the Log limits; the raw line has all of it.
- Click **Show full** on a cut value to read its raw line, as the record keeps it: with the secrets redacted.
- To find a step, type in the search box above the timeline. The search shows the steps of this Agent session, and of its subagents, that have the text, with any case. Click **Clear** to see the timeline again.

### Search across Agent sessions

To find which Agent session did a thing, type in the search box at the top of the Agent sessions page. It opens the Logs explorer on the Agent Project, with your text as the search, over the last 7 days. Each line of a transcript is a Log there: `file` is the transcript's path, and the attributes under `agent.` hold the Agent session id, the role and the tool name. Choose a longer range in the explorer when you accept a longer wait.

An agent can do the same over MCP: `query_logs` on the Agent Project runs a Query on the transcript Logs. `list_projects` names the Agent Project.

### Over MCP

An agent reads the Agent sessions with two MCP tools ([MCP](mcp.md)): `list_agent_sessions` lists them with the filters of the Agent sessions page, and `get_agent_session` reads one Agent session's header and its timeline in pages, or searches it. A token with `mcp:read` can call both.

## Set a Project's Factory settings

A Project's page has a **Factory** section when Settings → Instance has the Dispatcher's URL. It shows the Project's **Factory settings**, which the Dispatcher keeps: the repository (host/workspace/name, or a clone URL), the folder in the repository (empty is the root), and the default base branch (empty is the repository's default branch). Each member reads them; only an Admin of the Organization saves them. A save keeps the forge connection that `solidtrace projects set` gave. Console reads and saves them at the Dispatcher as you, with a factory token that lasts 5 minutes.

The section also shows the Project's tracker routes and schedules:

- **Tracker routes:** each member sees them. An Admin adds a route (a tracker connection and a match value; an empty value is the connection's default) and removes one. The Project needs Factory settings first. When the Dispatcher refuses a route, for example because another Project has the same pair, the section shows the Dispatcher's error.
- **Schedules:** each schedule with its cron expression and time zone, its next time (in the schedule's time zone), and its last Run with a link to the Run's page. Each member can **Pause** a schedule, which stops its next fire at once, and **Continue** a paused schedule. The section has no form to make a schedule: use `solidtrace schedules create`.

When the Dispatcher does not answer, or SolidTrace no longer accepts the Dispatcher's own token (for example, the Admin who made it left the Organization), the section shows that error. **Open this Project's Runs** opens the Runs page with the Project filter set.

## Start Runs from tracker tickets

A ticket of a tracker connection (Notion, Linear) that a person sets to the ready status starts a Run. The Run's prompt is the ticket's title and body, its page links to the ticket, and its actor is the tracker user who set the status.

- **The tracker settings** of a connection: the ready status (default `Ready for agent`), the routing field (the kind's default, such as the Notion select property `Project`), and the three statuses of the write-back: `working_status` (default `Agent working`), `review_status` (default `In review`) and `waiting_status` (default `Waiting on you`). An Admin saves them with `PUT /api/v1/connections/NAME/tracker` and `{"ready_status": "...", "routing_field": "...", "working_status": "...", "review_status": "...", "waiting_status": "..."}`; an empty value is the default. A save is refused (400) when a write-back status is the same as the ready status. `GET /api/v1/connections` shows them.
- **The write-back:** the Dispatcher tells the ticket what its Run does. When the first Execution starts, it sets the working status and posts a comment with the link to the Run. When the PR opens, it sets the review status; when that Execution ends, it posts one comment with the link to the PR, the link to the Run, the summary and the pass/fail list of the checks (`no Evidence` when no check passed). When the Run needs a person, fails, or is done with no PR, it sets the waiting status and posts a comment with the question, the reason or the summary. A stop changes no status, and there are no progress comments. A tracker that does not answer does not stop the Run: the Dispatcher keeps each step and sends it again, in order, for up to 24 hours. At the first failure of a step, and when it gives up a step, it writes a warning Log.
- **Routes** are part of a Project's Factory settings: each is a tracker connection and a match value. A ticket goes to the Project whose route has the value of the ticket's routing field. The route with an empty match value is the connection's default: it takes each ticket that no other route matches. Two Projects cannot have the same pair: the save answers 409 and names the Project that has it. An Admin saves them in `routes` of `PUT /api/v1/projects/SLUG/settings`; a save with no `routes` keeps them. Each member reads them.
- **No route:** a ready ticket that no route matches starts no Run. It gets one comment that names the routing field and the values that the routes match, and the Dispatcher writes a warning Log. A new edit of the ticket gets the comment and the warning again; the same edit gets none.
- A ready ticket starts at most one Run for each edit, and no Run while it has an Active Run.

### Continue a Run from its ticket

A comment on a Run's ticket that starts with the trigger word, `/agent` (for example `/agent Use the second fix`), is a message to the ticket's newest Run, as `solidtrace message` is. It is not a mention: the word is the first word of the comment. A Run that waits on a person, is done, failed or stopped continues in a new Execution with the comment; a queued Run keeps it for its next Execution; a running Run gets it at once. A Closed Run does not continue: when the ticket's newest Run is Closed, the comment goes to no Run, and an older Run of the ticket does not continue. The Run's events show the comment's text and its author's tracker name.

- **Who can steer:** a member of the tracker's team, and a guest whom the team invited. Never the connection's own identity (the Notion integration or the Linear app), another bot or integration, a Linear external user (such as a customer from a support tool), or anyone on a Notion page that is published to the web. The Dispatcher writes such a comment in its own log and does nothing.
- **Notion** gives the type of a comment's author only to an integration with the capability "Read user information without email addresses". With none, no comment can steer a Run, and the Dispatcher's log says why.
- Each comment goes to the Run one time, and again when its author edits it. A comment from before the Run (before its first minute) is not for it.
- Linear's **Comment** webhook and Notion's comment events make the Dispatcher read the ticket's comments at once. The backstop (and the poll of a Notion connection with no webhook) reads the comments of each ticket whose Run is not Closed.
- The operator changes the trigger word with `DISPATCHER_TRIGGER_WORD` (one word with no spaces).

### Connect Linear

A Linear connection is an install of a Linear OAuth app with `actor=app`, so the factory's status changes and comments show as the app, not as the Admin who installed it.

1. In Linear's settings, make an OAuth app. Add the callback URL `http://localhost:47615/callback` (or another port that you give with `--port`). Turn on its webhook with the **Issue** and **Comment** events; you paste its URL in step 3.
2. On a machine with a browser, as an Admin: `solidtrace connections add linear NAME --client-id ID --team KEY < client-secret-file`. `KEY` is the key of the connection's Linear team, such as `ENG`; you can leave out `--team` when the app can read only one team. The CLI opens Linear's approval page. After you approve, it gives the code to the Dispatcher, which gets the tokens from Linear and keeps them encrypted. The tokens never come to your machine. The access token lasts 24 hours; the Dispatcher refreshes it with the rotating refresh token, which lasts 30 days. Run the command again to install the app again, for example when the Dispatcher's log says that Linear refused the refresh token.
3. The command prints the connection's webhook URL. Paste it into the app's webhook, then give the app's signing secret: `solidtrace connections webhook-secret NAME < secret-file`.

- **One team:** a Linear connection is of one team. A ticket of another team starts no Run and gets no comment, though the app can read it. To connect another team, add another connection.
- **The routing field** of a Linear connection is a label group, by default `Project`. A ticket's routing value is the name of its label in that group, such as the label `Admin app` in the group `Project`.
- **Webhooks:** the Dispatcher checks `Linear-Signature` and refuses (401) a webhook whose `webhookTimestamp` is more than 60 seconds from its clock. It dedupes on `Linear-Delivery`. The backstop reads the ready tickets every 5 minutes.
- **Requests:** all the Runs of an install share the app's 5,000 requests an hour. A backstop reads at most 500 ready tickets.
- The operator can set `DISPATCHER_LINEAR_API_URL` (the base of Linear's API and token endpoint) for a test; the default is `https://api.linear.app`.

### Connect a Notion database

Each page of one Notion database is a ticket. Its title and its content (text blocks, also in columns, synced blocks and tables, at most 64 KiB) are the prompt. The Dispatcher reads the content when it starts the Run. When it cannot read all of the content, such as a block type that it does not know, the prompt ends with a note that the page has more content. Its status is the database's property of the type Status (the one named `Status` when there are several), and its routing field is a select property.

1. In Notion, make an internal integration with the capabilities "Read content", "Update content", "Read comments" and "Insert comments". Also give it "Read user information without email addresses": with it, a Run shows the name of the Notion user who set the status, and a `/agent` comment can continue a Run (see "Continue a Run from its ticket"). With none, a Run shows the user's Notion id, and no comment continues a Run. After an Admin adds the capability, it applies in at most 10 minutes. Share the database with the integration (the database's `...` menu → Connections).
2. Save the integration's token: `solidtrace connections set NAME --kind notion < token-file`. The Dispatcher keeps it encrypted.
3. Give the database: `PUT /api/v1/connections/NAME/tracker` with `{"database": "...", "ready_status": "...", "routing_field": "..."}`. The database is its id or the link to it. Each save gives all three values (an empty status or field is the default).

The Dispatcher reads the database every 30 seconds while the connection has no verified webhook, so a Notion connection works with no public webhook route. After the first verified webhook, it reads it every 5 minutes, and each webhook makes it read the page at once.

To add Notion's webhooks:

1. In the integration's Webhooks tab, make a subscription with the connection's webhook URL (`solidtrace connections list`), for the page and comment events.
2. Notion sends a verification token to the URL. The Dispatcher writes it in its own log (not in a SolidTrace Log), with the command to save it. Paste it into Notion to verify the subscription.
3. Save the same token as the webhook secret: `solidtrace connections webhook-secret NAME < token-file`. Notion signs each event with it (`X-Notion-Signature`).

Notion's `last_edited_time` has whole minutes. So a page that comes back to the same status and the same routing value in the same minute is the same edit, and starts no second Run.

## Connect a connection's webhooks

A tracker or a forge tells the Dispatcher of a change with a webhook. The Dispatcher has two listeners: the private listener serves the API, and the webhook listener serves only `POST /webhooks/<kind>/<connection id>`. Each other path and method gets 404. Caddy on the server VM sends only `/webhooks/*` from a public name to the webhook listener (`factory/reference/caddy/Caddyfile`). The operator sets the listener's address in `DISPATCHER_WEBHOOK_LISTEN` and the public name in `DISPATCHER_WEBHOOK_URL`.

1. `solidtrace connections list` shows the webhook URL of each connection whose kind sends webhooks. Paste it into the connection's service.
2. Give the secret that signs the webhooks: `solidtrace connections webhook-secret NAME < secret-file` (an Admin only). The Dispatcher keeps it encrypted, and no answer gives it.

A webhook is only a hint. The Dispatcher checks its HMAC-SHA256 signature on the raw body before it reads the body, and answers 401 when it is not valid. It refuses a body over 1 MiB (413) and more than 120 webhooks a minute from one connection (429). A delivery id that it got in the last 7 days does nothing. A valid webhook gets 200 at once, and the Dispatcher then reads the change from the service's API. It also reads each connection when it starts, and again at each backstop interval (5 minutes for most kinds), so a change is found also when no webhook comes.

`solidtrace connections list` shows the time of each connection's last webhook. When a backstop finds a change that no webhook announced since the previous backstop, the Dispatcher writes a warning Log with the Organization, the connection and the time of the last webhook, so that a Monitor can find a connection whose webhooks stopped. A connection with no webhook secret gets no webhooks, so its backstop does not warn.

### Bitbucket's webhook: the PR outcome

A Bitbucket connection's webhook gives the outcome of a Run's PR at once. Without it, the Dispatcher checks the PRs of the Runs that are not closed every 5 minutes, and that check stays as the backstop.

1. In each repository that a Project names, open **Repository settings → Webhooks → Add webhook**. Paste the Bitbucket connection's webhook URL (`solidtrace connections list`), and set a secret.
2. Select the **Pull request: Merged**, **Pull request: Declined** and **Pull request: Comment created** triggers.
3. Save the same secret: `solidtrace connections webhook-secret NAME < secret-file`.

The Dispatcher checks `X-Hub-Signature` (`sha256=<hex>` of the raw body) and dedupes on `X-Request-UUID`. Bitbucket's webhooks carry no time, so the signature is the only check. `pullrequest:fulfilled` and `pullrequest:rejected` make the Dispatcher read the PR's state from Bitbucket: a merged PR gives the PR outcome merged, a declined PR gives declined, and the Run closes (a Run with an Execution queued or live closes when its Execution ends). `pullrequest:comment_created` continues the Run (see below). Each other event, such as a push to a Run's work-in-progress ref, gets 200 and does nothing.

### Continue a Run with a comment on its PR

A comment on a Run's PR that starts with the trigger word (`/agent`; the operator changes it with `DISPATCHER_TRIGGER_WORD`) is a message to the Run, as `solidtrace message` is. Write your line comments first, then one general comment, such as `/agent address the review`: the agent gets all of them at once.

- The Dispatcher reads the PR's comments from Bitbucket with the Run's connection. The message has the trigger comment first, then each other comment that is not resolved, with its author, file, line and thread. A comment goes to the Run one time; after an edit, the next trigger comment gives it again.
- Each person who can comment on the PR of a private repository can continue the Run, also a reviewer who is not a SolidTrace member. The message's event has the Bitbucket account id and name (`actor.kind` `forge`).
- These comments do nothing: a comment on a public repository, a comment by a bot, an app or an access token, a comment by the account that opened the PR (the factory's own identity), and a resolved or deleted comment. The comments of such accounts are not in the message.
- On a Closed Run, the Dispatcher answers the trigger comment one time: the Run is closed, and a new Run continues the work. A PR gets at most one such answer in a day.
- With no webhook, a comment does nothing: write a new trigger comment when the webhook works again.

## Start Runs on a schedule

A **Schedule** starts a Run of a Project at each fire time of a five-field cron expression (minute, hour, day of the month, month, day of the week) in an IANA time zone (UTC if you give none). Each member of the Organization makes and changes schedules with the CLI:

```sh
solidtrace schedules create checkout --name "Update dependencies" --cron "0 9 * * 1" \
  --time-zone Europe/Berlin "Update the dependencies and run the tests"
solidtrace schedules list [--project checkout]
solidtrace schedules update ID --cron "0 7 * * 1"
solidtrace schedules pause ID      # stops the next fire at once
solidtrace schedules continue ID   # starts the fire times again, from now
solidtrace schedules delete ID
```

- The Project needs Factory settings. A Run starts from the schedule's base branch (`--base-branch`), else the Project's default base branch.
- The shortest interval between two fire times is 1 hour. The operator changes it with `DISPATCHER_SCHEDULE_MIN_INTERVAL` (a duration, such as `30m`).
- A fire time while the schedule's last Run is active starts no Run; the Dispatcher writes the warning Log "skipped: last Run is active".
- A fire time that the Dispatcher missed while it was down starts one Run when the Dispatcher starts, if it is less than 24 hours old. Older fire times start no Run.
- The Run's starter is the schedule, with the member who made it. Each change to a schedule is a Log of the Dispatcher (`event.kind` `schedule_created`, `schedule_updated`, `schedule_paused`, `schedule_continued` or `schedule_deleted`) with the person who made it.

## Start Runs from any tool: the generic webhook

A **Webhook sender** is a tool that starts Runs with signed requests, such as a CI job or a script. An Admin makes one with `solidtrace senders create NAME [--project PROJECT]...`. The command prints the sender's id, its webhook URL (`/webhooks/generic/<sender id>` under `DISPATCHER_WEBHOOK_URL`; the private listener serves the same path) and its secret. The Dispatcher shows the secret this one time only, and keeps it encrypted. With `--project`, the sender can start Runs only in those Projects; with none, in each Project of the Organization. A sender belongs to the Organization of the Admin who made it. `solidtrace senders list` shows each sender, with no secret, and the time of its last delivery. `solidtrace senders delete SENDER_ID` (an Admin only) deletes one; its webhook URL then gets 404, and its Runs stay.

A delivery is `POST` to the webhook URL, with a JSON body and three headers:

- `SolidTrace-Timestamp`: the Unix time in seconds. A timestamp more than 5 minutes from the Dispatcher's clock gets 401.
- `SolidTrace-Delivery`: a delivery id. A delivery id that the sender sent in the last 7 days gives the first delivery's Run and starts no Run.
- `SolidTrace-Signature`: `sha256=` and the hex HMAC-SHA256, with the secret, of the timestamp, a `.` and the raw body. With no valid signature, the delivery gets 401.

The body has `project` (necessary: the Project's slug), `prompt` (necessary, at most 64 KiB), and optional `branch` (the base branch; with none, the Project's default base branch), `title`, `link` (an `http` or `https` URL that the Run shows), `dedupe_key` and `attributes` (at most 20 strings, which the Run keeps). For example:

```sh
body='{"project":"shop","prompt":"Find why the cart test fails","dedupe_key":"ci-1234"}'
ts=$(date +%s)
sig=$(printf '%s.%s' "$ts" "$body" | openssl dgst -sha256 -hmac "$SECRET" | sed 's/^.* //')
curl -X POST "$WEBHOOK_URL" -H "SolidTrace-Timestamp: $ts" -H "SolidTrace-Delivery: ci-1234-1" \
  -H "SolidTrace-Signature: sha256=$sig" -H 'Content-Type: application/json' -d "$body"
```

The delivery is the truth: the Dispatcher starts the Run at once, with the sender as its starter. The answers:

- 202 with `run_id`, the Run's Console link in `url`, and `started` (false when the delivery gave an earlier Run).
- 400 for a body that is not valid; 403 for a Project that is not in the sender's list; 422 for a Project that SolidTrace does not have in the sender's Organization, or a Project with no Factory settings. A 422 also writes a warning Log that names the Project.
- 429 with `Retry-After` when the sender started 10 Runs in the last minute, or has 5 Runs with an Execution queued or live. The operator changes these limits with `DISPATCHER_SENDER_STARTS_PER_MINUTE` and `DISPATCHER_SENDER_ACTIVE_RUNS`.

While an Active Run of the sender has the same `dedupe_key`, a delivery gives that Run and starts none. The Dispatcher also takes a delivery with the same signature as an earlier one as a repeat, because the delivery id is not signed: two deliveries with the same body in the same second give one Run. A refused delivery is not kept, so the sender can send it again.

## Start Runs from Issues and Monitors: the Factory Integration

The **Factory** Integration provider starts a Run for an Issue or a Monitor of SolidTrace. It shows under Settings → Integrations only when an Instance admin set the Dispatcher URL on Settings → Instance. Only an Admin of the Organization adds, changes or deletes a Factory Integration.

- It has no URL field. When an Admin adds it, Console makes a Webhook sender at the Dispatcher as that Admin, with the Integration's name, and keeps the sender's secret encrypted. The sender has no list of Projects, so it starts Runs in each Project of the Organization, and in no other Organization. A Factory Integration has no list of Projects either: the Integration's rules, and the Monitors that name the Integration, decide what starts Runs.
- Console sends each delivery to `/webhooks/generic/<sender id>` under the Dispatcher URL. Where Console's server has no private path to the Dispatcher, an Instance admin sets the Dispatcher's public webhook URL (its `DISPATCHER_WEBHOOK_URL`) on Settings → Instance; then Console sends under that URL.
- Each Issue of a Notification is one delivery, with `dedupe_key` `solid-trace-issue-<Issue id>`. Each change of a Monitor's group is one delivery, with `dedupe_key` `solid-trace-monitor-<Monitor id>-<group>`, or `solid-trace-monitor-<Monitor id>` for a Monitor with no group. So one Issue or one group has at most one Active Run. A Monitor's resolve starts no Run. The delivery id is the Notification's id, so a retry gives the Run that an earlier attempt started.
- The body's `project` is the Project of the Issue or the Monitor. The prompt describes the Issue or the Monitor, `link` opens the Issue page or the Logs explorer, and `attributes` has `solidtrace.notification` (the notification kind) and `solidtrace.issue_number`, or `solidtrace.monitor_id` and `solidtrace.monitor_group`.
- A 429, another error status, or a Dispatcher that does not answer, is a failed delivery: Console tries it again as for Slack. Console does not try again a 4xx other than 429, such as the 422 for a Project with no Factory settings. The Integration's page shows its last failed delivery, with the reason.
- When an Admin deletes the Integration, Console deletes its sender at the Dispatcher. When the Dispatcher does not answer, Console still deletes the Integration, and shows a warning with the sender's id: the sender stays in `solidtrace senders list`, and an Admin deletes it with `solidtrace senders delete SENDER_ID`.

### What an Issue or a Monitor Run does

A delivery from a sender with `solidtrace.notification`, and `solidtrace.issue_number` or `solidtrace.monitor_id`, starts an Issue Run or a Monitor Run.

- **The standing task:** the Dispatcher puts it before the prompt in the Run's first message: find the cause and report it; open a PR only when a check proves the fix (a test that fails before the fix and passes after it). The agent reports the cause with `solidtrace report --outcome done --summary TEXT --cause-report TEXT`. Only a done report of an Issue or a Monitor Run can have a cause report; the Dispatcher refuses it on another Run. `evidence/report.md` has the cause report too.
- **The MCP token:** for each Execution, the Dispatcher makes a child token with `mcp:read` for the Run's Project only, with the Execution's expiry (its total limit plus 1 hour). It never reads the Agent Project or another Project. The wrapper writes it to a file in the Execution's secrets directory, outside the checkout, which the run user reads and cannot change. `SOLIDTRACE_MCP_TOKEN_FILE` names the file and `SOLIDTRACE_MCP_URL` the MCP server. The harness gets the MCP server `solidtrace` (`--mcp-config`), which reads the token from the file. The Dispatcher revokes the token at the end of the Execution. When SolidTrace refuses the token, the Run fails with the reason.
- **The flags:** a done Run with a cause report and no PR shows **Cause report**, and not **no Evidence** or **No PR**. A Run with a PR keeps the usual flags: a PR with no check that passed shows **no Evidence**.
- **The comments on the Issue:** the Dispatcher posts the cause report of a done Issue Run with no PR as a comment on the Issue, with the summary and the Run's link. When an Issue Run opens a PR, it posts one comment with the PR's link, the Run's link, the summary, the pass/fail list of the checks (`no Evidence` when no check passed) and the cause report. Console shows each comment with **Factory** and the Run's link as its author. A Monitor has no comments: a Monitor Run's cause report is on the Run's page only. The Dispatcher sends a comment again while SolidTrace does not answer, for up to 24 hours; it gives up a comment on an Issue that SolidTrace does not have, with a warning Log.

## When an account reaches its usage limit

Each Execution gets one subscription account of the pool: the free account with the fewest live Executions. When the agent's result reports the usage limit, the Execution ends with the reason `usage limit`, and the account is not free until its reset time (the time that Claude Code gives; one hour when it gives none, and at most seven days). `dispatcher accounts list` shows that time.

- **Another account is free:** the Run is queued on it at once, and a new Execution continues the Agent session with `--resume`. The Run's events show `account_switched` with the old and the new account, by the Dispatcher.
- **No account is free:** the Run waits on an account limit. Its reason names the account and the reset time. It continues in a new Execution when an account is free again. A message waits on the Run, and the agent gets it when the Run continues. Stop works on a Run that waits.
- **A report before the limit** sets the outcome: for example, a Run that reported done is done.
- **Switch by hand:** `solidtrace switch-account RUN_ID` moves a Run that waits on an account limit to another free account. With no other account free, it is refused with the first reset time.
- **The fallback of cluster check 5:** with `DISPATCHER_AT_ACCOUNT_LIMIT=wait` (the default is `switch`), a Run stays on its account: at the usage limit it waits for that account's reset time, also when another account is free, and later Executions of the Run use the same account. A person can still switch its account. When the operator removes the account from the pool, the Run continues on a free account.

## Read the Runs

Each member opens **Runs** in the Organization's navigation. The page shows each Run of the factory with an event in the time range (30 days if you set no range), as its last run event shows it: the state and its reason, the title and the Run id, the Project, the repository, the trigger, who started it and the time of the last event. Active Runs come first, then the latest event first.

- Filter by **Project** and by **State**. **Waiting on a person** shows the Runs that wait for an answer, in one click; click it again to see all states. The filters are in the page's URL, so you can share a filtered view.
- Each Run that is not closed has an event in the last 30 days, because a Run closes after 30 days with no activity. To see older Runs, set a longer range.
- Click a Run's title to open its page.

## Read a Run

The Run page shows one Run from its run events:

- The state and its reason, the trigger and who started the Run, the Project, the repository, the folder, the base branch, and the PR with its outcome.
- The **Executions**: for each, its start, its end, its duration, why it ended, the subscription account, the VM, and its form (headless, or take-over). An Execution that has not ended shows "Live".
- While the Run is taken over: who took it over, the VM, and the command that attaches to its terminal there.
- The **interventions**: each message, interrupt, stop, take-over, account switch and continue, with the person who did it. Below them, all the events of the Run in order.
- The **last report**: each check with pass or fail and the last lines of its output, the Evidence files (an image shows on the page; click another file to download it), and `evidence/report.md` in full. A done report with no check that passed shows **no Evidence**. The report of an Issue or a Monitor Run shows its cause report.
- Links to the Run's Agent sessions, and the CLI commands to message the Run, take it over or stop it.

### The totals of a Closed Run

The `closed` run event of a Run carries its totals, so that you can measure the factory with a Query or a Chart on the Agent Project:

- `totals.seconds_to_first_execution` and `totals.seconds_to_pr_opened` (from the trigger), and `totals.seconds_from_pr_opened_to_outcome`. A time is absent when its step did not occur: a Run that closed after 30 days with no activity and no PR has no PR times.
- `totals.interventions.total`, and `totals.interventions.message`, `.interrupt`, `.stop`, `.take_over`, `.account_switch` and `.continue`. They count what people (and senders, forge users and tracker users) did, not what the Dispatcher did by itself, such as an account switch at a usage limit. A message that continues the Run is a message and a continue; the total counts it one time.
- `totals.restarts`: the automatic restarts after a lost machine. `run.executions` counts all the Executions.
- `totals.agent_sessions`, `totals.tokens.input`, `totals.tokens.output`, `totals.tokens.cache_read`, `totals.tokens.cache_write`, `totals.cost_usd` and `totals.no_cost`: the sums of the Run's Agent sessions, as the Agent sessions page shows them, also for a take-over. `totals.cost_usd` is absent when no Agent session has a cost; `totals.no_cost` counts the Agent sessions with no cost.
- `totals.complete` is false when SolidTrace had not yet read all the uploads of the Run's Agent sessions at the close, or did not answer. The Run closes and does not wait.

Each event also has the PR outcome (`run.pr.outcome`), the trigger (`run.trigger`) and the Project (`factory.project`).

### The starter Factory Dashboard

Console makes one Dashboard, **Factory**, for each Organization, so that you see if the factory pays for itself. It is on the Agent Project, has a Dashboard filter, **Project**, and shows 30 days. Its four Charts read the `closed` run events:

- **Tokens for each merged PR:** the mean of the four token sums, for the Runs that closed with `pr_merged`.
- **Cycle time of merged PRs:** the mean of `totals.seconds_to_pr_opened` and `totals.seconds_from_pr_opened_to_outcome` added, in hours, for the same Runs.
- **Acceptance rate:** `pr_merged` over `pr_merged` and `pr_declined` as a percentage. The Query divides two `countif()` counts.
- **Interventions for each Run:** the mean of `totals.interventions.total` for the Runs that closed.

Console makes the Dashboard one time, when it first sees Factory settings of the Organization: when an Admin saves them in a Project's Factory section; at the next read of a Factory section that has settings (a save with the CLI or the API does not pass Console); or at the first read of the Runs page that finds a Run. It needs the Agent Project, so an Organization with none gets it at a later read. After that, Console never makes it again, also when a member deletes it. Any member can change or delete the Dashboard.

**Take over** a running Run with `solidtrace take-over <run id>`. The Run's agent stops, and its Agent session continues in a terminal on the same VM, in a new Execution. The command prints the VM and the command to attach, from the Dispatcher's Take-over setting (`DISPATCHER_TAKE_OVER_ATTACH`, such as ssh to the VM, then `sudo -u <run user> hc connect`). During the take-over, the Stop hook does not ask for a report, and only the total limit applies, not the idle limit. A message to the Run is kept, and starts a new Execution when the take-over ends. When you leave the Agent session with no report, the Run waits on a person with the reason "take-over ended with no report". A report of done gives done.

You can also take over a Run that waits on a person, is done, failed or stopped. The Run is queued, then taken over: a new Execution in the take-over form continues its Agent session in a terminal on a new VM. When that Execution runs, the Run page shows the VM and the command to attach. If the take-over Execution cannot start, the Run waits on a person with the reason "the take-over ended: …". A stop before it starts ends the take-over. A Run that waits on an account limit refuses a take-over, with the reset time: take it over when it runs again. A Closed Run refuses a take-over.

**Stop** ends a Run that goes wrong. It shows only when Settings → Instance has the Dispatcher's URL, and only on a Run that can stop (queued, running, taken over, or waiting on a person or an account limit). Console sends the stop to the Dispatcher as you, with a factory token that lasts 5 minutes. The page shows the state that the Dispatcher gives at once; the stop event shows when it arrives from the Dispatcher.

### The work-in-progress save

An agent machine can go during an Execution: a Spot VM that the cloud takes back, or a machine that stops. So the wrapper saves the work in the checkout to the Run's work-in-progress ref, `refs/agent-wip/<run id>`:

- **The checkpoint:** every 5 minutes, only when the work changed since the last save. It runs on standard VMs too. The operator changes the interval with `DISPATCHER_CHECKPOINT_INTERVAL` (a duration, such as `10m`); `0` turns the checkpoint off.
- **The preemption save:** when the machine goes with a notice, after the agent stops.

A save is a commit of the working tree, with the Run's HEAD as its parent, and the message "WIP [skip ci]: saved by the factory at <UTC time>, Run <id>, Execution <id>, reason <checkpoint|preempted|machine stopped>. Not reviewed." Commits that are not pushed go with it: when the push of the Run's branch fails at a preemption (for example, a person pushed to the branch), the WIP ref keeps the save. The save does not change the agent's index, HEAD or branch, and it does not run the hooks or the programs of the checkout's git configuration.

A save leaves out these files, and the preemption event names them:

- the files that `.gitignore` or `.git/info/exclude` excludes, and the factory file's `save_exclude` patterns;
- each untracked file over 5 MiB, and each untracked file after the first 50 MiB of untracked files.

A submodule with changes saves to the same ref name in its own repository. A submodule whose URL is not on the forge, or that the push cannot reach, is not saved; the preemption event names it. The secret files of a Run and the wrapper's state files are outside the checkout, so a save never has them.

For a forge that refuses refs outside `refs/heads`, the operator sets `DISPATCHER_WIP_REFS=refs/heads/agent-wip`: the saves then go to `refs/heads/agent-wip/<run id>`. The Dispatcher deletes the ref when the Run closes.

The save message also names the files that the save left out, below its first line.

### The restart after a lost machine

When the machine of a headless Execution is preempted, stops, or goes with no notice, the Dispatcher queues the Run again, and a new Execution continues it on a new machine, on the same subscription account when that account is free. It does this at most 3 times in a row. The fourth lost machine in a row fails the Run with "machine lost 4 times in a row". An Execution that ends for another reason starts the count again. The time that the Run waits in the queue for a machine is not part of the new Execution's limits. Each lost machine is a run event: `preemption` when the wrapper got the notice or the signal, `machine_lost` when the machine went with no notice.

The new Execution restores the last save of the lost Execution before the agent starts:

- The Run's branch is at the commit that was HEAD at the save, so commits that were not pushed come back, and the push at the end pushes them. The working tree is equal to the save, and the changes are not staged. No WIP commit is in the branch's history.
- If a person pushed to the Run's branch after the save, the person's push wins: there is no restore, and the Execution starts from the branch. The save is then the ref `refs/factory/saved-work` in the checkout, and the agent takes what it still needs from it.
- The restore uses only a save of the lost Executions. A save of an Execution that ended for another reason is old work, and does not come back.
- If the restore cannot fetch the save, the setup fails and the Run fails, with the save still on the forge. A message continues the Run, and that Execution restores the save.
- Submodules are as the clone gives them: their saved changes stay in their own repositories' WIP refs.
- Until the agent starts, the new Execution makes no save. If its machine also goes during the setup, the WIP ref keeps the lost Execution's save, and the next Execution restores it.

The first message of the new Execution says when the machine went, what is back, the files that the save left out, that containers, background processes, caches and ignored files are gone, and that the last tool call possibly did not finish.

A take-over does not restart by itself: when its machine is preempted or stops, the Run waits on a person with the reason "the take-over ended: preempted" (or "machine stopped"), also when messages are kept. The next Execution restores the save in the same way.

## Try the factory with mock data

To see the factory work with no Claude account, no forge and no Nomad, run the demo on the dev box, with the dev stack up (`bin/dev`):

```bash
bin/factory-demo up
```

The command starts the real Dispatcher and the real wrapper, with fakes for the agent (a scripted Claude Code), the forge and the machines. It connects them to your running Console and EventStore, then starts six Runs. It prints the Console links to open: the **Runs** list and the Project's **Factory** section. Sign in as `admin@solidtrace.local` (the default; give another Admin with `--admin`).

The six Runs:

- Done, with a PR, two checks that passed and one Evidence file.
- Done, with no Evidence (a check failed).
- Needs a person, with a question.
- Failed (the agent's harness exited in the middle of a turn).
- Stopped by the demo while it runs.
- A Run that works until you press its **Stop** button on the Run page.

Each Run has its Agent session, which opens from the Run page.

The demo changes these settings: it turns on Agent sessions when they are off (with a captured repository, `factory-demo.example/demo/*`), sets Settings → Instance's Console URL and Dispatcher URL, and makes two API tokens (the Dispatcher's and an Admin's). Press Ctrl-C, or run `bin/factory-demo down` in another terminal, to stop the Dispatcher and the wrappers and to put back the URLs and delete the tokens. A Run that still works is stopped first. The Runs and their Agent sessions stay in SolidTrace, so you can look at them after the demo. A second `bin/factory-demo up` adds six new Runs. If a demo died with no clean-up, the next `up` or `down` puts its settings back.

Options (after `up` or `down`): `--console-url` (default `http://localhost:4001`), `--ingest-url` (`http://localhost:4000`), `--org`, `--project` and `--admin`. The demo starts no Console and no EventStore.
