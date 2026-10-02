# M2-05: Histogram strip and summary line

**What to build:** above the stream, a member sees a slim histogram of Logs over time stacked by Level and a summary line, can zoom by dragging or clicking a bar, and can filter by clicking a per-Level count. Spec: [Logs explorer](../spec.md#logs-explorer-console-frontend) (Histogram, Summary line, Libraries); user stories 48–50.

**Blocked by:** M2-02, M2-04.

**Status:** ready-for-agent

- [ ] Console's EventStore client gains the histogram-and-summary operation.
- [ ] A slim strip (about 48 px of bars) stacked by Level, with time labels; bucket size follows the range. Drag across it or click a bar to zoom the time range (a run, so it pushes a history entry). Choose the histogram approach to fit the stack; note it in this ticket's Comments.
- [ ] Summary line: Log count, error rate = (error + fatal) ÷ Log count, and per-Level counts that add a Level chip when clicked. No p95.
- [ ] The histogram and summary use the same filters as the stream and refresh with each run.
- [ ] vitest: bucket size per range; drag/click → range; error rate.
