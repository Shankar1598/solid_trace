# 04: First Logs page: query API v0 and explorer shell

**What to build:** A user opens Logs in the Console, picks a Project and a time range, types a `where` filter in KQL-Lite, and sees a level-coloured histogram, a virtualised table of matching Logs and a detail sheet for any row. The Console proxies EventStore's new query, fields and record endpoints; the browser never calls EventStore.

**Blocked by:** 02

**Status:** wontfix

- [ ] EventStore exposes `POST /api/:pid/logs/query` (KQL restricted to `where`, `take`, `sort` in this ticket), `GET /api/:pid/logs/fields`, `GET /api/:pid/logs/records/:id`, each scoped by Project and a mandatory time range, with a query timeout, row cap and a dedicated read connection pool
- [ ] Console routes, `LogsController`, a `LogStore` service and serializers exist; errors from EventStore surface to the page rather than becoming empty results
- [ ] `Logs/Index` page with Project selector, time-range presets, KQL input, histogram (recharts via the shadcn chart component), virtualised table with column chooser, and a detail sheet with fields and JSON tabs; URL carries project, query and range
- [ ] Sidebar gains a Logs entry with correct active-state matching; new shadcn primitives are added in the repository's registry style
- [ ] Controller tests with a signed-in user and WebMock-stubbed EventStore; handler tests for the three endpoints

## Comments

2026-09-27: Closed as wontfix. This plan was superseded by the [logs map](../../logs/map.md). The file stays here as reference only.
