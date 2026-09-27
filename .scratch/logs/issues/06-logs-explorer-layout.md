# 06: How should the Logs explorer be laid out?

Type: prototype
Status: resolved
Blocked by: 01
Map: [SolidTrace Logs](../map.md)

## Question

What does the Logs page look like and how does it behave: project and time-range selection, the query bar, histogram, results table, log detail, and how a user pivots from a value to a filter? React to a rough shadcn prototype informed by Axiom's and Better Stack's explorers.

## Answer

Decided with the user on 2026-09-27, while scoping the map to an MVP. No prototype was needed.

The MVP explorer is one page:

- A filter bar: Project, time range, and a free-text search.
- A results table, newest first, with time, level and message.
- Clicking a row opens that log's full JSON.
- A SQL tab whose results show in the same table.

No histogram, field sidebar, value-to-filter pivot, or live tail in the MVP; those are on the map's Future list. Component choices (table, SQL editor) are left to [Write the MVP spec and its tickets](13-write-v1-spec-and-first-milestone-tickets.md). What free-text search matches is decided in [How do the filter bar and SQL mode query logs?](11-query-language.md).
