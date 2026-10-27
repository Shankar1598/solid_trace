# SolidTrace

Self-hosted error tracking and logs, on one machine.

SolidTrace groups your application's errors into Issues and keeps its Logs, with Dashboards, Monitors and an MCP server for agents on top. It runs as a handful of containers on a single VM that you own, and it's a 0.x prototype: it runs in production, but it isn't yet hardened for it.

## Philosophy

- **One machine, scaled up.** For low to medium scale, a bigger machine beats a cluster. No replicas, no orchestration: fewer moving parts, less to maintain, less to go wrong.
- **Few parts.** Self-hosting Sentry means running many databases and services. SolidTrace is two services, the embedded databases SQLite and Pebble, and one ClickHouse server beside them.
- **Your data, your bill.** Hosted log tools charge by the volume you send. On your own VM, the cost is the VM, its disk and an optional bucket, and the data stays in your account.
- **Strong UX.** A lightweight architecture shouldn't mean a rough tool.
- **Agents welcome.** Through MCP, an agent can query Logs, read Issues and build Dashboards and Monitors, as you would in the UI.

## What it does

- **Issues** from Sentry's SDKs, grouped, with comments and mentions.
- **Logs** sent as JSON over HTTP, searched and charted with a [Query language](docs/logs/query-language.md) or [SQL](docs/logs/sql.md), shaped on arrival by a Log transform, and moved to an [S3 or GCS bucket](docs/logs/object-store.md) as they age.
- **Dashboards** with filters, and **[Monitors](docs/logs/monitors.md)** that notify by email, Slack or PagerDuty.
- **The Overview**: what came in, what was kept and what it costs.
- **An [MCP server](docs/mcp.md)** for Claude Code, Cursor and other agents.

## Scale

One machine goes a long way. A production install on a GCP n4a-highmem-2 VM (2 Arm vCPUs, 16 GB) takes about 330 million Logs a day, 230 GiB as received: about 3,900 a second on average, 9,200 at the 99th percentile, and bursts of nearly 100,000 a second while shippers catch up after an outage. Its CPU averages 45%, and its memory is about 20% used.

## Install

On a Debian or Ubuntu machine, from the newest [release](https://github.com/Shankar1598/solid_trace/releases):

```bash
git clone --branch v0.14.1 https://github.com/Shankar1598/solid_trace.git
cd solid_trace
deploy/setup-gcp
```

It asks a few questions, installs Docker if it's missing and starts SolidTrace. Sign up as soon as it's running: the first user is the admin.

## Docs

- [Install and run](docs/install.md): HTTPS, users, email, upgrades, backups and high availability
- [Logs](docs/logs/README.md), [Traces](docs/traces.md) and [MCP](docs/mcp.md)
- [Development](docs/development.md)
- [Release notes](docs/changelogs/)
