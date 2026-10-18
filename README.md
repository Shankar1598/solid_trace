# SolidTrace

A self-hosted error tracking tool with lightweight architecture and strong UX.

## Why?

Self-hosting Sentry means managing multiple databases and services. SolidTrace does the same job with two services and embedded databases (SQLite, DuckDB, Pebble).

For low to medium scale application, Vertical scaling is often a better solution than scaling horizontally with multiple services. Less moving parts, less maintenance.

SolidTrace currently does not focus on offering high availability or massive scale. Periodic backups of VM/VPS should be sufficient for most use cases.

## Architecture

Two services:

- **Console** — Rails app. UI, API, user management.
  - Built with Rails, React, Inertia.js, Tailwind CSS.
  - Uses SQLite for relational data
- **EventStore** — Go service. Ingests and processes events.
  - Uses Pebble for event ingest and DuckDB for analytical queries.

## Install on a VM

On a Debian or Ubuntu machine:

```bash
git clone https://github.com/solidtrace/solid_trace.git
cd solid_trace
deploy/setup
```

`deploy/setup` asks, stage by stage, for Console's and ingest's public URLs, an SMTP server for email, and whether Log files and the Event archive go in a GCS bucket, with the `gcloud` commands that create the bucket and its HMAC key. It writes the answers to `deploy/.env` and `/etc/solid-trace/event_store.yml`, then runs `deploy/install`. Run it again to change any of them.

`deploy/install` installs Docker if it is missing, creates `/var/lib/solid-trace` for data and EventStore's config if there is none, writes Console's secret to `deploy/.env`, then builds and starts the services with `deploy/docker-compose.yml`. Run on its own, it installs with the defaults: no email, everything on local disk. Console is on port `3000`, ingest on `4000`. The services restart on failure and at boot, and their logs rotate.

To upgrade: `git pull && deploy/install`. It keeps the config and the secrets.

## Dev Setup

```bash
git clone https://github.com/solidtrace/solid_trace.git
cd solid_trace

cd console && bundle install && bun install && cd ..
cd event_store && go mod download && cd ..

cd console && bin/rails db:prepare
```

## Run

```bash
bin/dev
```

Starts Rails, Vite, EventStore, and background jobs via `Procfile.dev`.

The EventStore listens on two ports. Ingest (`4000`) is public: SDKs send events there. The query API (`query_port`, default `4100`) serves stored events to the console without authentication, so it binds to `127.0.0.1` only. Never expose it.

## MCP server

Agents such as Claude Code and Cursor can query Logs, read Issues and build Dashboards and Monitors through SolidTrace's MCP server, with an API token made in User settings. See [docs/mcp.md](docs/mcp.md).

## Project Structure

- `console/` — Rails app (UI, API, user management)
- `event_store/` — Go service (event ingestion and processing)

## Load Testing

Run event ingestion load tests using k6:

```bash
# Install k6 (macOS)
brew install k6

# Run with defaults (single_issue scenario, 100 VUs, 2 minutes)
bin/load-test

# Test specific scenario with custom VUs
bin/load-test --scenario per_event --vus 200 --duration 5m
```

Available scenarios:
- `single_issue` — All events grouped into one issue
- `per_1000` — New issue every 1000 events
- `per_100` — New issue every 100 events
- `per_event` — Every event creates a new issue

See [test/load/k6/README.md](test/load/k6/README.md) for detailed documentation.

## Performance Benchmarks

Benchmarks run on MacBook Pro M3 (11-core, 18GB RAM) with ~8KB event payloads.

| Scenario | VUs | Max RPS | P95 Latency | P99 Latency | Success Rate |
|----------|-----|---------|-------------|-------------|--------------|
| single_issue | - | - | - | - | - |
| per_1000 | - | - | - | - | - |
| per_100 | - | - | - | - | - |
| per_event | - | - | - | - | - |

*Run `bin/load-test` and update these results with your hardware's performance.*

## Future

### Tiered storage (S3 / GCS)

Log files and the Event archive can already go in an S3 or GCS bucket: see [docs/logs/object-store.md](docs/logs/object-store.md). Tiering Pebble's Events is not built yet. The goal is to keep recent events on local disk and move older ones to cheaper storage.

Pebble's support is limited. It can put almost all data on one remote store (S3, GCS, or an HDD path) and keep only recent writes local. It cannot chain tiers (SSD → HDD → S3) or tier by event age, so those would need to be built in the EventStore.

The early `storage_tiers` setting is not production-ready.
