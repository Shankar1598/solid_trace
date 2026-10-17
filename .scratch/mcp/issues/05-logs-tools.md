# 05: Logs tools

**Status:** ready-for-agent
**Track:** Console
**Blocked by:** 02, 03

## What to build

The Logs tools from the spec's Tool catalogue, answered as the spec's "Small, linked answers" says.

- **`query_logs`** (organization, project, query, range or from/to, limit, cursor) goes through `ChartRuns.hold` and `EventStore.query_logs` with `EventStoreProxy::RUN_TIMEOUT_SECONDS`.
  - Logs come back as compact lines with the next cursor; a table comes back as a header plus at most 100 rows.
  - The answer ends with the explorer link for that Query and range, built as `console/app/frontend/lib/logs/explorerState.ts` reads its URL.
  - Its description carries the short cheat sheet from the spec.
- **`get_log`** returns the whole Log, through `EventStore.get_log`.
- **`list_fields`** goes through `EventStore.logs_fields` from 03.
- **`query_language_reference`** returns `console/app/mcp/docs/query-language.md`, a copy of `docs/logs/query-language.md`. A test fails when the two differ.
- **`run_sql`** goes through `EventStore.run_logs_sql`. It is listed only while SQL is on, and it is a read tool (`readOnlyHint`) because 04 makes SQL read-only.

## Acceptance

- `console/test/mcp/logs_tools_test.rb`, with EventStore stubbed:
  - Logs and table shapes, with their caps and cursor;
  - the link;
  - Query mistakes come back as tool errors with their positions;
  - Busy and TimedOut;
  - SQL off;
  - the copy test.
- `bin/rails test` passes.

## Comments
