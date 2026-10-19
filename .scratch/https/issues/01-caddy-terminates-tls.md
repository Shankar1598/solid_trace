# 01: Caddy terminates TLS for Console and ingest

**Status:** ready-for-human
**Spec:** [spec.md](../spec.md)

**What to build:** the [spec](../spec.md)'s decisions, in `deploy/` and Console's production config.

- [x] `deploy/caddy/Caddyfile`: `PUBLIC_HOST` on 443 to Console and on 4443 to ingest, the `shortlived` issuer, `default_sni`.
- [x] A `caddy` service in `deploy/docker-compose.yml` under the `https` profile, with its data in `/var/lib/solid-trace/caddy`. Thruster's `FORWARD_HEADERS` comes from `deploy/.env`.
- [x] `deploy/setup-gcp` asks whether to use HTTPS, suggests and checks the public IP, writes `PUBLIC_HOST`, the URLs, `FORWARD_HEADERS` and `COMPOSE_PROFILES`, and prints the GCP static IP and firewall commands.
- [x] `deploy/install` creates Caddy's data directory, reloads Caddy's config, removes Caddy when HTTPS is off, checks both HTTPS URLs and prints them.
- [x] Console's `production.rb`: `assume_ssl` and `force_ssl` from `CONSOLE_URL`, HSTS off.
- [x] Comments that say nothing is in front of Console or ingest are updated; the README's install section mentions HTTPS.
- [x] Checked: `caddy validate` on the Caddyfile, `docker compose config` with and without the profile, `bash -n` on the scripts, Console's production config with and without HTTPS.

The real test needs the user's GCP VM: `deploy/setup-gcp` with HTTPS, a valid lock at `https://<ip>`, and an Event sent to `https://<ip>:4443`.

## Comments

### 2026-10-19: built (implementer)

Built on `main`. What's left needs the user's GCP VM: run `deploy/setup-gcp` with HTTPS, check for a valid lock at `https://<ip>`, and send an Event to `https://<ip>:4443`. So the ticket is `ready-for-human`.

**Checked here**
- `caddy validate` and `caddy adapt` on `caddy:2.11.4-alpine` pass. The adapted config has `profile: shortlived`, `default_sni` on both servers, and 5-second keep-alives to both upstreams.
- `docker compose config`: the `caddy` service exists only with `COMPOSE_PROFILES=https`. Compose leaves a profile's container running when the profile is turned off, so `deploy/install` removes Caddy itself.
- Setup's public host check: refuses private, loopback, CGNAT and link-local IPv4 addresses, `.local` and `.internal` names, and anything with a scheme or port.
- Console's production config, with an `https://` and an `http://` `CONSOLE_URL`. Through `RemoteIp`, `AssumeSSL` and `SSL`:
  - HSTS is sent as `max-age=0`;
  - `/up` is answered with no redirect;
  - `base_url` is `https://`;
  - `remote_ip` is the client's address from `X-Forwarded-For` behind two loopback hops.

**Beyond the spec**
- Caddy reuses an upstream connection for at most 5 seconds. EventStore closes idle connections after 10 seconds; with Caddy's default of 2 minutes, an ingest POST could have gone out on a connection EventStore was closing.

**Review (2026-10-19)**, Standards and Spec axes. Fixed:
- The ADR number clashed with 0006 (Monitors), so it's now [0010](../../../docs/adr/0010-caddy-terminates-tls.md), with Considered options and a Source line.
- Two comments still said ingest has nothing in front (`deploy/event_store.yml`, the Compose comment).
- The Caddyfile's repeated proxy block became a snippet.
- Turning HTTPS off left an empty `PUBLIC_HOST`, which hid the suggested IP on the next run. Setup now deletes it.

Left as they are (low):
- The HTTPS on/off state is in three `.env` values: `HTTPS` is setup's saved answer, and Compose needs `COMPOSE_PROFILES` and `FORWARD_HEADERS` set separately.
- Console's fallback ingest URL (`<scheme>://<host>:4000`, `event_store.rb`) is never used with HTTPS on, because setup always writes `PUBLIC_INGEST_URL`.
