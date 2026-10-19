# Caddy terminates TLS, for a bare IP

When an install turns on HTTPS, Caddy runs in front of Console and ingest and gets their certificates from Let's Encrypt. Console is at `https://<host>` and ingest at `https://<host>:4443`. The host can be the VM's public IP, with no domain. Caddy sets no body limit, so EventStore's own limits stay the only ones, as M1-11 left them when it removed Angie.

Let's Encrypt gives an IP address only a 160-hour certificate, from its `shortlived` profile. Caddy uses that profile for every certificate, IP or domain, so moving to a domain changes only the host setup asks for. One host means one name, so ingest has its own port instead of a hostname. A future reader may wonder why ingest isn't on 443: it's this. Moving it to `ingest.<domain>` would change every DSN already handed out.

## Considered options

- **Thruster's `TLS_DOMAIN`:** no new service, but it covers only Console, and needs a domain. Ingest would stay plain HTTP, or EventStore would need TLS of its own.
- **GCP's load balancer:** no certificates on the VM, but its managed certificates need a domain, and a certificate uploaded to it by hand would need replacing every six days for an IP.
- **A faster proxy in C or Rust** (HAProxy, nginx, Angie, Pingora, Sozu): no measurable difference behind Rails and EventStore. nginx and Angie bring back the 1 MB default body limit that M1-11 removed Angie for, and the Rust ones leave certificates to scripts.
- **An sslip.io hostname for the IP:** 90-day certificates and ingest on 443 under its own name. The user chose the bare IP for testing; a domain comes later.

## Consequences

- Caddy renews every few days. If it stays down for more than about six days, the certificate expires.
- The certificate names the IP, so the IP has to be static: a new one breaks HTTPS until Caddy gets a certificate for it, and breaks every DSN.
- Console and ingest still listen on 3000 and 4000 on the host network. The cloud firewall keeps them off the internet, and it also keeps clients from sending Console their own `X-Forwarded-For`, which Thruster now passes on.

Source: [.scratch/https/spec.md](../../.scratch/https/spec.md)
