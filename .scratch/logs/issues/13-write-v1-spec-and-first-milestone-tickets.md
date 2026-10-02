# 13: Write the v1 spec and first-milestone tickets

Type: task
Status: claimed
Blocked by: 08, 10, 11, 12, 14, 15, 16
Map: [SolidTrace Logs](../map.md)

## Question

Assemble the v1 spec from the resolved tickets, slice v1 into milestones, and write implementation tickets for the first milestone. Resolved when the spec and tickets exist and every decision in them points to a resolved ticket on this map.

Input (2026-10-02, from [How are logs stored and retained?](10-storage-architecture-and-retention.md)): the full-scale benchmark is not a prerequisite. Include a first-milestone ticket that load-tests the chosen storage design on the target VM (4 vCPU, 16 GB) at 3–5 TB/month with 5× bursts; it can merge with the log ingest load-test scenario in the map's fog.

## Comments

### 2026-10-02 spec written

The v1 spec is [logs-v1/spec.md](../../logs-v1/spec.md) (`ready-for-agent`), written with `/to-spec`. Test seams agreed with the user: the EventStore test environment with a controllable clock (main seam), Console request tests, and vitest for the explorer's state logic. The spec proposes three milestones and lists the choices it made that no ticket did (Further Notes). Still to do for this ticket: first-milestone tickets (`/to-tickets`).
