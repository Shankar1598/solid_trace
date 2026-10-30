# 01: Let a timed-out run's answer through Thruster

**What to build:** Console runs behind Thruster, whose write timeout defaults to 30 s, the same as Console's run timeout (`RUN_TIMEOUT_SECONDS`). A run stopped at its timeout answers a few milliseconds after Thruster has closed the connection. Caddy then logs `EOF`, and the client gets an error with an empty body: over MCP, a bare `Error POSTing to endpoint:` instead of "try a shorter time range". Give Thruster a write timeout well above the run budget, so the answer gets through. See the [spec](../spec.md)'s Findings.

**Blocked by:** None (can start immediately)

**Status:** resolved

- [x] The console service in `deploy/docker-compose.yml` sets `HTTP_WRITE_TIMEOUT=60`, with a comment saying why it must stay above `RUN_TIMEOUT_SECONDS`.
- [x] The 30 s run timeout is unchanged.
- [x] Check that nothing else on the path cuts first: Thruster's `HTTP_READ_TIMEOUT` and `HTTP_IDLE_TIMEOUT`, and Caddy's `reverse_proxy` timeouts.
- [ ] On the dev stack, a run that times out (for example, a Query with the run timeout lowered for the test) answers over MCP with EventStore's timeout message, and Caddy logs no `EOF`.
- [ ] After a release on production, a timed-out MCP `query_logs` returns "The run was stopped after 30 s…" to the agent.

## Comments

**2026-10-30, built on `feature/query-timeouts` (`be2ce26b`).**
- `deploy/docker-compose.yml` sets `HTTP_WRITE_TIMEOUT: "60"` on the console anchor, with a comment saying why. The anchor also reaches `jobs` and `db_prepare`, which don't run Thruster, so it's harmless there.
- `EventStoreProxy::RUN_TIMEOUT_SECONDS` gains a comment pointing at it.
- Thruster 0.1.27's README confirms the defaults: `HTTP_WRITE_TIMEOUT` 30, `HTTP_READ_TIMEOUT` 30 (the client sending its request), `HTTP_IDLE_TIMEOUT` 60.
- Caddy's `reverse_proxy` sets no response timeout, only `keepalive 5s`.
- `docker compose config` renders the variable.
- Not checked on a dev stack, which may not run Thruster. The production check waits for a release.
