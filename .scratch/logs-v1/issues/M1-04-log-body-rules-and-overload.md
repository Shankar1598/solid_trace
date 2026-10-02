# M1-04: Log body rules and overload

**What to build:** shippers can send compressed and large batches to the Log routes, and every overloaded ingest tells the sender when to retry. Spec: [Log ingest](../spec.md#log-ingest-eventstore) (Body, Overload); user stories 8, 9, 13.

**Blocked by:** M1-01.

**Status:** ready-for-agent

- [ ] Log routes accept `gzip`, `zstd` or identity bodies and decompress in EventStore, not through Fiber's pass-through.
- [ ] Over 10 MB compressed or 50 MB decompressed, the route answers `413` and stores nothing; decompression stops at the cap instead of inflating the whole body.
- [ ] Body limits are raised for Log routes only, in EventStore's server; Event routes keep today's limits. (Angie is removed by [M1-11](M1-11-remove-angie.md), so there is no proxy limit to raise.)
- [ ] Every ingest `503` (overload or shutting down), Event routes included, carries a `Retry-After` of a few seconds. No `429`.
- [ ] Tests: gzip and zstd bodies stored; `413` on each cap with nothing stored; `Retry-After` on an Event `503` and a Log `503`.
