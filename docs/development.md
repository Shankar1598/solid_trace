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

## Project structure

- `console/` — Rails app (UI, API, MCP server, user management)
- `event_store/` — Go service (ingest, processing, the Log transform)
- `deploy/` — the install: Compose file, `setup-gcp`, `install`, and the Caddy, ClickHouse and Vector config
- `docs/` — user docs, decisions in `adr/`, release notes in `changelogs/`
- `bin/` — dev and release scripts
