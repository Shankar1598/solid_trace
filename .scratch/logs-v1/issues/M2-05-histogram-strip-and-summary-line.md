# M2-05: Histogram strip and summary line

**What to build:** above the stream, a member sees a slim histogram of Logs over time stacked by Level and a summary line, can zoom by dragging or clicking a bar, and can filter by clicking a per-Level count. Spec: [Logs explorer](../spec.md#logs-explorer-console-frontend) (Histogram, Summary line, Libraries); user stories 48–50.

**Blocked by:** M2-02, M2-04.

**Status:** resolved

- [x] Console's EventStore client gains the histogram-and-summary operation.
- [x] A slim strip (about 48 px of bars) stacked by Level, with time labels; bucket size follows the range. Drag across it or click a bar to zoom the time range (a run, so it pushes a history entry). Choose the histogram approach to fit the stack; note it in this ticket's Comments.
- [x] Summary line: Log count, error rate = (error + fatal) ÷ Log count, and per-Level counts that add a Level chip when clicked. No p95.
- [x] The histogram and summary use the same filters as the stream and refresh with each run.
- [x] vitest: bucket size per range; drag/click → range; error rate.

## Comments

### 2026-10-03 built (logs-v1/m2-05)

What was built:
- **EventStore client:** `EventStore.logs_histogram(project_id:, query:, from:, to:, bucket_seconds: nil)` POSTs to EventStore's `/api/:project_id/logs/histogram` and answers its body as is. A 400 raises `Rejected`, other failures `Unavailable`, as for a run.
- **Console route:** `GET /:org_slug/projects/:project_slug/logs/histogram?query=&from=&to=&bucket_seconds=` (`histogram_project_logs_path`), behind the membership check. It forwards `from`/`to` in UTC. A range or `bucket_seconds` it can't read gets 400 `{error}` without calling EventStore. A Query with mistakes gets 400 `{errors}` (EventStore's body), and EventStore down gets 502 `{error}`. `bucket_seconds` is optional; the page always sends it.
- **Plain module** `app/frontend/lib/logs/histogram.ts` (vitest, `histogram.test.ts`):
  - Types `Histogram`, `HistogramBucket`, `LevelCounts` and `HISTOGRAM_LEVELS` (the six Levels, then `none`).
  - `bucketSeconds(from, to)` is the smallest step on a 1 s … 7 d ladder that gives at most 100 bars. For example: 1 h → 1 min, 6 h → 5 min, 24 h → 15 min, 7 d → 2 h, 30 d → 12 h.
  - `zoomRange(histogram, range, a, b)` turns bars `a`…`b` (a click is `a = b`) into an absolute range, cut to the run's range. EventStore's end buckets are epoch-aligned and reach past it, so a zoom never widens.
  - `barAt(x, width, bars)` gives the bar under the pointer, clamped to the end bars when a drag goes past the edge.
  - `errorRate({count, levels})` is `(error + fatal) / count`, or null with no Logs.
  - `filterByLevel(builder, level)` is `showOnly(builder, 'level', level)`, so clicking another Level replaces the first. `none` gives null.
- **API:** `fetchHistogram({…, bucketSeconds, signal})` in `api.ts`. It shares `runFailure` with `fetchLogPage`, so a Query's mistake reads the same in both.
- **Component** `pages/Logs/components/HistogramStrip.tsx`, between the bar and the result area in `Logs/Index.tsx`.
  - **Summary line:** the Log count, the error rate, and the non-zero per-Level counts. The counts are buttons only when the Builder can take a chip; `no Level` never is.
  - **Strip:** 48 px of flex bars, stacked fatal at the bottom up to `none`. Each bar's hover title gives its time and per-Level counts.
  - **Time labels:** local time, with the range's start and end at the edges and three bar starts between.
  - **Zooming:** a pointer drag (with pointer capture) highlights the bars it covers. Releasing it, or a click, calls `commit(pickRange(explorer, zoom))`, which is a run, so it pushes a history entry.
- **Each run refreshes it:** `start` fetches the histogram beside the first page, with the same Query, the same resolved `from`/`to` and the same abort signal. Cancel marks a counting histogram "Not counted". A failed count shows its message in the summary line, and the strip collapses.

Choices the ticket left open:
- **Histogram approach: no library.** It is plain flex `div`s with Tailwind colours and pointer events. A chart library (Recharts, visx, uPlot) would add weight and styling of its own for one fixed-height stacked bar strip with at most about 100 bars. It would also still need custom drag-to-zoom.
- **Bucket size is chosen in the browser** (`bucketSeconds`) and sent as `bucket_seconds`. The browser needs the size anyway, to size the bars and turn bars into ranges. Its ladder is finer than EventStore's `bin_auto` (it adds 2 s, 15 s, 2 min, 15 min, 2 h and 2 d), so ranges of 2 h or 24 h don't drop to 24 or 48 bars.
- **Per-Level click** replaces any Level chip the Builder shows only (`showOnly` semantics, from M2-04). The click runs at once, through `changeBuilder`, so it pushes a history entry. While validate is reading the Query, or when the Builder can't show the Query, the counts are plain text.
- **Zoom when only a search was typed:** the zoom runs the draft, as the range select does, so typed search text runs along with it.

Deviations: none.

Not verified in a browser: this box has no browser. tsc passes, and a throwaway server render of the strip (done, running, error, single bucket) ran without errors. Dragging wasn't exercised.

Follow-ups:
- **M3-02:** the strip shows in Query mode too, but the Level counts are plain text there, because `onLevel` is null without a Builder. Pivots in Query mode can add a `where level == "…"` stage the same way. Over a table result, the histogram counts the Query's head as M2-02 does, so no change is needed.
- **M3-04:** hide `HistogramStrip` and skip `fetchHistogram` in SQL mode.
