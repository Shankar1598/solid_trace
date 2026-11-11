# Console's and ingest's URLs are instance settings; HTTPS stays a deploy choice

Amends [ADR 0010](0010-caddy-terminates-tls.md), whose one host for Console and ingest had to stay put: Console's URL can now move on its own, and [ADR 0015](0015-email-settings-in-console-encrypted-with-secret-key-base.md)'s "`CONSOLE_URL` stays in the environment".

Console's URL and ingest's URL, as people, links and SDKs reach them, are two columns of the `instance_settings` row, `console_url` and `ingest_url`. Instance admins set them on Settings → Instance or with `update_instance_settings`, and they apply at once. Each is blank by default, and blank falls back to what applied before: `CONSOLE_URL` or `PUBLIC_INGEST_URL` from `deploy/.env`, then the address the request reached Console at (Console) or Console's host at port 4000 (ingest). Jobs and mailers have no request, so they stop at the env var.

One resolver per URL, `PublicUrls.console_url` and `PublicUrls.ingest_url`, builds every link and DSN: emails (Issues, invites, password reset), Notifier links, invite links, the MCP URL and the MCP server's links, DSNs, the Logs snippets and `get_project_setup`. Mailers read it as each message is built, so a change applies to the next one in every process.

HTTPS stays a deploy choice. Console's HTTPS mode (`assume_ssl`, `force_ssl`, secure cookies) follows `CONSOLE_URL`'s scheme at boot, as before, since starting or stopping the TLS proxy in front of Console from Console would need the Docker socket. So the dashboard can't lock its admin out, the page and MCP refuse a Console URL whose scheme differs from that mode. The page shows HTTPS as a restart-to-change row. `bin/rails instance_settings:set CONSOLE_URL_SETTING=…` saves any scheme, for recovery.

We chose this because Tagalys' Console moved into Tailscale while ingest stayed on the public IP: changing either URL meant editing `deploy/.env` and restarting, and links built from the request depended on which address the admin happened to open.

## Considered options

- **Env vars only, as before:** a shell and a restart for every move, and links that disagree between jobs (`CONSOLE_URL`) and pages (the request).
- **The request's address everywhere:** right behind any proxy, but jobs have no request, and an admin on one address hands out links to it.
- **HTTPS as a setting too:** it would need Console to start and stop Caddy, or Tailscale's serve, and a wrong save could leave the admin with no way back in but a shell.

## Consequences

- Saving a new Ingest URL changes only the DSNs Console shows from then on; DSNs already in apps keep sending to the old one, which must keep working until they're changed.
- `CONSOLE_URL` keeps two jobs: Console's HTTPS mode, and the fallback for a blank Console URL. `deploy/setup-gcp` writes it and `PUBLIC_INGEST_URL` for the first install, as before.
- With neither a setting nor `CONSOLE_URL`, emails and Notifier links point at `localhost:3000`.

Source: [.scratch/public-urls/spec.md](../../.scratch/public-urls/spec.md)
