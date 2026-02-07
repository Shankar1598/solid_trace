# SolidTrace

A self-hosted error tracking tool with lightweight architecture and strong UX.

## Why?

Self-hosting Sentry means managing multiple databases and services. SolidTrace does the same job with two services and embedded databases (SQLite, DuckDB, RocksDB).

For low to medium scale application, Vertical scaling is often a better solution than scaling horizontally with multiple services. Less moving parts, less maintenance.

SolidTrace currently does not focus on offering high availability or massive scale. Periodic backups of VM/VPS should be sufficient for most use cases.

## Architecture

Two services:

- **Console** — Rails app. UI, API, user management.
  - Built with Rails, React, Inertia.js, Tailwind CSS.
  - Uses SQLite for relational data
- **EventStore** — Go service. Ingests and processes events.
  - Uses RocksDB for event ingest and DuckDB for analytical queries.

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


