# 01: Day tables cleaned up by family

Spec: [../spec.md](../spec.md)

**What to build:** a prefactor with no change in behaviour. Retention, Project deletion, the disk limit and the bucket TTL work over **families** of day-partitioned ClickHouse tables, where each family has its own Retention lookup. Logs, with their companion count tables and Log Retention, become the first family. The disk limit counts and moves every family's local days, oldest first, and the bucket TTL applies to each family's main table. Spans can then join as a second family with trace Retention (02).

**Blocked by:** None (can start immediately)

**Status:** resolved

- [x] The existing Log Retention, Project deletion, disk-limit and bucket tests pass unchanged
- [x] A stand-in second family with its own Retention keeps its own days, counts toward the disk limit, and is dropped with its Project (Go test)
