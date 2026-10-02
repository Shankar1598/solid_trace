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

**Status:** resolved

- [x] `deploy/docker-compose.yml` has no Angie service, and `deploy/angie/` is gone. The `/var/run/solid-trace` mount goes if nothing else uses it.
- [x] The deploy config has EventStore's ingest app listen on TCP port 4000. The unix-socket listener and the `ingest_socket` setting are removed, because nothing else uses them.
- [x] Console serves port 80 through Thruster. Anything Console relied on Angie for is checked and handled in Rails config: the `Host`, `X-Forwarded-For` and `X-Forwarded-Proto` headers, and `assume_ssl` / `force_ssl`.
- [x] EventStore's ingest server sets read and idle timeouts deliberately, now that no proxy buffers slow clients for it (Angie had a 10 s keepalive). Fiber's defaults wait forever.
- [x] Nothing outside `.scratch/` history mentions Angie.
- [x] Tests: EventStore's config test covers the ingest port, and the existing suites pass.

Out of scope: TLS (Thruster's `TLS_DOMAIN` can do it later, as deferred hardening) and the rest of review §10.

## Comments

### 2026-10-02: built (implementer)

**What changed**
- `deploy/docker-compose.yml` has no Angie service and no `/var/run/solid-trace` mount. `deploy/angie/` is deleted.
- `deploy/event_store.yml` sets `ingest_port: "4000"` explicitly.
- EventStore no longer has `ingest_socket`, `Config.SocketPath` or the unix-socket listener (`listenIngest`). `main.go` calls `app.Listen(":" + cfg.Port)`.
- New `event_store/config/config_test.go` covers four cases through `config.Load` and `EVENT_STORE_CONFIG`, as an operator would load it:
  - the shipped deploy config gives port 4000;
  - with no config file, the port defaults to 4000;
  - `ingest_port` in the config file is used;
  - a config left over from the old deploy that still sets `ingest_socket` loads, and ingest listens on 4000. yaml.v3 ignores unknown keys, so an operator's old `/etc/solid-trace/event_store.yml` keeps working.

**Timeouts.** The ingest app sets `ReadTimeout` to 30 s and `IdleTimeout` to 10 s, inline in `main.go`. They aren't configurable.
- `ReadTimeout` covers reading one request's headers and body. fasthttp v1.51 starts it when the connection opens, or when a keep-alive request's first byte arrives. 30 s is Thruster's own default. A 10 MB compressed Log batch (M1-04's cap) needs about 2.7 Mbit/s to arrive in time.
- `IdleTimeout` is the wait between keep-alive requests. 10 s matches Angie's old `keepalive_timeout`.
- There's no `WriteTimeout`, because ingest responses are tiny. The query app is unchanged, since it is loopback only.
- No test sends a slow request, because the ticket names only the config test.

**What Console relied on Angie for.** I checked this by running Thruster 0.1.17 in front of a server that echoes request headers.
- **`Host`:** Thruster passes it through and sets `X-Forwarded-Host` to the same value, as Angie's `Host $host` did. `config.hosts` stays unset in production, so any Host is accepted, as before.
- **`X-Forwarded-For`:** Thruster connects to Puma from 127.0.0.1. Rails' `RemoteIp` trusts that address, so `request.remote_ip` (saved on Sessions) is the client Thruster saw.
- **Forwarded headers from clients:** with TLS off, Thruster's `FORWARD_HEADERS` is on by default. It then passes on a client's own `X-Forwarded-For`, `-Proto` and `-Host`. A client-sent `X-Forwarded-Host` would become Rails' `request.host`. Angie had overwritten `-Proto` with `$scheme`.
  - Compose now sets `FORWARD_HEADERS: "false"` on Console. Thruster then sets all three from the connection: the peer address, the `Host`, and `http`.
  - Thruster also turns `FORWARD_HEADERS` off by itself once `TLS_DOMAIN` is set.
- **`assume_ssl` / `force_ssl`:** both stay off, because nothing terminates TLS. Angie didn't either. A comment in `production.rb` says so.

**Deviation: port 80 needs a host setting.** Console's image runs Thruster as uid 1000. Under `network_mode: host`, a non-root process can't bind ports below 1024, because the host's `net.ipv4.ip_unprivileged_port_start` is 1024 by default. I verified this with Docker 25.0.2 on the dev box:
- as uid 1000, binding port 81 fails with "Permission denied";
- as root, it works;
- `--cap-add NET_BIND_SERVICE` doesn't help, because Docker gives a non-root user no ambient capabilities;
- Docker refuses that sysctl inside the host's network namespace.

I documented the host prerequisite in the compose file: `sysctl net.ipv4.ip_unprivileged_port_start=80`, persisted in `/etc/sysctl.d/`. That keeps the image unchanged and adds no layer. The alternatives are:
- `setcap cap_net_bind_service=+ep` on the `thrust` binary in Console's Dockerfile. This is self-contained, but it adds a build step I couldn't verify on this box;
- running Console as root, which would also change who owns the files in `/storage`.

**Follow-ups (not done, outside this ticket)**
- `docker compose config` fails with "dependency cycle detected: db_prepare". The cycle was already on the base branch. `db_prepare` inherits `depends_on: [db_prepare]` from the `console` anchor. `depends_on: []` on `db_prepare` fixes it, and the rest of the file then parses.
- Console has no request-body cap now. Thruster's `MAX_REQUEST_BODY` defaults to 0 (no limit), and nginx's 1 MB default used to cover Console too. This belongs with the deferred hardening, alongside TLS.
- M1-04: no proxy caps ingest bodies any more. Fiber's 4 MB app-wide `BodyLimit` is the only limit until M1-04 sets its own.
