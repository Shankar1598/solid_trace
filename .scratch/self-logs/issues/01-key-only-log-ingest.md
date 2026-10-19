# 01: Key-only Log ingest

**Status:** resolved
**Spec:** [spec.md](../spec.md), decision 7

**What to build:** a Log ingest route that needs only the project key, and the Logs page's shipper snippets on it.

- [x] EventStore: `POST /api/logs` takes the key as `/api/:project_id/logs` does (Sentry auth header or `Authorization: Bearer`), finds the Project from the key alone, and then ingests exactly as the existing route does: body limits, JSON and NDJSON, the same responses. An unknown key is a 401.
- [x] `/api/:project_id/logs` is unchanged.
- [x] Console: the curl, Vector and Fluent Bit snippets in `console/app/frontend/lib/logs/getStarted.ts` use `/api/logs`. The Sentry SDK snippet keeps its DSN.
- [x] Tests: Go tests for the new route (a valid key stores the Logs in its Project; an unknown or missing key is refused); vitest for the snippets' URLs.
- [x] A grep for other places that tell shippers the `/api/<id>/logs` URL (docs, README, MCP tools), updated to the key-only one where they describe shippers.

## Comments

- EventStore: `POST /api/logs` (`IngestHandler.KeyOnlyLogs`) shares one body with `/api/:project_id/logs` (`ingestLogs`); only the path check differs. Tests `TestKeyOnlyLogRouteStoresTheLogsInTheKeysProject` (every Sentry key form and Bearer, JSON and NDJSON, stored in the key's Project only) and `TestKeyOnlyLogRouteRejectsAMissingOrUnknownKey` in `event_store/logs_test.go`. Ran those plus the existing `TestLogRoute*`, `TestEachProjectListsOnlyItsOwnLogs`, `TestNDJSON*`, `TestLogIngest*`: pass.
- Console: `getStarted.ts` curl, Vector and Fluent Bit snippets use `/api/logs`; the Sentry DSN is unchanged. `getStarted.test.ts` passes.
- Grep: the MCP tool `get_project_setup` gave shippers `/api/<id>/logs` through `Project#logs_url`; it now gives `/api/logs` (`test/mcp/admin_tools_test.rb`, `test/models/project_test.rb` pass). No docs or README described the URL. Console's own "Send a test Log" (`EventStore.ingest_logs`) and `lib/tasks/dev_logs.rake` still post to `/api/<id>/logs`, which is unchanged; they are not shipper instructions.
