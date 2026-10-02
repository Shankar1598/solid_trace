# M2-02: Histogram and summary endpoint

**What to build:** for any filters, EventStore returns Log counts per time bucket per Level, the total Log count and per-Level counts, compiled through the same Query compiler. Spec: [Query compiler and query endpoints](../spec.md#query-compiler-and-query-endpoints-eventstore) (Histogram and summary); user stories 48, 50.

**Blocked by:** M2-01.

**Status:** ready-for-agent

- [ ] A query-app operation takes a Project, time range, Query and bucket size, and returns counts per bucket per Level (Logs with no Level counted as their own group), the Log count and per-Level counts.
- [ ] It is built as a Query through the compiler, not hand-written SQL, so it applies exactly the filters the stream does. For a Query ending in a tabular stage (M3-01), the counts are of the Logs that pass the filters before that stage.
- [ ] Tests: counts match the rows the run operation returns for the same filters, across hot table and Parquet; empty buckets; Logs without a Level.
