# 03: Vector ships SolidTrace's logs

**Status:** ready-for-human
**Spec:** [spec.md](../spec.md), decisions 1, 2 and 8
**Blocked by:** [01](01-key-only-log-ingest.md), [02](02-seed-the-meta-organization.md)

**What to build:** a Vector service on VM installs that sends every SolidTrace container's output to the Meta Organization's Project.

- [x] A `vector` service in `deploy/docker-compose.yml`: a pinned `timberio/vector` image at least two weeks old, the host network, `/var/run/docker.sock` read-only, its config from `deploy/vector/`, always on.
- [x] `deploy/vector/vector.yaml`:
  - a `docker_logs` source limited to this Compose project's containers, without Vector's own;
  - a transform that parses JSON lines and logfmt lines, and the Level prefix of Rails' lines, into time, level and message, sets `service` to the Compose service name and `container` to the container's name;
  - an `http` sink to `http://127.0.0.1:4000/api/logs`, NDJSON or a JSON array, with the key from `deploy/.env` as a bearer token, retrying on 503.
- [x] EventStore: no colour when its output is not a terminal.
- [x] Console: production log lines start with their Level (`INFO [request-id] message`).
- [x] `deploy/install` and the README mention the Meta Organization and where SolidTrace's own Logs are.
- [x] Checked: `vector validate` on the config; `docker compose config`; sample lines of each Service through the transform (`vector tap` or a unit test in the config) come out with the right fields; Logs from a running stack reach the Project.

## Comments

- `vector` service: `timberio/vector:0.58.0-alpine` (2026-08-26; 0.59.0 is under two weeks old), host network, socket `:ro`, `./vector:/etc/vector:ro`, `restart: unless-stopped`, rotated logs. `db_prepare` depends on it, so Vector is watching before db_prepare writes. Vector 0.58 doesn't interpolate `${VAR}` unless `VECTOR_DANGEROUSLY_ALLOW_ENV_VAR_INTERPOLATION=true`, which Compose sets; without it the key was sent literally and got a 401.
- `vector.yaml`: `docker_logs` limited to label `com.docker.compose.project=solid-trace`, without `solid-trace-vector-` containers. A `dedupe` on Docker's `container_id`/`timestamp`/`message` comes first: when a container stops, Vector reads its last line again (seen end to end). The remap: JSON object lines are kept as sent (EventStore reads `time`/`ts`, `level`, `msg`); `LEVEL message` lines (Rails) give Level and message, the tags staying in the message; logfmt lines with `level` and `message`/`msg` (EventStore) are kept as parsed; anything else is the message. A line without its own time gets Docker's. `service` is the Compose service, `container` the container's name; nothing else of Docker's is sent. The sink posts NDJSON to `/api/logs` with the bearer key; Vector retries 5xx (503 retry seen) and drops 4xx.
- EventStore: colour only when stderr is a terminal (`pkg/logger.New`), and its `timestamp=` is now RFC 3339, since `2026-10-19 10:00:00 UTC` split into three logfmt keys. Tests in `pkg/logger/logger_test.go`.
- Console: `LevelLogFormatter.logger(STDOUT)` in production gives `INFO [request-id] message`; `test/lib/level_log_formatter_test.rb`.
- Checked: `vector test` (5 tests, one line per service; they fail if the transform's service mapping is broken) and `vector validate` with the 0.58.0 image; `docker compose config`. End to end on the dev box, without the deploy stack (its ports clash with the dev stack): a production `db:prepare meta_organization:seed` with a key, this branch's EventStore on ports 4010/4110, the Vector container with this config (port changed to 4010), and short-lived containers labelled as each Compose service writing a sample line. Each line reached the Meta Project once, with the right Service, Level, message, time and `container`; a Vector-named container's line and another Compose project's were not shipped.
- Not checked: `deploy/install` on a real VM with the whole stack. Left for a human.
