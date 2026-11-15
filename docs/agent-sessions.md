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
