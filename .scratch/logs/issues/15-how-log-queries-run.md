# 15: How do log queries run?

Type: grilling
Status: resolved
Blocked by:
Map: [SolidTrace Logs](../map.md)

## Question

How are log reads authorized and routed (through Console, as Event queries go to EventStore's loopback-only query listener, or directly to EventStore), and what caps bound a query's results and resource use? SQL queries are unsandboxed and share EventStore's DuckDB with Log and Event processing ([ADR 0003](../../../docs/adr/0003-unrestricted-sql-queries-behind-an-instance-setting.md)).

Input (2026-10-02, user): **no per-query timeouts for now.** This changes [How are logs stored and retained?](10-storage-architecture-and-retention.md), whose answer said a per-query timeout stops a day-long rare-key search.

## Answer

Resolved 2026-10-02 by the user: **left to the implementer's judgement.**

- **No per-query timeouts** in v1. A running Query stops when the user cancels it ([ticket 16](16-explorer-building-blocks.md)).
- How log reads are authorized and routed (through Console to EventStore's loopback query listener, as Event queries go, or otherwise), whether a cancel stops the query on the server, and any caps on results or resource use are the implementer's call. [ADR 0003](../../../docs/adr/0003-unrestricted-sql-queries-behind-an-instance-setting.md) still bounds SQL queries.
