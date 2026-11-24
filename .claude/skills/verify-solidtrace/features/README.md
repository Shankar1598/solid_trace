# SolidTrace verification map

This folder is the maintained source for the verification of SolidTrace's user-facing behavior. Read this index before you drive the app. Then use the feature file that matches your change as the recipe. The words are the words of `GLOSSARY.md`: Project, Issue, Event, Log, Query, Dashboard, Run, Agent session.

## Baseline preconditions

- Start a stack from the checkout under test with `"$V" up`. `$V` is `.claude/skills/verify-solidtrace/bin/verify` in that checkout. Use the run directory that it prints.
- `"$V" doctor` has only `ok` lines.
- The seed data from `console/db/seeds.rb` is in place:
  - Organization `solid-trace-org`, Project `solid-trace-project` with key `testkey123`.
  - `admin@solidtrace.local`: Instance admin and Organization admin.
  - `member@solidtrace.local`: member.
  - The password of both users is `password123`.
- Logs come with the first `verify flow`. Its global setup sends about 20,000 dummy Logs over the last 24 hours through real ingest.
- Never drive a stack that this run did not start. That includes `bin/dev` on :4001, which can be the user's stack or another agent's stack.

## Driving conventions

- In the commands, `4511` and `4510` are the Console and ingest ports of slot 1. Read the real ports from `"$V" doctor` or from `<run>/meta`.
- Start each recipe from the baseline, unless its preconditions say otherwise.
- Use roles and accessible names first, then `data-*` handles. Never use CSS position or coordinates.
- Run flows with `"$V" flow -- <spec>`. Run single pages with `"$V" snap`. Run reads of side effects with `"$V" run -- bin/rails runner '...'`.
- Use the commands as written. Keep the quoted names and the flags as they are.
- A fixture that a flow makes with `rails runner` must be removed by that flow in `afterAll`. Proof artifacts are never removed.

## Proof and skip reporting

- Capture the user action and the state that it makes. A `verify flow` trace has both. A `snap` has only the state, so pair it with the command or click that caused the state.
- A UI proof has a trace or an ARIA snapshot, and one screenshot with the page visible.
- A CLI proof has the command, stdout, stderr and the exit code.
- A change proof has a second, read-only view of the stored value: a reload, a different page, the CLI, or a database read.
- Give the feature ID and the entry point with each artifact.
- For a path that you could not reach, report the command that you tried and the precondition that was missing.
- Do not report an entry point that you skipped as verified through a different path.

## Feature entry contract

Each feature file starts with an H1 title and one paragraph about the behavior that a user sees. Then it has exactly four H2 sections, in this order:

1. `Sub-features`: short IDs, with one line for each behavior.
2. `How to get to it (user POV)`: every entry point a user has.
3. `Driving it with verify`: starts with `Preconditions:`, then labeled bullets. Each bullet pairs a user action with an exact command and the result that you can see.
4. `Gotchas`: traps that can waste or invalidate a verification run.

Keep implementation details out of the map. Name only user paths, stable handles, the state that is needed, commands, and the proof that you can see.

## Features

- [Sign-in](./sign-in.md): password sign-in, sign-out, wrong password, the member and admin roles.
- [Logs explorer](./logs-explorer.md): Builder, Query mode, SQL mode, the histogram and the Live tail, on Logs sent through ingest.
- [Log ingest](./log-ingest.md): Logs sent to every ingest route (JSON, NDJSON, key only, Sentry envelope, OTLP/HTTP) show in the explorer and the Ingest counts, and a real OpenTelemetry Collector drives `/v1/logs` and `/v1/metrics`.
- [Issues](./issues.md): Events through ingest become Issues, and Issues are listed, filtered, resolved and unresolved.
- [Dashboards](./dashboards.md): make a Dashboard, add Charts from the explorer and from the editor, set the range, lay it out, delete it.
- [Factory Runs](./factory-runs.md): the Factory section of a Project, Runs started from the CLI, a Monitor or an Issue, the Runs and Run pages, Stop.
- [Agent sessions](./agent-sessions.md): turn on Agent sessions, CLI sign-in with the device flow, capture, and the Agent sessions pages.

Other flows in `console/e2e/flows/` cover features that are not in the map yet: Overview, Traces, Monitors, Integrations, API tokens, MCP OAuth, two-factor, passkeys, the Log transform, Settings and the palette. Run them with `verify flow` as they are, and add a map file when you verify one of them.
