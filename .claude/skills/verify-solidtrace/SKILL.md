---
name: verify-solidtrace
description: Launch an isolated SolidTrace stack (Console web UI, EventStore ingest and query, jobs, Vite) from the current checkout and drive it as a user does, with Playwright flows, a sign-in-and-snapshot helper, curl for ingest, and the `solidtrace` CLI with the factory's flow stack (Dispatcher and wrapper). Use it to prove that a change works in the real app (not only in unit tests), before you call UI, ingest, MCP, Agent session or factory Run work done.
---

# Verify SolidTrace

SolidTrace has these surfaces. Each one runs from one stack:

- **Console** (primary): the Rails web UI. Pages for Logs, Issues, Dashboards, Monitors, Traces, Overview, Settings, Agent sessions and Runs. Console also serves MCP at `/mcp` and the API at `/api/...`.
- **EventStore ingest**: the Go HTTP API that Sentry SDKs, log shippers and the CLI send to.
- **`solidtrace` CLI** (`cli/`): sign-in, Agent session capture, and factory commands (`start`, `runs`, `stop`, `projects`, `connections`).
- **Factory** (`factory/`): the Dispatcher and the wrapper. You drive them through `factory/internal/flowstack`. This is the real Dispatcher and wrapper, with fakes only for the external systems: the Runner, Claude Code, the Forge and the Tracker.

All commands below use `bin/verify` in this skill folder. Run it from any directory in the checkout under test:

```bash
V="$(git rev-parse --show-toplevel)/.claude/skills/verify-solidtrace/bin/verify"
```

The helper drives the checkout that it is in. To verify a worktree, use the copy of the helper in that worktree.

## Isolation: read this first

The dev box can have other stacks in operation, for example `bin/dev` on :4001 or another agent's stack. **Never drive, stop or reseed a stack that you did not start.** Never run `bin/dev`, `bin/go-dev` or `foreman start -f Procfile.dev` to verify. Those use the fixed ports 4001/4000/4100/3036 and the checkout's own `storage/`.

`verify up` puts each stack in a slot from 1 to 4. Each slot has its own ports and its own data:

| slot N | Console | Vite | ingest | query | ClickHouse database |
| --- | --- | --- | --- | --- | --- |
| 1 | 4511 | 3516 | 4510 | 4512 | `solidtrace_verify_s1` |
| N | 4501+10N | 3506+10N | 4500+10N | 4502+10N | `solidtrace_verify_sN` |

The SQLite databases (primary, queue, cache, message_queue) and Pebble are in the run directory. Two stacks can run at the same time, also from the same checkout. The one shared server is the dev ClickHouse (127.0.0.1:4200/4201). That is safe because each slot has its own database and its own SQL users (`<database>_sql_<project id>`).

These things are shared and are **not** isolated:

- Active Storage files (`storage/active_storage` of the checkout).
- `console/log/development.log`.
- Playwright's `console/tmp/playwright-auth/admin.json`. Run one `verify flow` at a time for each checkout.

Each stack needs about 2.5 GB of RAM, and Chromium needs about 1 GB more. `verify up` refuses to start when less than 3000 MB is free.

## Launch

```bash
"$V" up            # or: "$V" up --slot 2
```

The command does these steps:

1. It checks the dev ClickHouse, `console/node_modules` and free memory.
2. It builds EventStore from this checkout. A clean `event_store/` tree is cached in `~/.cache/solid-trace/verify/bin/`. A changed tree is built again for each run. A cold build takes some minutes.
3. It runs `db:prepare` on fresh databases. This makes the schema and the seed data from `console/db/seeds.rb`: Organization `solid-trace-org`, Project `solid-trace-project` with key `testkey123`, `admin@solidtrace.local` (an Instance admin and an Organization admin) and `member@solidtrace.local`, both with password `password123`.
4. It sets the install's Console URL and ingest URL to this stack's ports.
5. It starts web, Vite, jobs and EventStore in a new session.

**Ready** is the last line on stdout: the run directory, for example `~/.cache/solid-trace/verify/runs/20261122T085753Z-s1`. Before it prints that line, the command waits until all of these are true: Console `/health` says `healthy` (database and EventStore), EventStore `/health` says `healthy` (ClickHouse and Pebble), the query port answers, and Vite serves `/vite-dev/@vite/client`. If the stack does not become usable within 240 s, the command stops, shows the end of `stack/stack.log`, and tells you to run `down`.

A fresh stack has no Logs. The first `verify flow` sends about 20,000 dummy Logs through real ingest (`e2e/support/global-setup.ts`, `rails dev:logs`). To fill the stack without a flow, run `"$V" run -- bin/rails dev:logs PROJECT=solid-trace-project COUNT=20000 HOURS=24 SEED=42`. For Issues, Integrations, a Dashboard and Monitors, run `"$V" run -- bin/rails dev:data LOGS=0`. It sends Events through ingest as an SDK does.

Teardown is `"$V" down` (see Cleanup).

## Doctor

```bash
"$V" doctor        # read-only; exit 0 only when every line is ok
```

Doctor does these checks:

- The stack's session is alive.
- Each of the four ports has a listener in **this** stack's session. A port that another stack holds is a FAIL.
- Console and EventStore are healthy, Vite serves, and the login page renders.
- The ClickHouse database exists.
- The seed admin and Project exist.

Doctor also gives a warning when HEAD moved after `up`. Rails and Vite reload code, but EventStore does not. After an `event_store/` change, do `down` and then `up`.

Run doctor first when something looks wrong. `verify flow` runs doctor before each flow and refuses to start on a FAIL. The reason: Playwright's `webServer` would start `Procfile.dev` on the default ports if the stack were down.

## Drive

- **Playwright flows (the main harness).** The flows are in `console/e2e/flows/*.spec.ts`. They use stable handles: roles and accessible names, `data-run`, `data-section`, `data-event`, `data-action`, `data-chart-card` and `data-session`.

  ```bash
  "$V" flow -- e2e/flows/runs.spec.ts                 # paths relative to console/
  "$V" flow -- e2e/flows/logs-v1-query.spec.ts -g "Ctrl+Enter"
  "$V" flow --video -- e2e/flows/runs.spec.ts         # also a .webm of each browser context
  ```

  The arguments after `--` go to `playwright test --project=flows`. Global setup seeds the database, signs the admin in, and tops up the Logs. To prove a new behavior, add or extend a flow in `console/e2e/flows/`, in the same style as the other flows. Then run it with `verify flow`.

  `--video` records each browser context, as `E2E_VIDEO=on` in `e2e/support/video.ts` sets it. Headless Chromium shows no mouse pointer, so the video draws one. The pointer moves to each action and shows the click there, with the action's name at the top right and the test's title at the bottom left. A flow that opens a second user's browser must make the context with `newContext(browser, ...)` from `e2e/support/video.ts`. A context from `browser.newContext()` is not recorded. Use `--video` when a person will watch the proof, because each video is a few MB.
- **One page, signed in.** This makes a quick visual and ARIA proof without a spec:

  ```bash
  "$V" snap --path /solid-trace-org/runs --name runs-page --as member --wait-text Runs
  ```

  It prints one JSON line: the URL, the HTTP status, the page errors and `missing_text`. It writes `<name>.png` and `<name>.aria.txt` into the evidence folder, also when the check fails. The exit code is 1 when the HTTP status is 400 or more, when the page had an error, or when the `--wait-text` did not show within 30 s.
- **Commands in the stack's environment.** Use this for `rails runner` reads that check side effects, for `dev:data`, and to build the CLI:

  ```bash
  "$V" run -- bin/rails runner 'p Issue.count'
  "$V" run -- sh -c 'curl -s "$INGEST_URL/health"'
  ```

  The command runs in `console/`. It gets `DATABASE_URL` and the other database URLs, `INGEST_URL`, `E2E_BASE_URL`, `EVENT_STORE_URL` and `VITE_RUBY_PORT`. **Never** put `source <run>/env` in a shell where you then run `bin/rails test`. `DATABASE_URL` applies to every Rails environment, so the test suite would drop the stack's database.
- **Ingest, as a client does.** Example: an Event to the seed Project, with the Project's id from `"$V" run -- bin/rails runner 'print Project.find_by!(slug: "solid-trace-project").id'`:

  ```bash
  curl -s -X POST "http://localhost:4510/api/<project id>/store?sentry_key=testkey123" \
    -H 'Content-Type: application/json' -d '{"event_id":"…32 hex…","message":"verify","level":"error","platform":"ruby"}'
  ```
- **The factory.** `e2e/support/factoryFlow.ts` builds the `solidtrace` CLI and `factory/internal/flowstack` into `console/tmp/e2e-<name>-<id>/`. It starts the flow stack against this stack's Console and ingest, signs people in with `solidtrace login --device`, and puts back the install settings after. Drive the factory through its flows: `runs.spec.ts`, `factory-integration.spec.ts`, `agent-sessions.spec.ts` and `demo.spec.ts`. To look at Runs by hand, run `bin/factory-demo up --console-url http://localhost:<console port> --ingest-url http://localhost:<ingest port>` from the checkout root, with the stack's environment, as a long-running background command. Then `snap` the pages that it prints. Ctrl-C (SIGINT) or `bin/factory-demo down` puts back the settings.

The feature map in [`features/README.md`](features/README.md) gives the recipe for each feature. Read it before you drive.

## Evidence

The proof goes in `<run>/evidence/`. The run directory is in `~/.cache/solid-trace/verify/runs/`, outside the checkout and outside `/tmp` (the dev box clears `/tmp` when it restarts). `down` keeps it. `prune` deletes it 7 days after the run, so copy what must stay longer.

- `flow-<HHMMSS>/`: `output.txt` (the reporter output), `exit-code`, and `playwright/<test>/trace.zip` for every test. Traces are on for pass and fail, and each trace has the actions, DOM snapshots, network and console. Look at one with `bunx playwright show-trace <trace.zip>` from `console/`. With `--video`, the same test folder has one `.webm` for each browser context of the test, for example the admin's and the member's.
- `<name>.png` and `<name>.aria.txt` from `snap`.
- `doctor-before-flow.txt`, and `stack.log` (web, Vite, jobs and EventStore output), which `down` copies here.

Proof standards:

- Use the real user path: the pages, the CLI and the ingest API. Do not use `rails runner` setters or test-only endpoints to make the result. `rails runner` is correct for fixtures that a flow puts back, and for read-only checks of side effects.
- Capture the action and the state that it makes, not only the last screen. A Playwright trace has both.
- Check the side effects too: rows in the stack's SQLite databases (`"$V" run -- bin/rails runner ...`), rows in ClickHouse (`curl -s 'http://127.0.0.1:4201/?user=solidtrace' --data-binary 'SELECT count() FROM solidtrace_verify_s1.logs'`), files the CLI wrote, and the Dispatcher's log (`console/tmp/e2e-<flow>-flowstack.log`).
- Use mocks only at a boundary that production already isolates. The factory's fakes for the Runner, Claude Code, the Forge and the Tracker are such boundaries. Console, EventStore, ClickHouse, the Dispatcher, the wrapper and the CLI are always real.
- When you report, give the feature IDs from the map and the entry points that you drove. A path that you did not drive is not verified.

## Cleanup

```bash
"$V" down          # this checkout's newest running stack; or: "$V" down --run <run dir>
"$V" list          # each run: up/down, slot, Console port, checkout
```

`down` does these steps:

- It stops the processes in the stack's own session, with TERM first and KILL after 15 s. It never kills by process name.
- It drops the slot's ClickHouse database, its SQL users and its profile.
- It copies `stack.log` into `evidence/` and deletes `stack/` (SQLite, Pebble, config).
- It frees the slot.

The evidence folder stays. Run `down` after every failed `up` or flow too, so that no processes and ports are left behind.

`down` does not delete the evidence or the cached EventStore build (about 110 MB for each `event_store/` tree). `prune` deletes them later:

```bash
"$V" prune             # stopped runs older than 7 days, and all builds but the 2 newest
"$V" prune --days 0    # every stopped run, evidence too: only after the proof is reported
```

`up` runs `prune` before it starts, and refuses to start when less than 3 GB of disk is free. `prune` never touches a run that is up, or the build that it uses. A flow's own tmp folder (`console/tmp/e2e-*`) is deleted by the flow. The Dispatcher's log stays there.

## Gotchas

- Chromium on this box needs the libraries in `~/.cache/solid-trace/pw/libs`. Both `playwright.config.ts` and `snap.mjs` set `LD_LIBRARY_PATH` for them.
- After you change a `.tsx` file, `touch` it before you run a flow. Vite can serve stale code after an edit. The stack runs Vite with `CHOKIDAR_USEPOLLING=1`, because worktrees use up the inotify limit.
- Sign-in has a rate limit of 10 for each address in 3 minutes. If a long series of `snap` calls and flows gets the login page back, wait, or use one flow.
- A new worktree needs `mise trust` in the checkout and `bun install` in `console/` before `up`. Without `node_modules`, Vite's auto-build calls itself in a loop.
- `~/.netrc` must have mode 600. If it does not, every Console call to EventStore fails with "EventStore is unavailable".
- A change in `event_store/vrl/rust/` needs `event_store/vrl/build`. The helper builds VRL only when the library is missing.
- The dev ClickHouse is the one server that is shared. If it is down, start it from the main checkout with `bin/clickhouse-dev`. Never stop it: other stacks and the Go tests use it.
