# HTTPS for a VM install

Planned with the user on 2026-10-19, for testing on a GCP VM with no domain yet. The decision is in [ADR 0010](../../docs/adr/0010-caddy-terminates-tls.md).

## What we're building

`deploy/setup-gcp` can turn on HTTPS. Caddy, one more Compose service, gets Let's Encrypt certificates and terminates TLS for Console and ingest:

- Console at `https://<public host>` (443), proxied to Thruster on `127.0.0.1:3000`.
- Ingest at `https://<public host>:4443`, proxied to EventStore on `127.0.0.1:4000`.

The public host is the VM's external IP for now, and a domain later. Both use the same config: a domain is a different value typed into setup.

## Decisions

1. **HTTPS is optional**, asked in setup's Public URLs stage, with yes as the default. Without it the install runs as before: plain HTTP on 3000 and 4000, no Caddy.
2. **Bare IP, no domain.** Let's Encrypt has issued IP certificates since 2026-01-15, only from its `shortlived` profile (160 hours). With one host, ingest gets its own port, `4443`, instead of a hostname.
3. **One checked-in Caddyfile** (`deploy/caddy/Caddyfile`) reads `PUBLIC_HOST` from `deploy/.env`. Caddy is a Compose service under the `https` profile, which setup turns on with `COMPOSE_PROFILES=https`. Every certificate uses the `shortlived` profile, IP or domain, so a domain later changes one value. The cost is renewal every few days.
4. **Ports 3000 and 4000 still listen on the VM.** A GCP firewall rule opens only 80, 443 and 4443. Setup prints the `gcloud` commands in a stage of its own, with reserving the IP as static, since the certificate and every DSN name the IP.
5. **Image and data.** `caddy:2.11.4-alpine`, pinned. Certificates live in `/var/lib/solid-trace/caddy`, so restarts don't hit Let's Encrypt's rate limits. No ACME email.
6. **Console behind Caddy**: with an `https://` `CONSOLE_URL`, `assume_ssl` and `force_ssl` are on, with HSTS off until there's a domain (browsers ignore HSTS for IPs anyway). Thruster forwards `X-Forwarded-*` headers (`FORWARD_HEADERS=true`), so `remote_ip` sees the client: the password-reset rate limit and login sessions use it.
7. **No fallback when a certificate can't be had.** `deploy/install` checks Console and ingest over HTTPS after starting, warns and names Caddy's logs if they fail, and finishes anyway.
8. **Setup suggests the public IP** from GCP's metadata server, and refuses private and loopback addresses, which Let's Encrypt can't validate.

## Out of scope

- Binding Console and EventStore to `127.0.0.1` (the firewall closes them).
- A domain, 90-day certificates, ingest on its own hostname.
- IPv6 public hosts.
