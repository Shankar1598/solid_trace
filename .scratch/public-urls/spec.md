# Public URLs as instance settings

Planned with the user on 2026-11-10, when Tagalys' Console moves into Tailscale at `https://solid-trace.bluebuck-viper.ts.net` while ingest stays on the public IP (`https://35.214.179.92:4443`, through Caddy).

## Problem

Console's own URL and ingest's URL come from `deploy/.env` (`CONSOLE_URL`, `PUBLIC_INGEST_URL`, written by `deploy/setup-gcp`), read once at start, or from the address a request reached Console at (`request.base_url`). The two disagree:

- Issue emails take their host from `config.action_mailer.default_url_options`, set at boot from `CONSOLE_URL` (`console/config/environments/production.rb`, `app/views/issue_mailer/*`), and so does password reset.
- Invite links and MCP links use `request.base_url` (`User#invite_url`, `McpTools::Links`, `api_token_settings_controller`, `organization_users_controller`, `instance_settings_controller`).
- DSNs and the Logs snippets use `PUBLIC_INGEST_URL`, else Console's request host at port 4000 (`EventStore.public_ingest_url`).

Changing either URL means editing `.env` on the VM and restarting, and links built from the request depend on which address the admin happened to open.

## Solution

Two instance settings, on Settings → Instance and over the MCP server (`get_instance_settings`, `update_instance_settings`), that apply at once like the other editable settings:

- **Console URL**: `http(s)://host[:port]`, no path; a trailing slash is dropped.
- **Ingest URL**: `http(s)://host[:port][/path]`; a trailing slash is dropped.

Each is blank by default. Blank falls back to what applies today: the env var (`CONSOLE_URL`, `PUBLIC_INGEST_URL`), then the request's address (Console), or Console's host at port 4000 (ingest). So existing installs change nothing until an admin saves a value, and `deploy/setup-gcp` keeps writing `.env` as the first install's values.

One resolver per URL, used everywhere a Console or ingest URL is built: emails (Issue, invite, password reset, any other mailer or job), invite links, MCP links and the MCP URL on User settings, DSNs and the Logs snippets, `get_project_setup`. Jobs and mailers have no request, so they use setting → env → nothing (as now).

`CONSOLE_URL` and `PUBLIC_INGEST_URL` leave the "restart to change" list as URLs. The page shows the fallback a blank field uses.

### HTTPS stays a deploy choice

Whether TLS is terminated in front of Console (Caddy, Tailscale, or nothing) is decided at setup, not on the page: starting or stopping Caddy from Console would need the Docker socket. Console's HTTPS mode (`assume_ssl`, `force_ssl` with secure cookies) still follows `CONSOLE_URL`'s scheme at boot, as today. The settings page shows it as a restart-to-change row ("HTTPS", from `CONSOLE_URL`'s scheme).

So the dashboard can't lock an admin out: saving a Console URL whose scheme differs from the HTTPS mode Console started in is refused, with a message saying the scheme changes with `CONSOLE_URL` in `deploy/.env` and a restart. `bin/rails instance_settings:set` from a shell can still set anything, for recovery.

### Out of scope

- Tailscale on the VM (`tailscale serve` for 443 → 3000) and Caddy's Console site; deploy steps come after this ships.
- Clients that point at the instance: `~/.claude.json`'s MCP URL, `solid_trace_logs_url` in `~/Tagalys/infra`'s Ansible inventory, DSNs already in apps, docs naming the IP. Updated by hand once deployed.
- EventStore: it reads neither URL.
