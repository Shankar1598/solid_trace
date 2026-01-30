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

bundle install
bun install

cd services/event_store && go mod download && cd ../..

bin/rails db:prepare
```

## Run

```bash
bin/dev
```

Starts Rails, Vite, EventStore, and background jobs via `Procfile.dev`.

## Project Structure

- `app/` — Rails code
- `services/event_store/` — Go service
- `config/` — Config files
- `db/` — Migrations and schema


