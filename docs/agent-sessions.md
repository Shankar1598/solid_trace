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

Claude Code gets the plugin when it starts on a machine, and the plugin runs from the next start. Each engineer must still install the CLI and sign in (steps 1 and 3 below).

Step 5 of the Capture setup page gives a managed-settings block that also sends Claude Code's telemetry. See [Send Claude Code's telemetry to SolidTrace](#send-claude-codes-telemetry-to-solidtrace).

## For engineers: turn on capture

**Settings → Agent sessions → Capture setup** in Console gives each command with your instance's version and addresses filled in. Each member of the Organization can open it.

1. Install the `solidtrace` CLI. The script checks the SHA-256 of the file before it installs it in `~/.local/bin`:

   ```bash
   curl -fsSL https://github.com/Shankar1598/solid_trace/releases/download/v0.19.0/install.sh | sh
   ```

2. Install the Claude Code plugin, unless your Admin pushes it with managed settings. Claude Code reads a `github.com` URL as a git repository, so it cannot add the marketplace file from the release's URL. And it does not load a marketplace file that you add as a plain path. So download the file into a `.claude-plugin` folder, and add it from there:

   ```bash
   mkdir -p ~/.local/share/solidtrace/.claude-plugin
   curl -fsSL -o ~/.local/share/solidtrace/.claude-plugin/marketplace.json https://github.com/Shankar1598/solid_trace/releases/download/v0.19.0/marketplace.json
   claude plugin marketplace add ~/.local/share/solidtrace/.claude-plugin/marketplace.json
   claude plugin install solidtrace@solidtrace
   ```

   Then check that the list shows `solidtrace@solidtrace` with `Status: ✔ enabled`:

   ```bash
   claude plugin list
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

   It tells you if capture works on this machine. It marks each setup step with ✓ or ✗ (the sign-in, the Claude Code plugin, the captured repositories list and the uploads) and tells you what to do for each ✗. Then it counts the Agent sessions on this machine as "uploaded", "not captured", "failed" or "not checked yet", and shows the failures and the newest Agent sessions. `solidtrace capture status --all` lists each Agent session.

### What uploads, and when

- The plugin's hooks upload the new part of an Agent session when a turn ends (`Stop`, `SubagentStop`) and when the Agent session ends (`SessionEnd`). `SessionStart` uploads the Agent sessions that a crash or a closed terminal left. So an Agent session shows in Console after its first turn ends.
- A hook never makes Claude Code wait: it starts the upload in the background. A failure goes to `capture.log` in the solidtrace config directory only.
- An Agent session uploads when the folder where Claude Code started is in a git repository (or a worktree of one) with a remote that matches a captured repository. An Agent session that starts in a folder that is not a repository does not upload, also when it works in a captured repository below that folder.
- The SSH and HTTPS remotes of a repository give the same name. The SSH-only hosts of a service give the name of its web host: `git@ssh.bitbucket.org:ws/repo.git` is `bitbucket.org/ws/repo`, and `ssh://git@ssh.github.com:443/acme/shop.git` is `github.com/acme/shop`.
- The CLI gets the list from SolidTrace and keeps it for at most 10 minutes. With no list, it uploads nothing.
- SolidTrace removes the credentials that its Redaction patterns match before it stores an Agent session.

## Read the Agent sessions

Each member opens **Agent sessions** in the Organization's navigation. The page lists each Agent session with activity in the time range (7 days if you set no range), the newest activity first unless you sort by another column: the title, the start, the last activity, the person or the Run, the repository, the model and its effort (as `claude-opus-5-5 · high`), the tokens, the estimated cost, and the counts of messages and tool calls.

The title of an Agent session is the one that Claude Code shows for it:

1. The last name that a person gave it with `/rename`. A later `/rename` replaces an earlier one.
2. Else the last title that Claude Code made for it. Claude Code writes a new title as the Agent session goes on, so the title can change.
3. Else the start of its first person message, on one line. A slash command shows as the command and its arguments, and a shell command as `!` and the command.
4. Else the Agent session has no title, and the pages show the start of its id, in grey.

Only the main transcript gives the title: a subagent's title lines do not change it. A long title is cut with "…" in the list; hold the pointer on it to read all of it. On a phone, the title is the main text of the row, with the person or the Run, the start and the repository below it.

An Agent session that SolidTrace parsed before v0.23.2 has no title, until new lines of it have a title that Claude Code made or a `/rename`. This is also true for an Agent session that was live during the update: when all its lines with a title uploaded before the update, it has no title until Claude Code writes its title again, at its next turn.

The activity of an Agent session is the times of its transcript lines that have a timestamp. Claude Code writes some lines with no timestamp, such as its title, the last prompt and the mode. Such a line is not activity, so an old Agent session that uploads again, or that a person only opens again, does not show as active at the time of the upload. A transcript with no line that has a timestamp has no activity, so it is in no time range. Its page shows the time when SolidTrace stored it as its first and last activity. Agent sessions that SolidTrace parsed before v0.23.3 keep their times.

- Filter by time, person, repository, **Project (Runs)**, and Run or no Run. The Project filter selects the Agent sessions of Runs only, because an engineer's own Agent session has no Project. Use the repository filter to find those. The **Project (Runs)** and Run filters show only when the Factory is on, and the Project list holds only the Projects whose Runs have Agent sessions in the range. The filters are in the page's URL, so you can share a filtered view.
- **Last activity** shows how long ago it was: "a few seconds ago" in its first minute, then "5 minutes ago", "3 hours ago" or "2 days ago". It changes each 30 seconds while the page is open. The hover text shows the time. A live Agent session is at the top, with "a few seconds ago" after a reload of the page.
- Sort the list by **Start**, **Last activity**, **Tokens** or **Estimated cost**: click the column's heading. The first click shows the latest times, the most tokens or the highest cost first; a second click turns the order around. An arrow next to the heading shows the sorted column and its direction. An Agent session with no cost comes after those with a cost, in both directions. The order is in the page's URL (`sort` and `direction`), with the filters, and a change of the range or a filter keeps it.
- The totals of tokens and estimated cost are for the Agent sessions that the filters select. The page shows the first 500 of them in its order; the totals include all of them. SolidTrace sorts all of them before it takes the 500, so the most expensive Agent sessions of the range are always on the page.
- The model and the effort of an Agent session are those of the latest reply in its main transcript, not in a subagent's. The effort is the one that Claude Code writes on each reply (`low`, `medium`, `high`, `xhigh` or `max`), from Claude Code 2.1.296. An Agent session that SolidTrace parsed before v0.24.0 has no effort until new lines of it are parsed; its page says "Not known".
- The cost is an estimate at list prices, not an invoice. It is Claude Code's own estimate when the transcript has one after its last reply, and else the tokens at the list price of each model. An Agent session whose model has no list price has no cost, and the totals say how many Agent sessions they do not include.

### Read one Agent session

Click a row in the list to open the Agent session page.

- The page's heading is the title of the Agent session.
- The header shows the person or the Run, the repository, the folder, the parent Agent session (a link, for a fork or a `/clear`), the model, the effort, the first and last activity, the tokens by kind, the estimated cost and the flags.
- The timeline shows the person messages, the agent replies and the tool calls in time order, the oldest step first, as a chat reads. Choose **Newest first** above the timeline to see the newest step first; **Oldest first** goes back. Your browser keeps the choice, with the choices of what to show. Each tool call shows its input, with its output below it. The agent's replies and its thinking show as Markdown; a person's messages show as plain text. The title of each reply shows its model and its effort, as `claude-opus-5-5 · high`, but not on a small screen.
- A message that a person sent while the agent worked shows at the time when the agent got it, not at the time when the person wrote it.
- Hook feedback, subagent prompts and the notices of background tasks show as "Not from a person". In an engineer's own Agent session, each person message is from that engineer.
- A slash command, such as `/compact`, shows once, as a person message in mono: the command and its arguments. Claude Code's output of the command shows under it, after a ⎿ mark, with no colour codes of the terminal. The output is "Not from a person": it is not a person message, and not a turn. Claude Code's caveat before a command is for the model, and does not show.
- A shell command that a person runs with `!`, such as `! terraform plan`, shows in the same way: once, in mono, as `!` and the command, with its output under it after a ⎿ mark. The output is the command's stdout and then its stderr, with no colour codes of the terminal. The output is "Not from a person". Logs that SolidTrace parsed before v0.24.1 keep the role "person" for the output, but the page still shows it under the command.
- After a `/compact`, or when the context is full, Claude Code writes a summary of the conversation as a message. It shows as one quiet line, **Compaction summary**. Click it to read the summary. Claude Code's tags in it, such as `<tools-used-before-summary>`, show as headings.
- A subagent shows inside the tool call that started it. Click it to open its steps.
- A long Agent session loads in pages. In the oldest order it loads from its start: click **Load more steps** at the end of the timeline for the later steps. In the newest order it loads from its end: click **Load earlier steps** at the end for the earlier steps.
- A **cut** or **partial** flag means that the record is not complete, so a resume of this Agent session can fail. The page says so above the header. When a Run's next Execution finds its current Agent session so, it does not resume it: a new Agent session joins the Run, starts with the task and the messages, and becomes the Run's current Agent session. A value marked **cut** was cut at the Log limits; the raw line has all of it.
- Click **Show full** on a cut value to read its raw line, as the record keeps it: with the secrets redacted.
- To find a step, type in the search box above the timeline. The search shows the steps of this Agent session, and of its subagents, that have the text, with any case. Click **Clear** to see the timeline again.

### Search across Agent sessions

To find which Agent session did a thing, type in the search box at the top of the Agent sessions page. It opens the Logs explorer on the Agent Project, with your text as the search, over the last 7 days. Each line of a transcript is a Log there: `file` is the transcript's path, and the attributes under `agent.` hold the Agent session id, the role and the tool name. A reply's Log has its model in `agent.model` and its effort in `agent.effort`. Choose a longer range in the explorer when you accept a longer wait.

An agent can do the same over MCP: `query_logs` on the Agent Project runs a Query on the transcript Logs. `list_projects` names the Agent Project.

### Over MCP

An agent reads the Agent sessions with two MCP tools ([MCP](mcp.md)): `list_agent_sessions` lists them, each with its title, with the filters of the Agent sessions page, and `get_agent_session` reads one Agent session's header and its timeline in pages, or searches it. A token with `mcp:read` can call both. `list_agent_sessions` gives at most 50 Agent sessions, or the `limit` that the agent sets, at most 200, with the totals of all of them. Its `sort` (`last`, `start`, `tokens` or `cost`) and `direction` (`desc` or `asc`) sort them as the page's headings do, before the limit: `sort: cost` gives the most expensive Agent sessions first. When it says `more`, the agent narrows the range or filters by person or repository.

## Record what each review found: the skills plugin

The SolidTrace marketplace has a second plugin, `solidtrace-skills`, which is not on by default. It reviews a change in two stages, opens its PR, works on the PR's feedback, and records every finding with every decision on it:

- **`/solidtrace-skills:code-review`** reviews one step of a feature. The agent can also start it after each step of a build. Three reviewers run at the same time: bugs and spec, and tests, on the session's model, and code quality on Sonnet. They read the repository's `REVIEW.md` sections and its coding standards file, when these exist. The agent decides each finding: `fix`, `dispute`, `trivial`, `defer` or `ask`. A verifier checks each finding that the agent disputes or calls trivial. The agent records a `solidtrace-findings` block with `solidtrace review block`, which checks the block. The block is in the transcript inside that tool call, not in the reply text, and a subagent's blocks are read from its own transcript. The block has the model and effort of each reviewer. Each finding has an id (`F1`, `F2`) and its events (`found`, `triage`, `verify`). A later review keeps the id for the same root cause. After the fixes, the agent commits them with a `Review-Findings: run 1: F2, F5` trailer. Then `solidtrace review fixed` records the update block of that commit, which gives each fixed finding the commit and its files. A changed decision goes in an update block too. So a fix can be found from the commit too, with `git log --format='%H %(trailers:key=Review-Findings,valueonly)'`.
- **`/solidtrace-skills:deep-review`** tries to break the whole branch with several adversaries, each in its own worktree, and keeps only the findings that a run proves again.
- **`/solidtrace-skills:open-pr`** runs the final stage, then pushes the branch and opens its PR (Bitbucket, GitHub, or `solidtrace pr` in a Run). The final stage reviews the whole branch diff, in this order:
  1. the deep review;
  2. the high availability and infrastructure reviewer;
  3. the person's decisions, and the fixes;
  4. security, last, because a fix can add a fault.

  When the branch already has an open PR, `open-pr` stops and points to `update-pr`. `/solidtrace-skills:open-pr local` does all of this, but does not push. It writes the PR to `solidtrace-pr/<branch>.md` in the repository's Git folder, and the record gets `"pr": "local:<path>"`. Use it to test the skills, or to read the PR before it goes out.

  While the branch pushes, `solidtrace review record` builds the mechanical part of the record from the digest: the runs, the ids, each finding's events, the fixes and the outcomes. A Sonnet subagent that can only read files adds notes: the story, what the person said, and what the reviews missed. The CLI merges the notes and works out the outcomes again. Each finding keeps its events in time order:
  - what the reviewer found;
  - the agent's decision, and the verifier's verdict;
  - what the person said, and what the agent then found and changed;
  - the fix.

  A problem that the person, a later agent or a test run found is a finding too, with `found_by`. So the record also shows what the reviews missed. The record tells the `story` of the feature, and gives the outcome of each finding: `fixed`, `ignored_by_person`, `disputed`, `deferred`, `ignored_trivial` or `open`. The PR body shows the record for the human reviewer. The reply ends with it in a `solidtrace-pr-record` block.

- **`/solidtrace-skills:update-pr`** works on the feedback on the open PR: its comments, its reviews (also from a review bot) and its failed checks. Each change request becomes a finding with `found_by` (the comment's author, the bot or the check). The agent decides and fixes each one, runs security on the fix diff, and replies to each comment. Then it writes the next round of the record, and updates the PR body. A record replaces the earlier rounds of the same PR. `update-pr local` reads the feedback from `<branch>.comments.md` next to the local PR file.

The repository can set the model and effort of each role in `.solidtrace/review.toml`:

```toml
[models]
code-quality = "sonnet high"
deep-review = ["opus xhigh", "opus xhigh", "opus xhigh"]
record = "sonnet"
```

The roles are `bugs-and-spec`, `tests`, `code-quality`, `verifier`, `ha-and-infrastructure`, `security`, `deep-review`, `record` and `sessions`. `inherit` is the session's model. A role with no line keeps its default. The defaults are the values above, `haiku` for `sessions`, and `inherit` for the other roles. Each block records the model and effort that ran, so a team can compare them.

A feature often spans several Agent sessions. Before the record, `open-pr` runs `solidtrace review sessions`, which lists the sessions on this machine that made one of the PR's commits (`strong`), checked out its branch, or changed one of its files. The person confirms the list. When no other session has a link, a Haiku subagent reads the titles of the recent sessions in the repository, and the person confirms only the ones it names.

The record reads `solidtrace review digest --session A --session B`, not the transcripts: one short timeline of the confirmed sessions in time order. Before a session's first review, it keeps the task, the person's messages, the delegations and the commits. From the first review on (a review start, or a findings block in the agent's text or a subagent's report), it also has the agent's texts, the findings blocks, the subagents' reports and the edits. Each commit event keeps the commit's `Review-Findings` trailers whole. Then `solidtrace review check --digest D --record R` checks the record against the digest: each block finding and each trailer reaches the record, each outcome agrees with its events, and each claim cites an event of the digest. It exits 1 when it finds a problem. A session on Bitbucket needs `BITBUCKET_TOKEN` for `open-pr` to open the PR. Without it, `open-pr` gives the link that opens a new PR, and the body to paste.

In a captured repository, the record uploads with the Agent session, and SolidTrace keeps it as the PR's **Review record** (see [Read the Review records](#read-the-review-records)).

## Read the Review records

Each member opens **Review records** in the Organization's navigation, next to **Agent sessions**. The page has one row for each PR whose `open-pr` or `update-pr` Agent session uploaded, the PR stored last first: the PR, the round, the time when SolidTrace stored it, the counts of findings, fixed findings, findings that the reviews missed and confirmations not given, and a link to its Agent session. A row shows the newest record of its PR: the highest round. Next to the round, the page says how many earlier records the PR has. A record with no PR (`"pr": null`) has a row of its own for its Agent session, named **No PR**. The page shows the 500 PRs stored last.

- A finding that the reviews missed is one that a person, an agent or a test run found (`found_by`), not a reviewer.
- A confirmation not given is one whose `given` is false or not known.
- **Open the PR** opens its PR. A local PR (`local:<path>`) has no link.

Click the PR to open its record. The page shows:

- the PR, its round, the time it was stored, its Agent session and the counts;
- the story of the feature;
- the review runs, with the skill, the stage, the step, the reviewers with their models and efforts, and the verifier;
- each finding with who found it (**missed** when the reviews did not), its severity, its title and place, its decisions in the order of its events (as `trivial, verifier pushed back, person asks fix, fixed`), and its outcome. **Details** shows its scenario and the words of its events: why, what the person said, what the agent found, and the files;
- the confirmations, each **Given**, **Not given** or **Not known**;
- the rounds of the PR, when it has more than one. Click a round to open it.

The record is what the agent wrote, with the words of PR commenters in it, so the pages show all of it as text, never as HTML or Markdown.

SolidTrace reads the record from the final reply of the Agent session: the last `solidtrace-pr-record` block of an agent reply in the main transcript, when it is valid JSON of version 2 ([ADR 0024](adr/0024-review-records-come-from-the-final-reply-of-an-agent-session.md)). So:

- A record shows when its Agent session uploads: in a captured repository, or in a Run.
- An Agent session that SolidTrace parsed before Review records came has no Review record. SolidTrace does not read the old lines again.
- A record that is not valid JSON, or not version 2, does not show. A record in a person's message, a subagent or a tool output does not show.
- The records follow the Agent Project's Retention, as the Agent sessions do.

### Over MCP

An agent reads the Review records with two MCP tools ([MCP](mcp.md)): `list_review_records` lists the newest record of each PR, as the page does, and `get_review_record` reads one record with the rounds of its PR. It gives the findings in pages of 30, and `findings_from` and `findings_limit` select a page. A token with `mcp:read` can call both. To compare the reviews of many PRs, an agent lists the records, then reads each one, page by page, until `next_findings_from` is `null`.

## Send Claude Code's telemetry to SolidTrace

Claude Code can send OpenTelemetry events and metrics while it works. SolidTrace stores them as Logs of the Agent Project, next to the Agent sessions. The records arrive in real time, but they do not hold the transcript.

### What the records hold

- An event for each prompt, reply, API request, tool result and hook, with its counts and durations: for example the tokens, `duration_ms`, the tool name and if the tool call was a success.
- The metrics: the cost, the tokens, the sessions, the lines of code, the commits, the pull requests and the active time.
- The model and the effort of each API request.
- The cost (`cost_usd` on `claude_code.api_request`, and the `claude_code.cost.usage` metric) is Claude Code's estimate at list prices. It is not an invoice, the same as the cost on the Agent sessions page.
- The engineer's email address, the account id and the Claude Code session id, on each record.
- The repository: `vcs.repository.url.full`, `vcs.owner.name`, `vcs.repository.name` and `vcs.provider.name`.

The records do not hold the text of a prompt or a reply (Claude Code sends `<REDACTED>`), Bash commands or tool inputs. The settings do not set `OTEL_LOG_USER_PROMPTS` or `OTEL_LOG_TOOL_DETAILS`, because they send that text. Do not add them: Claude Code sends the records of each repository on the machine, also of the repositories that SolidTrace does not capture. Without `OTEL_LOG_TOOL_DETAILS`, Claude Code also hides the names of your own skills and subagents (as `custom_skill` and `custom`). To see which skills ran, read the Agent sessions.

### Turn it on

1. An Admin opens the Agent Project's page in **Projects** and clicks **Generate a key**. The Agent Project can have only one key. Each member of the Organization can read it.
2. Open **Settings → Agent sessions → Capture setup**, step 5, **Send telemetry (optional)**. It gives the settings with your instance's Ingest URL and the key filled in.
3. Add the `env` block to `~/.claude/settings.json` on each machine:

   ```json
   {
     "env": {
       "CLAUDE_CODE_ENABLE_TELEMETRY": "1",
       "OTEL_LOGS_EXPORTER": "otlp",
       "OTEL_METRICS_EXPORTER": "otlp",
       "OTEL_EXPORTER_OTLP_PROTOCOL": "http/protobuf",
       "OTEL_EXPORTER_OTLP_ENDPOINT": "https://solidtrace.example.com:4443",
       "OTEL_EXPORTER_OTLP_HEADERS": "Authorization=Bearer <key>",
       "OTEL_METRICS_INCLUDE_REPOSITORY": "1"
     }
   }
   ```

   Or, if your team uses Claude Code's managed settings, use step 5's managed-settings block in place of the block of step 2. It has the plugin and the `env` block, so each machine gets both.
4. Start Claude Code again. Claude Code reads these variables only when it starts.

Claude Code ignores these variables in a repository's `.claude/settings.json`. Put them in the user settings or in the managed settings.

### Which records SolidTrace keeps

- Ingest keeps a record only when its `vcs.repository.url.full` matches a captured repository. It drops each other record when it arrives, and does not store it: the records of other repositories, and the records with no repository. Claude Code sends two events (`managed_settings_resolved` and the first `hook_execution_start`) before it reads the repository, so SolidTrace always drops them.
- When the captured repositories list is empty, Ingest drops every record.
- Claude Code reads only the `origin` remote, one time, in the folder where the session starts. So the records of a captured repository arrive only from a clone whose `origin` is that repository. SolidTrace drops all the records of a clone with no `origin`.
- A change to the captured repositories applies to the next request.
- Claude Code gets a success for a dropped record, so it shows no warning. The Overview shows the dropped records in **Received → Kept → Stored**, as a slice after Kept and as **Filtered out as not of a Captured repository** below it. Look there when no record arrives.
- The key works only on `/v1/logs` and `/v1/metrics` ([ADR 0023](adr/0023-the-agent-project-takes-one-key-for-opentelemetry-only.md)). It cannot send JSON Logs, a Sentry SDK's Events or Spans.

### Read the records

SolidTrace stores each record of the key in the file `otel`, and keeps a `file` that the sender sends as `original.file`. The Agent sessions and the Runs are in other files, so start each Query with `file == "otel"`.

Open **Logs** and choose the Agent Project. Each event is a Log with the event's name as its message, such as `claude_code.api_request`. Each metric data point is a Log with the metric's name as its message and its number in `metric.value` (see [Metrics](logs/opentelemetry.md#metrics)). Claude Code sends delta metrics, so `sum(metric.value)` adds them up correctly.

The estimated cost by model and effort:

```
where file == "otel" and message == "claude_code.cost.usage"
| summarize sum(metric.value) by model, effort
```

The tokens by kind (`input`, `output`, `cacheRead` and `cacheCreation`) and model:

```
where file == "otel" and message == "claude_code.token.usage"
| summarize sum(metric.value) by type, model
```

The estimated cost by engineer and day:

```
where file == "otel" and message == "claude_code.cost.usage"
| summarize sum(metric.value) by user.email, bin(time, 1d)
```

The estimated cost by repository:

```
where file == "otel" and message == "claude_code.cost.usage"
| summarize sum(metric.value) by vcs.repository.name
```

The tool calls, their failures and their time, by tool. On a tool result, Claude Code sends `success` and `duration_ms` as text, so the Query compares `success` with `"false"` and changes `duration_ms` to a number with `toint`:

```
where file == "otel" and message == "claude_code.tool_result"
| summarize calls = count(), failed = countif(success == "false"), p95_ms = percentile(toint(duration_ms), 95) by tool_name
| sort by calls desc
```

The API requests and their time, by model:

```
where file == "otel" and message == "claude_code.api_request"
| summarize count(), avg(duration_ms), percentile(duration_ms, 95) by model
```

An agent can run the same Queries over MCP, with `query_logs` on the Agent Project.

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
- Linear's **Comment** webhook and Notion's comment events make the Dispatcher read the ticket's comments at once. The poll of a Notion connection with no webhook (every 30 seconds) reads the comments of each ticket whose Run is an Active Run. At most every 5 minutes, the backstop or the poll also reads the comments of each ticket whose Run is done, failed or stopped, so a comment on such a ticket with no webhook can take up to 5 minutes.
- On Notion, write the `/agent` comment as a comment on the page (the comments at the top of the page). The Dispatcher reads only the page's own comments: an inline comment on a block of the page does nothing.
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
- These comments do nothing: a comment on a public repository (when a trigger comment comes while the repository is public, as its webhook says or as Bitbucket says at the step, each comment on the PR at that time never goes to the Run, also after the repository becomes private again), a comment by a bot, an app or an access token, a comment by the account that opened the PR (the factory's own identity), and a resolved or deleted comment. The comments of such accounts are not in the message.
- On a Closed Run, the Dispatcher answers the trigger comment one time: the Run is closed, and a new Run continues the work. A PR gets at most one such answer in a day.
- With no webhook, a comment does nothing: write a new trigger comment when the webhook works again.
- When the Dispatcher cannot read the PR's comments (for example, Bitbucket is down) or cannot post the answer of a Closed Run, it tries again at each PR check (every 5 minutes). After 4 failed tries it stops and writes a warning Log that names the repository and the PR: write a new trigger comment when the cause is fixed. A restart of the Dispatcher forgets these tries.

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
- 400 for a body that is not valid, or for an attribute whose name starts with `solidtrace.` from a sender that is not a Factory Integration's (below); 403 for a Project that is not in the sender's list; 422 for a Project that SolidTrace does not have in the sender's Organization, or a Project with no Factory settings. A 422 also writes a warning Log that names the Project.
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

A delivery from a Factory Integration's sender with `solidtrace.notification`, and `solidtrace.issue_number` or `solidtrace.monitor_id`, starts an Issue Run or a Monitor Run. Console makes that sender as the Factory Integration's (`"factory": true` in the Admin's call to make it). The Dispatcher refuses each attribute whose name starts with `solidtrace.` from any other sender, so another sender cannot get an MCP token or comment on an Issue.

- **The standing task:** the Dispatcher puts it before the prompt in the Run's first message: find the cause and report it; open a PR only when a check proves the fix (a test that fails before the fix and passes after it). The agent reports the cause with `solidtrace report --outcome done --summary TEXT --cause-report TEXT`. Only a done report of an Issue or a Monitor Run can have a cause report; the Dispatcher refuses it on another Run. `evidence/report.md` has the cause report too.
- **The MCP token:** for each Execution, the Dispatcher makes a child token with `mcp:read` for the Run's Project only, with the Execution's expiry (its total limit plus 1 hour). It never reads the Agent Project or another Project. The wrapper writes it to a file in the Execution's secrets directory, outside the checkout, which the run user reads and cannot change. `SOLIDTRACE_MCP_TOKEN_FILE` names the file and `SOLIDTRACE_MCP_URL` the MCP server. The harness gets the MCP server `solidtrace` (`--mcp-config`), which reads the token from the file. The Dispatcher revokes the token at the end of the Execution. When SolidTrace refuses the token, the Run fails with the reason.
- **The flags:** a done Run with a cause report and no PR shows **Cause report**, and not **no Evidence** or **No PR**. A Run with a PR keeps the usual flags: a PR with no check that passed shows **no Evidence**.
- **The comments on the Issue:** the Dispatcher posts the cause report of a done Issue Run with no PR as a comment on the Issue, with the summary and the Run's link. When an Issue Run opens a PR, it posts one comment with the PR's link, the Run's link, the summary, the pass/fail list of the checks (`no Evidence` when no check passed) and the cause report. Console shows each comment with **Factory** and the Run's link as its author. A Monitor has no comments: a Monitor Run's cause report is on the Run's page only. A comment has at most 65,536 characters, as Console takes. When the summary and the cause report make it longer, the Dispatcher cuts the end of the summary first, then the end of the cause report, and the comment ends with a line that gives the Run's page, which has the full text. The comment names the Project by its slug at the time of the comment, so a renamed Project still gets it. The Dispatcher sends a comment again while SolidTrace does not answer, for up to 24 hours; it gives up a comment on an Issue or a Project that SolidTrace does not have, with a warning Log.

## When an account reaches its usage limit

Each Execution gets one subscription account of the pool: the free account with the fewest live Executions. When the agent's result reports the usage limit, the Execution ends with the reason `usage limit`, and the account is not free until its reset time (the time that Claude Code gives; one hour when it gives none, and at most seven days). `dispatcher accounts list` shows that time.

- **Another account is free:** the Run is queued on it at once, and a new Execution continues the Agent session with `--resume`. The Run's events show `account_switched` with the old and the new account, by the Dispatcher.
- **No account is free:** the Run waits on an account limit. Its reason names the account and the reset time. It continues in a new Execution when an account is free again. A message waits on the Run, and the agent gets it when the Run continues. Stop works on a Run that waits.
- **A report before the limit** sets the outcome: for example, a Run that reported done is done.
- **Switch by hand:** `solidtrace switch-account RUN_ID` moves a Run that waits on an account limit to another free account. With no other account free, it is refused with the first reset time.
- **The fallback of cluster check 5:** with `DISPATCHER_AT_ACCOUNT_LIMIT=wait` (the default is `switch`), a Run stays on its account: at the usage limit it waits for that account's reset time, also when another account is free, and later Executions of the Run use the same account. A person can still switch its account, also when the Run is queued and waits for its account. When the operator removes the account from the pool, the Run continues on a free account.

## The first message of a Run's Agent session

The wrapper puts the factory's commands before the task in the first message of each Agent session that an Execution starts, for each Run: `solidtrace check` (the checks of the factory file, or `solidtrace check COMMAND`), `solidtrace pr --title TEXT --description TEXT` (push the Run's branch and open its PR) and `solidtrace report --outcome done|needs-a-person|failed --summary TEXT`. A resume of the Agent session does not give them again. An Issue or a Monitor Run also gets its standing task.

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

The Dispatcher sends each run event at least once, so a `closed` event can come two times. Each Query has a `distinct` stage on the time, `run.id`, `run.seq` and its value, so that a Run counts one time. The Queries are in `console/config/factory_dashboard_queries.yml`, and EventStore's tests run them on closed run events.

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

A submodule with changes saves to the same ref name in its own repository. A submodule with changes whose URL is not on the forge, or that the push cannot reach, is not saved; the preemption event names it, and the next checkpoint tries the push again. A submodule with no changes is not a part of the save. The secret files of a Run and the wrapper's state files are outside the checkout, so a save never has them.

For a forge that refuses refs outside `refs/heads`, the operator sets `DISPATCHER_WIP_REFS=refs/heads/agent-wip`: the saves then go to `refs/heads/agent-wip/<run id>`. The Dispatcher deletes the ref when the Run closes.

The save message also names the files that the save left out, below its first line.

### The restart after a lost machine

When the machine of a headless Execution is preempted, stops, or goes with no notice, the Dispatcher queues the Run again, and a new Execution continues it on a new machine, on the same subscription account when that account is free. It does this at most 3 times in a row. The fourth lost machine in a row fails the Run with "machine lost 4 times in a row". An Execution that ends for another reason starts the count again. The time that the Run waits in the queue for a machine is not part of the new Execution's limits. Each lost machine is a run event: `preemption` when the wrapper got the notice or the signal, `machine_lost` when the machine went with no notice.

The new Execution restores the last save of the lost Execution before the agent starts:

- The Run's branch is at the commit that was HEAD at the save, so commits that were not pushed come back, and the push at the end pushes them. The working tree is equal to the save, and the changes are not staged. No WIP commit is in the branch's history.
- If a person pushed to the Run's branch after the save, the person's push wins: there is no restore, and the Execution starts from the branch. The save is then the ref `refs/factory/saved-work` in the checkout, and the agent takes what it still needs from it. The wrapper also pushes the save to `refs/agent-wip/<run id>-kept`, because the next save replaces the WIP ref; the Dispatcher deletes that ref with the WIP ref when the Run closes. If that push fails, the setup fails, and a message continues the Run.
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

- Done, with a PR, two checks that passed and one Evidence file. The demo merges its PR on the fake forge, so the Run closes with the reason `pr_merged`, and the starter **Factory** Dashboard draws its Charts from that one closed event.
- Done, with no Evidence (a check failed).
- Needs a person, with a question.
- Failed (the agent's harness exited in the middle of a turn).
- Stopped by the demo while it runs.
- A Run that works until you press its **Stop** button on the Run page.

Each Run has its Agent session, which opens from the Run page.

The demo changes these settings: it turns on Agent sessions when they are off (with a captured repository, `factory-demo.example/demo/*`), sets Settings → Instance's Console URL and Dispatcher URL, and makes two API tokens (the Dispatcher's and an Admin's). Press Ctrl-C, or run `bin/factory-demo down` in another terminal, to stop the Dispatcher and the wrappers and to put back the URLs and delete the tokens. A Run that still works is stopped first. The Runs and their Agent sessions stay in SolidTrace, so you can look at them after the demo. A second `bin/factory-demo up` adds six new Runs. If a demo died with no clean-up, the next `up` or `down` puts its settings back.

Options (after `up` or `down`): `--console-url` (default `http://localhost:4001`), `--ingest-url` (`http://localhost:4000`), `--org`, `--project` and `--admin`. The demo starts no Console and no EventStore.
