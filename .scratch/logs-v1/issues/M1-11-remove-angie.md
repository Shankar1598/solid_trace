# M1-11: Remove Angie; Console and EventStore listen directly

**What to build:** a deploy with no reverse proxy in front of the two services. Angie, the nginx fork in `deploy/`, does three things. It forwards port 80 to Console and port 4000 to EventStore's unix socket. It terminates no TLS. And it adds a body limit of its own (nginx's 1 MB default) that caps every ingest request, Log batches included. Remove it:
- EventStore's ingest app listens on TCP port 4000 itself.
- Console serves port 80 through Thruster, as its Dockerfile already does.

Body limits then live only in EventStore, where [M1-04](M1-04-log-body-rules-and-overload.md) sets them. Asked for by the user on 2026-10-02, during the logs v1 implementation.

Why the deploy can't work as written anyway:
- `angie.conf` proxies to `http://console:80`, which doesn't resolve under `network_mode: host` ([prod-readiness review](../../prod-readiness/review.md) §10).
- On the host network, Angie and Console's Thruster would both bind port 80.
- EventStore already supports both `ingest_port` (TCP) and `ingest_socket` (unix). The socket exists only for Angie.

**Blocked by:** None (can start immediately).

**Status:** ready-for-agent

- [ ] `deploy/docker-compose.yml` has no Angie service, and `deploy/angie/` is gone. The `/var/run/solid-trace` mount goes if nothing else uses it.
- [ ] The deploy config has EventStore's ingest app listen on TCP port 4000. The unix-socket listener and the `ingest_socket` setting are removed, because nothing else uses them.
- [ ] Console serves port 80 through Thruster. Anything Console relied on Angie for is checked and handled in Rails config: the `Host`, `X-Forwarded-For` and `X-Forwarded-Proto` headers, and `assume_ssl` / `force_ssl`.
- [ ] EventStore's ingest server sets read and idle timeouts deliberately, now that no proxy buffers slow clients for it (Angie had a 10 s keepalive). Fiber's defaults wait forever.
- [ ] Nothing outside `.scratch/` history mentions Angie.
- [ ] Tests: EventStore's config test covers the ingest port, and the existing suites pass.

Out of scope: TLS (Thruster's `TLS_DOMAIN` can do it later, as deferred hardening) and the rest of review §10.
