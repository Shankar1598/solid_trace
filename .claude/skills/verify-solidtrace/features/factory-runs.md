# Factory Runs

A Run is one piece of work that the factory's Dispatcher gives to an agent. A member starts a Run with the `solidtrace` CLI. A Monitor or a new Issue can also start one through the Factory Integration. The member then follows the Run on the Runs page and on the Run page (events, Executions, report, interventions), and can stop it with the Stop button. An admin sets the Project's repository and forge connection in the Project's Factory section, and a member sees that section read-only. The Execution's child token reads only its own Agent session and stops working when the Execution ends.

## Sub-features

- `runs-factory-section`: an admin edits `#factory-repository` in `[data-section="factory"]` on the Project page, and a member sees it `readonly`.
- `runs-cli-settings`: `solidtrace connections set forge` and `solidtrace projects set|get` from an admin. A member's token is refused with `only an Admin`.
- `runs-start-cli`: `solidtrace start --project <slug> "<prompt>"` prints `Run <id> is queued.` and the Run page's URL.
- `runs-list`: `/<org>/runs` has `tr[data-run="<id>"]` with the state (`Running`, `Stopped`, ...) and the prompt.
- `runs-page`: `/<org>/runs/<id>` shows the Run id, `[data-section="events"] [data-event=...]`, `[data-section="executions"]`, `[data-section="report"]` and `[data-section="interventions"]`.
- `runs-stop`: `[data-action="stop-run"]` (a confirm dialog) ends the Execution. Events `execution_ended` and `stop` appear, the intervention says `by <email>`, and the button goes away.
- `runs-token-scope`: the Execution's token gets 200 on its own Agent session, 403 on another, and 401 after the Execution.
- `runs-integration`: a Monitor with the `Factory` Integration starts one Run with a `Cause report`. A new Issue starts an Issue Run, whose token reads only its Project over MCP, and whose cause report is a comment by `Factory` (`factory-integration.spec.ts`).
- `runs-demo`: `bin/factory-demo up|down` starts six Runs of different outcomes, and puts back the settings (`demo.spec.ts`).

## How to get to it (user POV)

- Sidebar link `Runs`. It is there only when the install has a Dispatcher URL.
- `solidtrace start`, `solidtrace runs`, `solidtrace stop`, `solidtrace message`, `solidtrace take-over` and `solidtrace interrupt` in a terminal, after `solidtrace login --device`.
- The Project page's Factory section (`/<org>/projects/<project>`).
- Settings → Integrations → New, kind `Factory`, then a Monitor that tells it.

## Driving it with verify

Preconditions:

- The baseline stack is up, and `"$V" doctor` is all `ok`.
- Go can build `cli/` and `factory/` (the flows build both into `console/tmp/e2e-<flow>-<id>/`).
- At least 3 GB of memory is free. The flow runs Chromium, the Dispatcher and wrapper processes next to the stack.

- **Admin settings, read-only member.** `"$V" flow -- e2e/flows/runs.spec.ts -g "only an Admin"`. Pass: the repository saves and reloads, `solidtrace projects get` shows it with `forge`, and the member's field is `readonly` and has no `Save` button.
- **Start, follow and stop a Run.** `"$V" flow -- e2e/flows/runs.spec.ts`. Pass: the member's `solidtrace start` exits 0 with `Run <id> is queued.`, and `tr[data-run=<id>]` shows `Running` and then `Stopped`. The Run page has one `execution_started` event, then `execution_ended` and `stop` events after the Stop button. The intervention names `member@solidtrace.local`, and the token is 403 on a different Agent session and 401 after the end.
- **Resume in a second Execution.** `"$V" flow -- e2e/flows/runs.spec.ts` (the last test, which needs the first one). Pass: after `solidtrace message` to the done Run, the second Execution's token is for the same Agent session, its wrapper log (`second-execution-wrapper.log` in the test's evidence folder, from the control API's `GET /executions/<id>/output`) has no `wrapper: restore:` line, and its harness' arguments (`second-harness-args.txt`) have `--resume <session id>`.
- **Integration and Issue Runs.** `"$V" flow -- e2e/flows/factory-integration.spec.ts`. Pass: one Run with the heading `Cause report`, and the Issue's `Comments` tab has a comment by the link `Factory`.
- **Demo.** `"$V" flow -- e2e/flows/demo.spec.ts`. Pass: the six Runs show their states, and `down` puts back the settings.
- **Side effects.** After a flow, read the Dispatcher's log in `console/tmp/e2e-runs-flowstack.log`. Also read the install settings that the flow put back: `"$V" run -- bin/rails runner 's = InstanceSetting.current; p [s.console_url, s.dispatcher_url]'`. The result is `["http://localhost:4511", nil]` on a fresh stack.
- **Proof.** The flow's `trace.zip` files in `<run>/evidence/flow-<time>/playwright/`, and `output.txt` with `N passed`.

## Gotchas

- The fakes are the Runner (each Execution is a local wrapper process), Claude Code (a scripted harness), the Forge and the Tracker. The Dispatcher, the wrapper, the CLI, Console, EventStore and ClickHouse are real. Do not report forge or tracker behavior as verified from these flows.
- `factoryFlow.start` sets the install's `console_url` and `dispatcher_url` with `rails runner`, and `stop` puts them back. A flow that is killed in the middle leaves `dispatcher_url` set. Fix it with `"$V" run -- bin/rails runner 'InstanceSetting.current.update!(dispatcher_url: nil)'`, or use a new stack.
- The Runs link is not in the sidebar until a Dispatcher URL is set. A page without the link on a fresh stack is correct.
- The first run of a factory flow builds Go binaries, which takes up to 4 minutes. The `beforeAll` timeout is 420 s.
- `bin/factory-demo` uses `--console-url` and `--ingest-url` with the default dev ports. Always give the stack's own ports, or the demo changes the dev stack on :4001.
