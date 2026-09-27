# Query API has no authentication, and the deploy exposes it

Status: resolved

**Source:** `.scratch/prod-readiness/review.md` §2
**Severity:** Critical — cross-tenant data disclosure on any deploy using `deploy/`
**Area:** `event_store` (Go), `deploy/angie`, `console` (Rails client)

## Problem

The four read endpoints on the event store have no authentication of any kind, and
`deploy/angie/angie.conf` publishes them on a public port.

`event_store/main.go:95-104` registers every route with no middleware — there is no
`app.Use(...)` anywhere in the file:

```go
app.Post("/api/:project_id/store",         ingestHandler.Store)     // key-checked
app.Post("/api/:project_id/envelope",      ingestHandler.Envelope)  // key-checked
app.Get("/api/events/:event_uuid",         eventsHandler.GetEvent)            // no check
app.Get("/api/:project_id/events/context", eventsHandler.GetEventWithContext) // no check
app.Get("/api/:project_id/events/count",   eventsHandler.Count)               // no check
app.Get("/api/:project_id/events",         eventsHandler.List)                // no check
```

Ingest validates the Sentry key (`handler/ingest.go:44-50`). The read path validates
nothing. `project_id` is taken straight off the URL in
`handler/events.go:118-121` (`parseParams`) and passed to the DuckDB query as the only
tenant boundary:

```go
projectID, err := strconv.ParseUint(c.Params("project_id"), 10, 32)
```

`deploy/angie/angie.conf:31-40` then publishes that app on `listen 4000` (all
interfaces — every service in `deploy/docker-compose.yml` runs `network_mode: host`),
proxying to the event store socket.

## Impact

`Organization has_many :projects` (`console/app/models/project.rb:4`), so `project_id` is
a tenant boundary. Incrementing it walks every tenant on the install:

```
curl http://host:4000/api/1/events          # another org's event index
curl http://host:4000/api/1/events/count
curl http://host:4000/api/events/<uuid>     # full raw payload
```

`GetEvent` (`handler/events.go:26-40`) returns the stored Pebble blob verbatim — the
entire SDK payload: stack traces with source context, request headers, cookies, user
context, whatever the client attached. `List` yields the UUIDs needed to call it, so the
two chain into a full dump with no credential at any step.

This is a data breach on first deploy, not a hardening nice-to-have.

## The constraint that makes this non-trivial

Port 4000 cannot simply be firewalled. It serves **both** roles:

- **Public:** SDKs must reach `POST /api/:project_id/store` and `/envelope` from anywhere.
- **Private:** Rails reaches the read API over the same port —
  `console/app/services/event_store.rb:8` defaults `EVENT_STORE_URL` to
  `http://localhost:4000`, and `deploy/docker-compose.yml` sets it to exactly that.

So the fix has to separate the two classes of route, not close the port.

## Proposed fix

Defence in depth, all three layers:

**1. Shared-secret auth on the read routes (primary control).**
The read API is server-to-server only — Rails is the sole client. Add a
`config.internal_token` / `EVENT_STORE_INTERNAL_TOKEN`, require it as a header
(e.g. `X-SolidTrace-Internal-Token`) on the four read routes via a Fiber middleware
group, and compare with `crypto/subtle.ConstantTimeCompare`. Refuse to start if the
token is unset or shorter than ~32 bytes — a silently-optional token reintroduces the
bug on every install that forgets to set it. `EventStore.make_request` in
`console/app/services/event_store.rb:40-44` is the single place that needs the header
added.

**2. Block the read paths at the edge.**
In the `listen 4000` server block, allow only `/api/*/store`, `/api/*/envelope` and
`/health`; return 404 for everything else. Rails keeps reaching the read API over the
unix socket or loopback, not through the public listener.

**3. Scope `GetEvent` to a project.**
Even with a valid token, `/api/events/:uuid` currently reads any event on the install —
`KeyForEvent` (`storage/pebble.go:144-150`) is the bare UUID bytes, so the key carries no
tenant. Move it under `/api/:project_id/events/:event_uuid` and verify ownership before
returning the payload (the DuckDB row already carries `project_id`; a key prefix would
be the alternative but needs a migration). The token is shared across all projects, so
this does not limit a leaked token; it stops a console bug that passes the wrong UUID from
returning another project's payload.

## Also in scope (review §2, "Related")

`handler/ingest.go:49` and `:68` collapse two different failures into one response:

```go
projectID, err := h.auth.ValidateKey(publicKey)
if err != nil || projectID == 0 {
    return c.Status(401).JSON(fiber.Map{"error": "Invalid project key"})
}
```

`err != nil` is a SQLite failure, not a bad key. Sentry SDKs treat 401 as fatal and
**drop** the event instead of retrying, so a transient DB blip silently destroys traffic.
Split it: `projectID == 0` stays 401, `err != nil` becomes 500 (logged) so clients retry.

## Acceptance criteria

- [ ] Read endpoints reject requests with a missing or wrong internal token (401/403).
- [ ] Token comparison is constant-time; service refuses to boot without a token set.
- [ ] Rails `EventStore` sends the header; issue list, detail, count and context views
      all still work end to end.
- [ ] `curl http://host:4000/api/1/events` from off-box returns 404/401, not data.
- [ ] Event payload fetch is project-scoped and rejects a UUID from another project.
- [ ] SDK ingest against `:4000/api/:id/store` and `/envelope` is unaffected.
- [ ] `ValidateKey` DB error returns 500, invalid key returns 401; covered by a test.
- [ ] Deploy docs state the token is required and how to generate it.

## Out of scope

Per-user or per-org authorisation of the read API (Rails already enforces that before it
calls out), rate limiting (review §10), and the ingest-side key cache (review §10).

## Resolution (2026-09-26)

Built differently from the proposed fix. The query routes moved to a separate
listener instead of being authenticated on the shared one.

- **Separate query listener, loopback only.** The event store serves ingest
  (`store`, `envelope`, `/health`) on the public port and the query routes on
  `query_port` (default 4100), always bound to `127.0.0.1`. The query routes do
  not exist on the public listener, so no edge filtering in `angie.conf` is needed.
- **No internal token.** A shared-secret token was built, then removed as
  overengineered once the listener is loopback-only.
- **Project-scoped payload reads.** Pebble keys are `project_id (4 bytes,
  big-endian) + UUID`, so `GET /api/:project_id/events/:uuid` misses for another
  project's event. No migration: nothing was deployed.
- **Key lookup status.** An unknown Sentry key returns 401; a failed lookup
  returns 500 so SDKs retry.

**Accepted residual risk:** any process on the VM (and any container, under
`network_mode: host`) can read the query API. Rails posts to user-supplied URLs
for Slack and PagerDuty integrations; revisit if an integration ever issues GETs
to user-supplied URLs and shows the response.
