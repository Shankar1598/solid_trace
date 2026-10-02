# M1-06: Log processing errors and backlog

**What to build:** Log processing survives bad Logs and store failures as Event processing does, and the operator can see when it is behind. Spec: [Log processing](../spec.md#log-processing-eventstore) (Errors); user stories 35, 38.

**Blocked by:** M1-01.

**Status:** ready-for-agent

- [ ] A store error (DuckDB unavailable, disk full) retries with backoff and skips nothing; the cursor doesn't move.
- [ ] A batch that fails for its content is retried, then isolated Log by Log; a Log that can never be appended is skipped and logged, and the rest of the batch is stored. Classify errors the way Event processing does (ingest-write-path 04).
- [ ] A periodic log line names Projects whose oldest unprocessed Log batch is older than a threshold.
- [ ] A stuck or failing Log Project doesn't stall other Projects' Logs or Event processing.
- [ ] Tests: a bad Log in a batch is skipped and its neighbours are queryable; a store failure followed by recovery loses nothing; the backlog line appears for a lagging Project.
