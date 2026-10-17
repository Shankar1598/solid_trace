# 03: Fields endpoint in EventStore

**Status:** ready-for-agent
**Track:** Go
**Blocked by:** none

## What to build

`GET /api/:project_id/logs/fields?from&to&limit` in EventStore (`event_store/handler/routes.go`, `handler/logs.go`, `storage/logs_*.go`). It answers the Project's Fields in the range, so an agent can learn what to query:
- the built-in fields, as listed in `docs/logs/query-language.md`;
- the top Attributes as `{ name, type, logs }`, summed from `logs.log_key_counts` over the range's UTC days and sorted by `logs`. `limit` defaults to 100 and is capped at 500.

Add `EventStore.logs_fields(project_id:, from:, to:, limit: nil)` to `console/app/services/event_store.rb`, with the same error handling as the other logs calls.

## Acceptance

- Go tests:
  - sums across days;
  - range edges by UTC day;
  - the limit and its cap;
  - a Project with no counts answers only the built-in fields;
  - another Project's counts are never included.
- A Rails service test with WebMock.
- `go test -p 1 ./...` passes.

## Comments
