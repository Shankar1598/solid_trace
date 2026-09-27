# 09: Saved Queries, history, share links and export

**What to build:** Users save the current Query with a name, reopen it from a menu, see their recent Queries, copy a link with a relative or an absolute time range, and download the current results as CSV or JSON.

**Blocked by:** 05

**Status:** wontfix

- [ ] `log_saved_queries` model, CRUD routes under the Organization scope, serializer and menu on the Logs page
- [ ] Query history per user (last 50) stored on run
- [ ] Copy link (relative keeps `range=15m`, absolute freezes start and end); links open the same Query
- [ ] Download CSV/JSON of the current result set, generated server-side from the same Query
- [ ] Controller and model tests

## Comments

2026-09-27: Closed as wontfix. This plan was superseded by the [logs map](../../logs/map.md). The file stays here as reference only.
