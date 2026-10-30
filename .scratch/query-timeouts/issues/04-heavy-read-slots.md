# 04: One or two heavy reads at a time

**What to build:** EventStore runs at most one or two heavy Log reads at once, across the whole instance. A heavy read is a text-match read that 02's estimate puts over a share of the bytes budget (for example a quarter), or any forced read. Other reads are never heavy, for now (02's scope change, 2026-10-30). A heavy read past the cap is refused at once, not queued, with: "Another large Query is running. Run them one at a time." Small reads skip the cap. See the [spec](../spec.md)'s Implementation Decisions. It replaces nothing: Console's `ChartRuns` cap still guards Puma threads.

**Blocked by:** 02

**Status:** resolved

- [x] The cap lives in EventStore, beside ClickHouse, and counts the whole instance.
- [x] The slot count is configurable, defaulting to 1 on a 2-vCPU install. Pick a default from the CPU count, and record why.
- [x] A heavy read past the cap is refused at once. MCP returns the refusal as the tool's text; the explorer shows it.
- [x] A heavy read releases its slot on every exit: success, error, timeout, client gone.
- [x] Small reads, Dashboard Charts' among them, never wait on or count toward the cap.
- [x] Test: two heavy reads at once, where the second is refused while the first runs and accepted after it ends.
- [x] The refusal says "run them one at a time", not "try again later", so agents serialize instead of retrying in a loop.

## Comments

**2026-10-30, built by the solid-trace-1c session on `query-timeouts/04-heavy-read-slots` (`22486aa8`), merged into `feature/query-timeouts` (`43da9959`).**
- **The cap:** `handler.HeavyReads` (`handler/logs_heavy_reads.go`). A checked or forced run whose estimate is heavy takes a slot right after the size check, in `Query` (first pages only) and `Histogram`. It's released in `run.end()`, so on every exit.
- **Slots:** keyed by Project, Query text and the estimate's range, so the explorer's run and its histogram share one.
- **Past the cap:** HTTP 200 `{"refusal": {"message": "Another large Query is running. Run them one at a time.", "busy": true, "estimate", "largest_range": null, "top_files": [], "narrower_query": null}}`, logged as `kind=log_read_busy`. 02's refusals gain `busy: false`.
- **Config:** `logs_heavy_read_slots`, default `min(2, max(1, cores/4))`, so 1 on production's 2 vCPUs. Documented in `deploy/event_store.yml`. 0 means no cap; a negative value fails.
- **Console:** unchanged, since MCP's refusal text already prints the message.
- **Tests:** the config and handler packages, and new root tests that hold a slot:
  - checked, forced and histogram runs are refused as busy, and go through once the slot is released;
  - a histogram beside its own run goes through, while another range is refused;
  - small, Chart and non-text reads run while a slot is held.

  Also 02's budget tests and the root package with `-run 'Histogram|Run|Query|Timeout|Cursor|Page'`.
