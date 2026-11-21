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

     The usual `go test ./...` runs the Nomad Runner's tests against a fake Nomad HTTP API, but does not build its stack tests (`factory/nomadrunner`, build tag `factorystack`). The stack tests are the only tests of the Runner against a real Nomad, with its workload identity check. Run them from the repo root with `factory/local/test-runner`: it starts a local factory stack of its own (compose project `factory-runner-test`), runs the tests, and stops the stack. It needs Docker, and takes about 2 minutes, with the first build of the stack's images more. On a small box, run it when no other stack or suite runs:

     ```bash
     factory/local/test-runner
     ```

     The agent VM image recipe (`factory/image/agent-vm.sh`) has a test of its own: it runs the recipe in a clean Debian container and checks what it made, the sudoers rule and the refusals among it. It needs Docker and the internet, and takes about 2 minutes (more for the first download of Claude Code). Run it when the recipe changes, and before a release, with no other stack running:

     ```bash
     factory/image/test
     ```

     The Dispatcher's image (`factory/Dockerfile`) has a test of its own: it builds the image as the release job does, runs it with the host's network, and checks its version, its user, its private listener (it answers on the configured address only), its operator commands, its database volume and the Squid allowlist that it writes from the reference configuration's default list. The factory's release files (the wrapper, the agent CLI and the image recipe, as archives with their sums) have a test too. Both need Docker or Go, the internet for the image, and no other stack. The image test takes about 3 minutes (more for the first build). Run them when the image, the recipe or the release job changes, and before a release. The release job itself is tried with a test release (below):

     ```bash
     factory/test-dispatcher-image
     factory/image/test-release-files
     ```

     A test release proves the release job's factory parts: run the workflow "Release images and the agent CLI" by hand (`workflow_dispatch`) for the tag of a draft or pre-release, with "images" set. Its `build` job pushes the Dispatcher's image for amd64 and arm64 (`solid-trace-dispatcher`), and its `factory` job adds `factory-bin-linux-amd64.tar.gz`, `factory-image-recipe.tar.gz` and `factory-SHA256SUMS` to the release.

     The Harness contract checks (Seam 4, `factory/internal/harnesscontract`) are skipped there. They run the real Claude Code with Haiku, under your own Claude login, and save the streams that they record in `fakeharness/streams/`. Run them only when the pinned Claude Code version (`fakeharness.Version`) changes: change that constant first, then run the checks from `factory/` and commit the new streams. They take about 2 minutes and cost about 2 US cents:

     ```bash
     FACTORY_HARNESS_CONTRACT=1 go test -count=1 -v -timeout 15m ./internal/harnesscontract/
     ```

   - Playwright, from `console/`, takes about 10 min:
     - Run `bin/rails db:migrate` first, so the dev database has any new migration.
     - Then run `bun run e2e --reporter=line`.
     - `e2e/flows/runs.spec.ts` (Seam 5) builds the factory's flow stack (`factory/internal/flowstack`): the real Dispatcher and wrapper against the dev stack's Console and EventStore, with the Seam 3 fakes for the Runner, the Harness, the Forge and the Tracker. Its wrappers need a kill boundary, as the factory tests do: run the flows from a shell whose cgroup your user can write to (a delegated scope, as above). The dev stack's EventStore must be built from the same commit, as the factory calls its agent API.
3. **Run the Rails suite after the Go suite has finished.** EventStore's root tests use Console's test database (they run `bin/rails db:reset` in `console/`), so the two suites can't run at once.
   - From `console/`, takes about 1 min:

     ```bash
     RAILS_ENV=test bin/rails db:test:prepare && bin/rails test
     ```

If Vite stops with `ENOSPC` on a `watch`, the inotify watch limit is used up, not the disk. Restart Vite with `CHOKIDAR_USEPOLLING=1` set, and it polls instead of watching.

Each release has its changelog, `docs/changelogs/<tag>.md`: the GitHub release's notes, then a "Release record" section saying what was tagged, the migrations and config changes, and the tests run. Before tagging, record each suite's result (exit, time, counts) there.
