# Running the full suites

Day to day, run the tests for what you changed. Before a release, run all three suites on the commit to be tagged, in this order:

0. **Start the dev ClickHouse with `bin/clickhouse-dev`.** The Go tests and the dev stack's EventStore both use it, so the Go suite and the Playwright flows need it. `bin/go-dev` runs it too. It's one container for every checkout and worktree: each Go test package makes and drops a database of its own on it.
1. **Start or restart the dev stack's EventStore on that commit, and wait until it answers.** Building EventStore and running the Go suite both write `event_store/vrl/lib/<os>_<arch>/libst_vrl.a`. So if the stack's EventStore is rebuilt while the Go suite builds, the link can fail.
2. **Run the Go suite and the Playwright flows in parallel.** The flows run against the dev stack and its development database, so they don't touch the Go suite's data.
   - Go, from `event_store/`, takes about 12 min:

     ```bash
     vrl/build && go test -p 1 -count=1 -timeout 60m $(go list ./... | grep -v /bench/)
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

Before tagging, record each suite's result (exit, time, counts) in the release's entry in `.scratch/STATUS.md`.
