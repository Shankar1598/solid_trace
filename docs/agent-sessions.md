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

When the Dispatcher does not answer, or SolidTrace no longer accepts the Dispatcher's own token (for example, the Admin who made it left the Organization), the section shows that error. **Open this Project's Runs** opens the Runs page with the Project filter set.

## Start Runs from tracker tickets

A ticket of a tracker connection (Notion, Linear) that a person sets to the ready status starts a Run. The Run's prompt is the ticket's title and body, its page links to the ticket, and its actor is the tracker user who set the status.

- **The tracker settings** of a connection: the ready status (default `Ready for agent`) and the routing field (the kind's default, such as the Notion select property `Project`). An Admin saves them with `PUT /api/v1/connections/NAME/tracker` and `{"ready_status": "...", "routing_field": "..."}`; an empty value is the default. `GET /api/v1/connections` shows them.
- **Routes** are part of a Project's Factory settings: each is a tracker connection and a match value. A ticket goes to the Project whose route has the value of the ticket's routing field. The route with an empty match value is the connection's default: it takes each ticket that no other route matches. Two Projects cannot have the same pair: the save answers 409 and names the Project that has it. An Admin saves them in `routes` of `PUT /api/v1/projects/SLUG/settings`; a save with no `routes` keeps them. Each member reads them.
- **No route:** a ready ticket that no route matches starts no Run. It gets one comment that names the routing field and the values that the routes match, and the Dispatcher writes a warning Log. A new edit of the ticket gets the comment and the warning again; the same edit gets none.
- A ready ticket starts at most one Run for each edit, and no Run while it has an Active Run.

## Connect a connection's webhooks

A tracker or a forge tells the Dispatcher of a change with a webhook. The Dispatcher has two listeners: the private listener serves the API, and the webhook listener serves only `POST /webhooks/<kind>/<connection id>`. Each other path and method gets 404. Caddy on the server VM sends only `/webhooks/*` from a public name to the webhook listener (`factory/reference/caddy/Caddyfile`). The operator sets the listener's address in `DISPATCHER_WEBHOOK_LISTEN` and the public name in `DISPATCHER_WEBHOOK_URL`.

1. `solidtrace connections list` shows the webhook URL of each connection whose kind sends webhooks. Paste it into the connection's service.
2. Give the secret that signs the webhooks: `solidtrace connections webhook-secret NAME < secret-file` (an Admin only). The Dispatcher keeps it encrypted, and no answer gives it.

A webhook is only a hint. The Dispatcher checks its HMAC-SHA256 signature on the raw body before it reads the body, and answers 401 when it is not valid. It refuses a body over 1 MiB (413) and more than 120 webhooks a minute from one connection (429). A delivery id that it got in the last 7 days does nothing. A valid webhook gets 200 at once, and the Dispatcher then reads the change from the service's API. It also reads each connection when it starts, and again at each backstop interval (5 minutes for most kinds), so a change is found also when no webhook comes.

`solidtrace connections list` shows the time of each connection's last webhook. When a backstop finds a change that no webhook announced since the previous backstop, the Dispatcher writes a warning Log with the Organization, the connection and the time of the last webhook, so that a Monitor can find a connection whose webhooks stopped. A connection with no webhook secret gets no webhooks, so its backstop does not warn.

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

## Read the Runs

Each member opens **Runs** in the Organization's navigation. The page shows each Run of the factory with an event in the time range (30 days if you set no range), as its last run event shows it: the state and its reason, the title and the Run id, the Project, the repository, the trigger, who started it and the time of the last event. Active Runs come first, then the latest event first.

- Filter by **Project** and by **State**. **Waiting on a person** shows the Runs that wait for an answer, in one click; click it again to see all states. The filters are in the page's URL, so you can share a filtered view.
- Each Run that is not closed has an event in the last 30 days, because a Run closes after 30 days with no activity. To see older Runs, set a longer range.
- Click a Run's title to open its page.

## Read a Run

The Run page shows one Run from its run events:

- The state and its reason, the trigger and who started the Run, the Project, the repository, the folder, the base branch, and the PR with its outcome.
- The **Executions**: for each, its start, its end, its duration, why it ended, the subscription account and the VM. An Execution that has not ended shows "Live".
- The **interventions**: each message, interrupt, stop, take-over, account switch and continue, with the person who did it. Below them, all the events of the Run in order.
- The **last report**: each check with pass or fail and the last lines of its output, the Evidence files (an image shows on the page; click another file to download it), and `evidence/report.md` in full. A done report with no check that passed shows **no Evidence**. The report of an Issue or a Monitor Run shows its cause report.
- Links to the Run's Agent sessions, and the CLI commands to message the Run, take it over or stop it.

**Stop** ends a Run that goes wrong. It shows only when Settings → Instance has the Dispatcher's URL, and only on a Run that can stop (queued, running, taken over, or waiting on a person or an account limit). Console sends the stop to the Dispatcher as you, with a factory token that lasts 5 minutes. The page shows the state that the Dispatcher gives at once; the stop event shows when it arrives from the Dispatcher.
