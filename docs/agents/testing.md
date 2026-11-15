# Running the full suites

Day to day, run the tests for what you changed. Before a release, run all three suites on the commit to be tagged, in this order:

0. **Start the dev ClickHouse and MinIO with `bin/clickhouse-dev`** (`docker-compose.dev.yml`, project `solidtrace-dev`). The Go tests and the dev stack's EventStore both use them, so the Go suite and the Playwright flows need them. `bin/dev` and `bin/go-dev` run it too. It's one ClickHouse for every checkout and worktree: each Go test package makes and drops a database of its own on it. Their data is in the main checkout's `storage/`.
1. **Start or restart the dev stack's EventStore on that commit, and wait until it answers.** Building EventStore and running the Go suite both write `event_store/vrl/lib/<os>_<arch>/libst_vrl.a`. So if the stack's EventStore is rebuilt while the Go suite builds, the link can fail.
2. **Run the Go suite and the Playwright flows in parallel.** The flows run against the dev stack and its development database, so they don't touch the Go suite's data.
   - Go, from `event_store/`, takes about 12 min:

     ```bash
     vrl/build && go test -p 1 -count=1 -timeout 60m $(go list ./... | grep -v /bench/)
     ```

   - The solidtrace CLI, from `cli/`, takes less than a minute. It needs no ClickHouse and no database, so it can run at any time. `cli/build` then builds the binary with no cgo for Linux and macOS on amd64 and arm64:

     ```bash
     go vet ./... && go test -race -count=1 ./... && ./build
     ```

   - The factory module (the Dispatcher and the wrapper), from `factory/`, takes less than a minute. Its tests are Seam 3: the real Dispatcher with SQLite in a temp dir, a SolidTrace recorder and the real solidtrace CLI, with fakes at the edges in `factory/internal/` (a Runner of local processes with its own workload identity keys, a scripted Harness that plays the recorded streams in `fakeharness/streams/`, and a Forge of bare git repositories with a part of Bitbucket Cloud's REST API, which needs `git`). The tests in `factory/wrapper/` start the test binary as the wrapper, the Harness and the agent's solidtrace (symbolic links in a temp dir), and need `sh`. The wrapper starts each harness in a child cgroup (the kill boundary), so those tests need a cgroup v2 (Linux 5.14 or later) that they can write to: root, or a delegated cgroup. An SSH shell is in a session scope that root owns, so the command below runs the suite in a delegated scope of your user. Where there is no kill boundary, those tests skip with the reason, and `go test` still prints `ok`. So the release run sets `KILLBOUNDARY_REQUIRED=1`, which makes each of those skips a failure. `factory/local/smoke` runs the cgroup cases of `internal/killboundary` as root under Nomad's raw_exec. It needs no ClickHouse and no database, so it can run at any time. It uses cgo for SQLite:

     ```bash
     systemd-run --user --scope -p Delegate=yes sh -c 'go vet ./... && KILLBOUNDARY_REQUIRED=1 go test -race -count=1 ./...'
     ```

     The Harness contract checks (Seam 4, `factory/internal/harnesscontract`) are skipped there. They run the real Claude Code with Haiku, under your own Claude login, and save the streams that they record in `fakeharness/streams/`. Run them only when the pinned Claude Code version (`fakeharness.Version`) changes: change that constant first, then run the checks from `factory/` and commit the new streams. They take about 2 minutes and cost about 2 US cents:

     ```bash
     FACTORY_HARNESS_CONTRACT=1 go test -count=1 -v -timeout 15m ./internal/harnesscontract/
     ```

   - Playwright, from `console/`, takes about 10 min:
     - Run `bin/rails db:migrate` first, so the dev database has any new migration.
     - Then run `bun run e2e --reporter=line`.
3. **Run the Rails suite after the Go suite has finished.** EventStore's root tests use Console's test database (they run `bin/rails db:reset` in `console/`), so the two suites can't run at once.
   - From `console/`, takes about 1 min:

     ```bash
     RAILS_ENV=test bin/rails db:test:prepare && bin/rails test
     ```

If Vite stops with `ENOSPC` on a `watch`, the inotify watch limit is used up, not the disk. Restart Vite with `CHOKIDAR_USEPOLLING=1` set, and it polls instead of watching.

Each release has its changelog, `docs/changelogs/<tag>.md`: the GitHub release's notes, then a "Release record" section saying what was tagged, the migrations and config changes, and the tests run. Before tagging, record each suite's result (exit, time, counts) there.
