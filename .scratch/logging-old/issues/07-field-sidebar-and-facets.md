# 07: Field sidebar with top values and click-to-filter

**What to build:** A sidebar on the Logs page lists the Fields seen for the selected Project and time range with their kinds; clicking a Field shows its top values and counts for the current Query, and clicking a value adds a `where` filter. The detail sheet offers "filter for" and "filter out" on any value and "view in context" around a Log.

**Blocked by:** 05

**Status:** wontfix

- [ ] `GET /api/:pid/logs/facets` returns top values for a Field over the filtered, time-bounded set using approximate top-k; the Console proxies it
- [ ] Sidebar search over field names; kinds shown; hidden system fields excluded
- [ ] Filter-for / filter-out from the sidebar and the detail sheet append correct KQL for string, number and boolean values
- [ ] "View in context" opens the Logs page centred on the record's time with its Project and Query

## Comments

2026-09-27: Closed as wontfix. This plan was superseded by the [logs map](../../logs/map.md). The file stays here as reference only.
