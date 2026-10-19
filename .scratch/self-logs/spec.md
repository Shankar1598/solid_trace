# SolidTrace's own Logs

Planned with the user on 2026-10-19. The decisions are in [ADR 0011](../../docs/adr/0011-meta-organization-and-vector-reads-docker-logs.md). Terms are in [CONTEXT.md](../../CONTEXT.md): see **Meta Organization**.

## What we're building

A VM install sends SolidTrace's own logs into SolidTrace. Every line that Console, the jobs, EventStore, Caddy and `db_prepare` write becomes a Log in a "SolidTrace" Project. From there it is kept, searched and charted like any other Log, and it outlasts the containers.

- **The Meta Organization:** `db_prepare` seeds an Organization called "SolidTrace" with a Project called "SolidTrace", before the other services start.
- **Vector**, one more Compose service, reads every SolidTrace container's output from Docker and sends it to Log ingest on `127.0.0.1:4000`.
- **Log ingest** gets a route that needs only the project key: `POST /api/logs`.

## Decisions

1. **Vector reads Docker's logs**, through the Docker socket (`docker_logs` source). The services keep logging to stdout as now. There is no `deploy/logs/` folder, no rotation to manage, and `docker compose logs` works as before. Lines written while Vector is down are not shipped (a few seconds on an upgrade or reboot). Vector's socket access is root-equivalent on the VM, which is accepted for a pinned, official image.
2. **Vector is always on**, on VM installs (`deploy/`) only, not in local development. It ships every Compose service of this project except itself: `console`, `jobs`, `event_store`, `caddy`, `db_prepare`.
3. **The Meta Organization is seeded by `db_prepare`**, on fresh installs and on existing ones (the user's VM), once. A flag in `instance_settings` records the seeding, so a deleted Organization or Project stays deleted; deleting either is how the feature is turned off. Vector's requests then get a 401 and are dropped.
4. **The project key comes from `deploy/.env`.** `deploy/install` generates it once, and the seeding gives the Project that key. Vector reads the same value.
5. **Its admin** is the first user to sign up on a fresh install, or the oldest user on an install that already has users. Admins invite others the usual way. It is otherwise an ordinary Organization: no protection from rename or leaving, no marking in the UI.
6. **Names:** the term in docs and code is **Meta Organization**. In the UI, the Organization and its Project are both "SolidTrace".
7. **Key-only Log ingest:** `POST /api/logs` finds the Project from the key alone (a project key belongs to one Project). It does what `/api/:project_id/logs` does. The old route stays, for shippers already set up. The Logs page's curl, Vector and Fluent Bit snippets use the new route; the Sentry SDK snippet keeps its DSN, as the Sentry protocol needs the Project id.
8. **Lines become Logs:**
   - Vector parses JSON lines (Caddy, Thruster) and logfmt lines (EventStore), filling time, Level and message.
   - EventStore turns off colour when its output is not a terminal, so no ANSI codes reach a message.
   - Rails' production lines start with their Level: `INFO [request-id] message`. Vector parses the prefix.
   - Each Log's Service is its Compose service name, with `container` as an Attribute, and no environment.
9. **Retention** is the instance default (30 days now), like any new Project.

## Out of scope

- Writing log files on the host.
- Local development (`bin/dev`).
- Shipping Vector's own output.
- Special treatment of the Meta Organization in the UI.

## Tickets

- [01](issues/01-key-only-log-ingest.md): key-only Log ingest, and the snippets that use it.
- [02](issues/02-seed-the-meta-organization.md): seed the Meta Organization.
- [03](issues/03-vector-ships-solidtraces-logs.md): Vector ships SolidTrace's logs. Blocked by 01 and 02.
