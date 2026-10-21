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

On a Debian or Ubuntu machine, from a [release](https://github.com/Shankar1598/solid_trace/releases):

```bash
git clone --branch v0.1.0 https://github.com/Shankar1598/solid_trace.git
cd solid_trace
deploy/setup-gcp
```

`deploy/setup-gcp` asks, stage by stage, whether to use HTTPS and for Console's and ingest's public URLs, an SMTP server for email, and whether Log files and the Event archive go in a GCS bucket, with the `gcloud` commands that create the bucket and its HMAC key. It writes the answers to `deploy/.env` and `deploy/config/event_store.yml`, then runs `deploy/install`. Run it again to change any of them.

`deploy/install` installs Docker if it is missing, creates `deploy/data/` for data and EventStore's config (from the template `deploy/event_store.yml`) if there is none, writes Console's secret to `deploy/.env`, then starts the services with `deploy/docker-compose.yml`. It pulls a release's images from GitHub's registry (`ghcr.io/shankar1598/solid-trace-console` and `-event-store`, for amd64 and arm64, so x86 VMs and Arm ones such as GCP's T2A both work), which each published release builds (`.github/workflows/release.yml`): the release the checkout is at, or on any other commit, `main` included, the newest release before it. `deploy/install --build` builds them from the checkout instead, which takes several minutes; EventStore's alone takes about ten on two cores, as it compiles the Log transform's Rust library. Run on its own, it installs with the defaults: no email, everything on local disk. Console is on port `3000`, ingest on `4000`. The services restart on failure and at boot, and their logs rotate.

SolidTrace keeps its own logs as Logs, in the **Meta Organization**: an Organization and Project both called "SolidTrace", which the install creates. Its admin is the first user to sign up, or the oldest user on an install that already had users; they invite others the usual way. Vector ([`deploy/vector/vector.yaml`](deploy/vector/vector.yaml)) reads every service's output from Docker and sends it there, with each Log's Service the Compose service's name. Lines written while Vector is down, during an upgrade or reboot, are only in `docker compose logs`. Deleting the Organization or its Project turns this off for good ([ADR 0011](docs/adr/0011-meta-organization-and-vector-reads-docker-logs.md)).

Sign-up works only until the install has a user, so sign up as soon as it is running; after that, people join by Invite. To let anyone sign up, `sudo docker compose -f deploy/docker-compose.yml exec console bin/rails instance_settings:set SIGN_UP_OPEN=true`, and `=false` to close it again.

Every MCP tool call and its Log reads are timed into the Meta Organization's "SolidTrace" Project, with the SQL EventStore ran for them. To time every Log read too, the explorer's, Dashboards' and Monitors', `sudo docker compose -f deploy/docker-compose.yml exec console bin/rails instance_settings:set LOG_ALL_READS=true`; they refresh every 15 seconds, so it adds many Logs.

With HTTPS, Caddy ([`deploy/caddy/Caddyfile`](deploy/caddy/Caddyfile)) gets a Let's Encrypt certificate for the machine's public IP or domain: Console is on `https://<host>`, ingest on `https://<host>:4443`. Ports 80, 443 and 4443 must be open to the internet, and 3000 and 4000 closed; setup prints the GCP firewall and static-IP commands. An IP's certificate lasts 160 hours, and Caddy renews it ([ADR 0010](docs/adr/0010-caddy-terminates-tls.md)).

To upgrade to a newer release: `git fetch --tags && git checkout <tag> && deploy/install`. To run `main`'s code: `git pull && deploy/install --build`. Either way it keeps the config and the secrets.

Everything the install keeps is in `deploy/`, which git ignores: `.env` (the secrets), `config/` and `data/`. Back up those three. `git clean -x` would delete them.

## Dev Setup

```bash
git clone https://github.com/solidtrace/solid_trace.git
cd solid_trace

cd console && bundle install && bun install && cd ..
cd event_store && go mod download && vrl/build && cd ..

cd console && bin/rails db:prepare
```

EventStore links the Rust `vrl` crate, built as a static library, for the Log transform. Rust comes through `mise`. `event_store/vrl/build` builds the library; run it before `go build` or `go test` in `event_store/`, and again after changing `event_store/vrl/rust/`. The first build takes about four minutes. EventStore's Docker image builds the library in a Rust stage of its own, with the crate's `dist` profile (fat LTO), so building the image needs only Docker.

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
