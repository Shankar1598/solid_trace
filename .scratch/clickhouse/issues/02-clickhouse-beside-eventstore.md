# 02: ClickHouse beside EventStore: dev, tests, compose and install

**What to build:** EventStore connects to a ClickHouse server, in development, in tests and on an install, and nothing reads or writes it yet. This is the [spec](../spec.md)'s "ClickHouse beside EventStore": a pinned 26.8 LTS container, its config, EventStore's `clickhouse` config section and client, the test harness, and `/health`.

**Blocked by:** 01

**Status:** resolved

- [x] **`bin/clickhouse-dev`** starts one ClickHouse container for the dev stack and the Go tests, capped at about 2 GB, with its data under the checkout's storage. `Procfile.dev`/`bin/go-dev` use it. `docs/agents/testing.md` adds it as a prerequisite.
- [x] **ClickHouse's config lives in `deploy/clickhouse/`:**
  - it listens on loopback only;
  - the default user is limited to localhost;
  - EventStore's user takes its password from `SOLIDTRACE_CLICKHOUSE_PASSWORD`;
  - system tables need a grant (`select_from_system_db_requires_grant` and its `information_schema` twin);
  - `query_log` is on with a TTL of a few days, and the other system logs are off;
  - `max_threads` is the CPU count, with a merge pool sized from 01;
  - `max_server_memory_usage` comes from the environment.
- [x] **A storage policy:** local only without a bucket, and with one, local plus an S3 disk on the same bucket under `clickhouse/`, using the existing key env vars, GCS's no-batch-delete setting and a filesystem cache. Configured from what `event_store.yml`'s `object_store` says.
- [x] **Compose** gains a `clickhouse` service, pinned to the patch 01 chose, with its data volume and the Log file store mounted read-only for old Log files. `deploy/install` generates the password, gives ClickHouse half of RAM as it gives DuckDB now, and pulls the image.
- [x] **`event_store.yml`** gains `clickhouse` (address, database, user) with defaults. EventStore connects at start, makes its database and tables with versioned `CREATE … IF NOT EXISTS` steps recorded in a migrations table, and `/health` pings ClickHouse.
- [x] **The Go test harness** gives each test package its own database on the dev ClickHouse, named from the package and the worktree, made fresh and dropped at the end. A root test helper reaches it as the DuckDB helpers do now.
- [x] **A root test:** EventStore starts against ClickHouse, `/health` is OK, and it answers not-OK when ClickHouse is unreachable.
- [x] Targeted tests only: the config package, the new root test and `go vet`. The full suite is for the user.

## Answer

Built on `clickhouse/02-clickhouse-beside-eventstore`. Notes for later tickets (harness API, migrations, policy, ports) are in `~/.cache/solid-trace/clickhouse-notes/02-notes.md`.

**What was built**
- **`bin/clickhouse-dev`** runs one `solidtrace-clickhouse` container (26.8.34.13) on the host network, as the developer's user, with 2 GiB set by `max_server_memory_usage`. Its data is in the *main* checkout's `storage/clickhouse/`, so worktrees share it and removing one doesn't break it. It copies `deploy/clickhouse/` from the checkout that runs it, and restarts when that config changed. `bin/go-dev` and `bin/test` run it, `bin/reset-db` stops it before wiping storage, and `testing.md` lists it as step 0.
- **`deploy/clickhouse/`** (`config.d/solidtrace.xml`, `users.d/solidtrace.xml`):
  - listens on 127.0.0.1 only, native on **4200** and HTTP on **4201**, not 9000, which the dev MinIO takes. MySQL, Postgres and interserver ports are off.
  - `default` has no password and is limited to localhost. EventStore's user is `solidtrace`, with its password `from_env` `SOLIDTRACE_CLICKHOUSE_PASSWORD` and `access_management` on, for the SQL users of 07.
  - both system-table grant settings are on;
  - `query_log` keeps 3 days. Every other configured system log is removed.
  - `max_server_memory_usage` comes `from_env` `SOLIDTRACE_CLICKHOUSE_MEMORY_BYTES`;
  - `user_files_path` is `/logs_parquet/`, where the Log file store is mounted read-only;
  - logs go to the console only, at warning level.
- **Storage policy `solidtrace`.**
  - Its local volume (priority 1) is in the static config.
  - With an `object_store`, EventStore writes `solidtrace-storage.xml` into `clickhouse.config_dir`, which is ClickHouse's `conf.d`. It adds:
    - an S3 disk at `<endpoint>/<bucket>/<prefix>clickhouse/` (AWS: virtual-hosted), with its key `from_env` from the `SOLIDTRACE_OBJECT_STORE_*` variables and `support_batch_delete=false` on GCS;
    - a 10 GiB cache disk;
    - a `bucket` volume at priority 2.
  - ClickHouse reloads the file by itself, and EventStore waits up to 30 s for the volume.
  - Checked by hand against dev MinIO: a part moved to the bucket volume and read back after the cache was dropped.
- **Compose** has a `clickhouse` service on the pinned patch.
  - It mounts the config, `data/clickhouse`, the Log file store read-only and EventStore's `conf.d`. It sets `CLICKHOUSE_DO_NOT_CHOWN`, because the image's chown fails on the read-only mount.
  - It has a healthcheck, and `event_store` waits for it to be healthy.
  - `deploy/install` writes the password and half of RAM to `.env`, and makes `data/clickhouse` owned by uid 101. The image is pulled with the others.
  - Checked by hand: the service starts healthy, the password is enforced and the memory comes from the environment.
- **EventStore** has an `event_store.yml` `clickhouse` block: address, database, user and `config_dir`, with defaults matching `bin/clickhouse-dev`.
  - At start, `storage.OpenClickHouse` makes the database, writes and waits for the policy, and runs versioned steps recorded in `schema_migrations`. The list of steps is empty; 03 adds the first.
  - `/health` has a `clickhouse` check.
- **Harness:** `pkg/clickhousetest` (`Main`, `Config`, `Empty`). The root package's `TestMain` uses it. `TestEnv` has `ClickHouse` and `ClickHouseConfig`, and empties the database for each test.
- **Tests:**
  - config: defaults, the deploy file, keys from the file and bad blocks;
  - storage: the policy config for none, MinIO, GCS and AWS;
  - root `clickhouse_test.go`: start and healthy; start again on the same database; 503 while a link to ClickHouse is cut, and OK once it's restored.
  - `go vet ./...` is clean.

**Differs from the ticket or spec**
- **`config_dir`** is a fourth key in the `clickhouse` block. "Configured from `object_store`" needed a way to reach ClickHouse's config. Its default `../storage/clickhouse/conf.d` resolves to `/storage/clickhouse/conf.d` in the image, so installs whose `event_store.yml` predates the block work too.
- **Removing** an `object_store` needs a ClickHouse restart. ClickHouse can't drop a volume while it runs, so it logs an error and keeps the volume.
- **`max_threads` and the merge pool** keep 26.8's defaults, as the spec and 01 say. `max_threads` defaults to the CPU count, so the ticket's line holds.
- **`s3queue_log`** is built in, not configured, so it still exists. It stays empty.
- **Dependencies:** clickhouse-go v2.49.0 raised some minimum versions: klauspost/compress 1.19.1, otel 1.46, golang.org/x/*. Only targeted tests ran on them.
- **Memory:** DuckDB still gets half of RAM too, so an install of this branch before 10 would give away all of it.

**Left for later**
- The insert settings from 01 (`async_insert=0`, the dedup token, `max_insert_threads=1`) aren't on the connection. 03 sets them per insert.
- The migration runner has only been run with temporary steps, by hand. 03's first step is its first real use.
- Every root test now needs `bin/clickhouse-dev`.
