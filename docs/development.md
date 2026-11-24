# Developing SolidTrace

## Architecture

- **Console** — Rails app: UI, API, user management, the MCP server. Built with Rails, React, Inertia.js and Tailwind CSS, with SQLite for relational data.
- **EventStore** — Go service: ingests and processes Events and Logs. Pebble holds what arrives until it's processed, and EventStore runs each Project's Log transform on it.
- **ClickHouse** — keeps Logs and Events, and runs the queries on them ([ADR 0016](adr/0016-logs-and-events-in-clickhouse.md)).

In an install, Caddy terminates TLS in front of them and Vector ships SolidTrace's own logs into it, all from one Docker Compose file.

## Setup

```bash
git clone https://github.com/Shankar1598/solid_trace.git
cd solid_trace

cd console && bundle install && bun install && cd ..
cd event_store && go mod download && vrl/build && cd ..

cd console && bin/rails db:prepare
```

Ruby, Go, Rust and Bun come through `mise`; ClickHouse and MinIO run in Docker.

## Run

```bash
bin/dev
```

Starts the dev ClickHouse and MinIO (`bin/clickhouse-dev`, from `docker-compose.dev.yml`), then Rails, Vite, EventStore and background jobs via `Procfile.dev`. The Go tests use the same ClickHouse, so run `bin/clickhouse-dev` before `go test` on its own. [agents/testing.md](agents/testing.md) describes the test suites.

EventStore links the Rust `vrl` crate, built as a static library, for the Log transform. `event_store/vrl/build` builds the library; run it before `go build` or `go test` in `event_store/`, and again after changing `event_store/vrl/rust/`. The first build takes about four minutes. EventStore's Docker image builds the library in a Rust stage of its own, with the crate's `dist` profile (fat LTO), so building the image needs only Docker.

EventStore listens on two ports. Ingest (`4000`) is public: SDKs and shippers send there. The query API (`query_port`, default `4100`) serves stored Logs and Events to Console without authentication, so it binds to `127.0.0.1` only. Never expose it.

`bin/security` scans every component for known vulnerabilities: gems, Rails code, JS packages, Go modules, the VRL crate, secrets in git's history, and the images the install runs.

### OpenTelemetry Collector

To try OTLP Log ingest (`POST /v1/logs`) with a real OpenTelemetry Collector, run `bin/otel-dev <count> [ingest-url] [key]`. The script starts the opt-in `otel` profile of `docker-compose.dev.yml`. The profile has the Collector (`otel/opentelemetry-collector`, set up in `local/otel/collector.yaml`) and two `telemetrygen` containers, one for Logs and one for Metrics. The script sends `<count>` Logs through the Collector and waits until the Collector has sent them. Then it stops the three containers.

The Collector sends each Log twice, as protobuf with gzip and as JSON. So `<count>` Logs make `2 x <count>` Logs with service `telemetrygen` and environment `dev`. The Collector carries Metrics too (`POST /v1/metrics`). The script also sends `<count>` Metric data points. Half of the metric data points are a sum and half are a gauge. The Collector sends each data point twice, and EventStore stores each one as a Log, with the metric name (`otel_dev.sum` or `otel_dev.gauge`) as the message. Without the ingest URL and the key, the Logs go to the dev stack (`http://127.0.0.1:4000`) with the seed Project's key, `testkey123`. For a verify stack, pass its ingest URL, for example `bin/otel-dev 50 http://127.0.0.1:4510 testkey123`.

The profile is opt-in because the dev box is short of memory. `bin/dev` does not start it. The Collector listens on `127.0.0.1:14318` (OTLP/HTTP) and `127.0.0.1:14888` (its own metrics). It does not use 4317 or 4318, so it cannot clash with a Collector that you run yourself. Run one `bin/otel-dev` at a time.

## Project structure

- `console/` — Rails app (UI, API, MCP server, user management)
- `event_store/` — Go service (ingest, processing, the Log transform)
- `cli/` — `solidtrace`, the agent CLI: a Go module of its own with no cgo. `solidtrace login` signs in through SolidTrace OAuth (the browser, or a code with `--device`) and keeps the sign-in in `credentials.json` in `SOLIDTRACE_CONFIG_DIR`, else in `solidtrace/` in the user's config directory, readable only by that user
- `deploy/` — the install: Compose file, `setup-gcp`, `install`, and the Caddy, ClickHouse and Vector config
- `docs/` — user docs, decisions in `adr/`, release notes in `changelogs/`
- `bin/` — dev and release scripts
