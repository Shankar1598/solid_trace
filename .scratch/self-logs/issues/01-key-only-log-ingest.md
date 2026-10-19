# 01: Key-only Log ingest

**Status:** ready-for-agent
**Spec:** [spec.md](../spec.md), decision 7

**What to build:** a Log ingest route that needs only the project key, and the Logs page's shipper snippets on it.

- [ ] EventStore: `POST /api/logs` takes the key as `/api/:project_id/logs` does (Sentry auth header or `Authorization: Bearer`), finds the Project from the key alone, and then ingests exactly as the existing route does: body limits, JSON and NDJSON, the same responses. An unknown key is a 401.
- [ ] `/api/:project_id/logs` is unchanged.
- [ ] Console: the curl, Vector and Fluent Bit snippets in `console/app/frontend/lib/logs/getStarted.ts` use `/api/logs`. The Sentry SDK snippet keeps its DSN.
- [ ] Tests: Go tests for the new route (a valid key stores the Logs in its Project; an unknown or missing key is refused); vitest for the snippets' URLs.
- [ ] A grep for other places that tell shippers the `/api/<id>/logs` URL (docs, README, MCP tools), updated to the key-only one where they describe shippers.
