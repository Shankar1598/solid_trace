# Installing and running SolidTrace

## Install on a VM

On a Debian or Ubuntu machine, from the newest [release](https://github.com/Shankar1598/solid_trace/releases):

```bash
git clone --branch v0.14.1 https://github.com/Shankar1598/solid_trace.git
cd solid_trace
deploy/setup-gcp
```

`deploy/setup-gcp` asks, stage by stage, whether to use HTTPS and for Console's and ingest's public URLs, and whether older Logs go in a GCS bucket, with the `gcloud` commands that create the bucket and its HMAC key. It writes the answers to `deploy/.env` and `deploy/config/event_store.yml`, then runs `deploy/install`. Run it again to change any of them.

`deploy/install` installs Docker if it is missing, creates `deploy/data/` for data and EventStore's config (from the template `deploy/event_store.yml`) if there is none, writes Console's secret to `deploy/.env`, then starts the services with `deploy/docker-compose.yml`. Run on its own, it installs with the defaults: no email, everything on local disk. Console is on port `3000`, ingest on `4000`. The services restart on failure and at boot, and their logs rotate.

It pulls a release's images from GitHub's registry (`ghcr.io/shankar1598/solid-trace-console` and `-event-store`, for amd64 and arm64, so x86 VMs and Arm ones such as GCP's T2A and N4A both work), which each published release builds (`.github/workflows/release.yml`): the release the checkout is at, or on any other commit, `main` included, the newest release before it. `deploy/install --build` builds them from the checkout instead, which takes several minutes; EventStore's alone takes about ten on two cores, as it compiles the Log transform's Rust library.

ClickHouse gets half the machine's memory (`max_server_memory_usage`), set by `deploy/install`.

## HTTPS

With HTTPS, Caddy ([`deploy/caddy/Caddyfile`](../deploy/caddy/Caddyfile)) gets a Let's Encrypt certificate for the machine's public IP or domain: Console is on `https://<host>`, ingest on `https://<host>:4443`. Ports 80, 443 and 4443 must be open to the internet, and 3000 and 4000 closed; setup prints the GCP firewall and static-IP commands. An IP's certificate lasts 160 hours, and Caddy renews it ([ADR 0010](adr/0010-caddy-terminates-tls.md)).

## Users and instance admins

Sign-up works only until the install has a user, so sign up as soon as it is running; after that, people join by Invite. To let anyone sign up, `sudo docker compose -f deploy/docker-compose.yml exec console bin/rails instance_settings:set SIGN_UP_OPEN=true`, and `=false` to close it again.

The first user to sign up is an instance admin; on an install that already had users, the oldest is. To make someone else one, `sudo docker compose -f deploy/docker-compose.yml exec console bin/rails instance_admins:grant EMAIL=<their email>`; `instance_admins:revoke` and `instance_admins:list` do the rest. Instance admins change the install's settings on Settings → Instance, with no restart: SQL queries, the default Retention of Logs and of traces, log-all-reads, log-MCP-reads, whether Console sends its own Issues and traces, and the text-match budget, the most a Query that matches text may read before the Logs explorer and the MCP server refuse it (4 GiB by default, and 0 refuses none). The page also shows what Console and EventStore read at start.

## Email

Email is set up in Console once it is running: an instance admin sets the SMTP server and the sender on Settings → Email and sends a test email from there. Console encrypts the SMTP password with keys derived from its secret ([ADR 0015](adr/0015-email-settings-in-console-encrypted-with-secret-key-base.md)). It no longer reads `SMTP_*` and `MAIL_FROM` from `deploy/.env`: an install that set them there sends no email until an instance admin copies them into the page; then delete those lines.

## SolidTrace's own logs

SolidTrace keeps its own logs as Logs, in the **Meta Organization**: an Organization and Project both called "SolidTrace", which the install creates. Its admin is the first user to sign up, or the oldest user on an install that already had users; they invite others the usual way. Vector ([`deploy/vector/vector.yaml`](../deploy/vector/vector.yaml)) reads every service's output from Docker and sends it there, with each Log's Service the Compose service's name. Lines written while Vector is down, during an upgrade or reboot, are only in `docker compose logs`. Deleting the Organization or its Project turns this off for good ([ADR 0011](adr/0011-meta-organization-and-vector-reads-docker-logs.md)). Console also sends its Issues there, and traces 5% of its requests and jobs into it, with `SENTRY_DSN` and `SENTRY_TRACES_SAMPLE_RATE` in `deploy/.env` ([Traces](traces.md#solidtrace-traces-itself)).

Every MCP tool call and its Log reads are timed into the Meta Organization's "SolidTrace" Project, with the SQL EventStore ran for them. To time every Log read too, the explorer's, Dashboards' and Monitors', turn on Log all reads on Settings → Instance, or `sudo docker compose -f deploy/docker-compose.yml exec console bin/rails instance_settings:set LOG_ALL_READS=true`; they refresh every 15 seconds, so it adds many Logs. To stop timing the MCP server's Log reads (its tool calls stay timed), turn off Log MCP reads there, or `LOG_MCP_READS=false`; it applies while Log all reads is off.

## Upgrading

To upgrade to a newer release: `git fetch --tags && git checkout <tag> && deploy/install`. To run `main`'s code: `git pull && deploy/install --build`. Either way it keeps the config and the secrets. Each release's notes, with anything an upgrade needs, are in [`docs/changelogs/`](changelogs/).

Upgrading from a release that kept Logs in DuckDB (v0.10.0 or older), `deploy/install` first stops the services and runs the last such release's `prepare-clickhouse`, which readies its Logs for ClickHouse; EventStore won't start on DuckDB Logs that weren't prepared. The Logs from before the upgrade stay in their Log files, on disk or in the bucket, and are read in place until Retention deletes them; nothing is copied. Events from before it aren't moved. Once the upgrade has started, `logs.duckdb` and `events.duckdb` in `deploy/data/storage/duckdb/production/` can be deleted; keep `logs_parquet/` beside them. The config's DuckDB keys are ignored, each with a warning at start.

## Backups and high availability

SolidTrace runs on one machine, with no clustering or replicas. Everything the install keeps is in `deploy/`, which git ignores: `.env` (the secrets), `config/` and `data/` (SQLite, Pebble, ClickHouse and Caddy's certificates), plus older Logs in a bucket if one is set up. `git clean -x` would delete them. The bucket alone can't restore an install, as ClickHouse's record of its parts is on the disk ([ADR 0016](adr/0016-logs-and-events-in-clickhouse.md)).

- **Backups.** Periodic snapshots of the VM's disk recover from a lost VM or a mistake, back to the last snapshot.
- **A zone outage.** Put the checkout on a disk replicated across zones, such as GCP's regional Persistent Disk or Hyperdisk Balanced High Availability, and point shippers at a DNS name rather than the VM's IP. When the zone goes down, attach the disk to a new VM in the other zone (GCP's force-attach takes it from the VM that's down), run `deploy/install` and move the name: SolidTrace is back in minutes, with nothing lost. ClickHouse's read cache of the bucket, `deploy/data/clickhouse/caches`, refills on its own, so it can stay on the VM's own disk. A replicated disk copies a mistake to both zones at once, so it doesn't replace backups.
