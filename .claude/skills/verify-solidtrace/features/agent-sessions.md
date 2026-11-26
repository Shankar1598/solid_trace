# Agent sessions

SolidTrace records the Claude Code Agent sessions of an Organization's engineers, as transcripts in the Organization's Agent Project. An admin turns on Agent sessions and lists the captured repositories. An engineer signs in the `solidtrace` CLI with the device flow, and the plugin's SessionEnd hook runs `solidtrace capture`, which uploads the sessions of captured repositories only. Each member reads the Agent sessions page (rows, totals, person and repository filters) and an Agent session page (steps, subagents inside their parent, secrets shown as `*`).

## Sub-features

- `sessions-turn-on`: Settings → Agent sessions, one captured repository on each line, the confirm, then `Turn on Agent sessions`.
- `sessions-setup`: Settings → Agent sessions → Capture setup shows the commands with this install's version and addresses.
- `sessions-device-login`: `solidtrace login --device --ingest-url <ingest> <console origin>` prints `open <url> and type this code: <code>`. The page has the `Code` field, `Continue`, the note `What uploads`, and `Approve`.
- `sessions-capture`: `solidtrace capture` uploads the transcripts of captured repositories, and never the others.
- `sessions-list`: `/<org>/agent_sessions` has the table `Agent sessions` with `tr[data-session]` rows, the region `Totals of the rows`, and person and repository comboboxes.
- `sessions-nav`: the sidebar's **Coding agents** group (`button` with `aria-expanded`) holds the links Agent sessions and Review records. It is open on their pages, and elsewhere stays as the viewer last left it, also after a reload. Only the link of the page has `aria-current="page"`.
- `sessions-page`: `/<org>/agent_sessions/<id>` has `[data-section="agent-session-header"]`, `[data-step=...]` steps, `[data-subagent]` inside its `Agent` tool step, `Show full` on a cut output, and secrets as `*`.

## How to get to it (user POV)

- Sidebar group **Coding agents**, link **Agent sessions**, which opens `/<org>/agent_sessions`.
- Settings → Agent sessions (an admin), and Capture setup (any member).
- `solidtrace login --device` and `solidtrace capture` in a terminal, or the Claude Code plugin's hook.

## Driving it with verify

Preconditions:

- The baseline stack is up, and `"$V" doctor` is all `ok`.
- Go can build `cli/`. The flow builds it into `console/tmp/`.

- **Capture to pages.** `"$V" flow -- e2e/flows/agent-sessions.spec.ts`. Pass: the admin and the member sign in with the device flow, the fixture transcripts upload through `solidtrace capture`, and the list and the page show them with their totals, filters, subagent and a redacted secret.
- **Coding agents group.** `"$V" flow -- e2e/flows/coding-agents-nav.spec.ts`. Pass: the group opens and closes, keeps the choice after a reload, works with no localStorage, and lights only the link of the page. The test folder has screenshots in light and dark.
- **Setup page.** `"$V" snap --path /solid-trace-org/settings/agent_sessions/setup --name capture-setup --as member`. Pass: status 200, and the ARIA snapshot has the install command with this stack's ports.
- **Side effect.** `curl -s 'http://127.0.0.1:4201/?user=solidtrace' --data-binary 'SHOW TABLES FROM solidtrace_verify_s1'`, then count the rows of the Agent transcripts table that it lists. The count goes up after a capture.
- **Proof.** The traces in `<run>/evidence/flow-<time>/playwright/`.

## Gotchas

- OAuth needs a Console URL. `verify up` sets it to this stack's origin, and the flow sets it again and puts it back.
- ClickHouse keeps the rows of earlier runs in the same slot's database until `down`. The flow uses its own repositories under one captured entry for each run, so old rows do not change its counts.
- The device flow page goes to the next state only after `Approve`. Wait for the `status` role, then for the CLI to exit 0.
- Upload filters by the captured repository list. A transcript in a repository that is not listed must not upload. A test that sees it upload has found a fault.
