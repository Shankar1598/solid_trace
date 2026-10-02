# 01: EventStore Event count honours the time window

**What to build:** A Console caller asking EventStore for an Issue's Event count within a time window gets the number of Events inside that window, not the lifetime count. The Event count applies the same filters as the Event listing (Project, Issue fingerprints, `newer_than`, `older_than`, tags) through one predicate builder that the listing, the count and the Event-with-context query all use, so the filters can't drift apart again. A malformed `newer_than` or `older_than` on the Event query routes is rejected with HTTP 400 and an error, instead of being silently ignored. Otherwise the HTTP contract of `/api/:project_id/events/count` is unchanged: same parameters, same `{"count": n}` response.

See spec: "EventStore: windowed Event count" (user stories 26, 27).

**Blocked by:** None (can start immediately)

**Status:** resolved

- [x] The count route honours `newer_than` and `older_than`, each alone and together
- [x] The time window combined with fingerprint filtering returns the intersection
- [x] Regression: a windowed count request no longer returns the lifetime count
- [x] A malformed `newer_than` or `older_than` returns 400 with an error on the count route and the listing route
- [x] The listing, the count and the Event-with-context query (for the filters it supports) build their WHERE clauses from one shared predicate builder
- [x] Tests drive the HTTP route through an in-process Fiber app with a real DuckDB in a temporary directory, not the storage function beneath it
