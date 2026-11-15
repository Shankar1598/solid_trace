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
