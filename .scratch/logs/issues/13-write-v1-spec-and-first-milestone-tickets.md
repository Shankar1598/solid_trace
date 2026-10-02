# 13: Write the v1 spec and first-milestone tickets

Type: task
Status: open
Blocked by: 08, 10, 11, 12, 14
Map: [SolidTrace Logs](../map.md)

## Question

Assemble the v1 spec from the resolved tickets, slice v1 into milestones, and write implementation tickets for the first milestone. Resolved when the spec and tickets exist and every decision in them points to a resolved ticket on this map.

Input (2026-10-02, from [How are logs stored and retained?](10-storage-architecture-and-retention.md)): the full-scale benchmark is not a prerequisite. Include a first-milestone ticket that load-tests the chosen storage design on the target VM (4 vCPU, 16 GB) at 3–5 TB/month with 5× bursts; it can merge with the log ingest load-test scenario in the map's fog.
