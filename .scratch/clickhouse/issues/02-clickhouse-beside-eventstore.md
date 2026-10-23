# 02: ClickHouse beside EventStore: dev, tests, compose and install

**What to build:** EventStore connects to a ClickHouse server, in development, in tests and on an install, and nothing reads or writes it yet. This is the [spec](../spec.md)'s "ClickHouse beside EventStore": a pinned 26.8 LTS container, its config, EventStore's `clickhouse` config section and client, the test harness, and `/health`.

**Blocked by:** 01

**Status:** ready-for-agent

- [ ] **`bin/clickhouse-dev`** starts one ClickHouse container for the dev stack and the Go tests, capped at about 2 GB, with its data under the checkout's storage. `Procfile.dev`/`bin/go-dev` use it. `docs/agents/testing.md` adds it as a prerequisite.
- [ ] **ClickHouse's config lives in `deploy/clickhouse/`:**
  - it listens on loopback only;
  - the default user is limited to localhost;
  - EventStore's user takes its password from `SOLIDTRACE_CLICKHOUSE_PASSWORD`;
  - system tables need a grant (`select_from_system_db_requires_grant` and its `information_schema` twin);
  - `query_log` is on with a TTL of a few days, and the other system logs are off;
  - `max_threads` is the CPU count, with a merge pool sized from 01;
  - `max_server_memory_usage` comes from the environment.
- [ ] **A storage policy:** local only without a bucket, and with one, local plus an S3 disk on the same bucket under `clickhouse/`, using the existing key env vars, GCS's no-batch-delete setting and a filesystem cache. Configured from what `event_store.yml`'s `object_store` says.
- [ ] **Compose** gains a `clickhouse` service, pinned to the patch 01 chose, with its data volume and the Log file store mounted read-only for old Log files. `deploy/install` generates the password, gives ClickHouse half of RAM as it gives DuckDB now, and pulls the image.
- [ ] **`event_store.yml`** gains `clickhouse` (address, database, user) with defaults. EventStore connects at start, makes its database and tables with versioned `CREATE … IF NOT EXISTS` steps recorded in a migrations table, and `/health` pings ClickHouse.
- [ ] **The Go test harness** gives each test package its own database on the dev ClickHouse, named from the package and the worktree, made fresh and dropped at the end. A root test helper reaches it as the DuckDB helpers do now.
- [ ] **A root test:** EventStore starts against ClickHouse, `/health` is OK, and it answers not-OK when ClickHouse is unreachable.
- [ ] Targeted tests only: the config package, the new root test and `go vet`. The full suite is for the user.
