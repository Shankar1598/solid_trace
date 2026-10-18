# Dashboard and stream measurements (ticket 20)

2026-10-18, on `main` (`d2bd874`). Ticket 20's last two measurements: a 12-chart Dashboard over 7 days, cold and warm, and 10,000 stream lines staying smooth. They also re-check the compaction call (carry-over, "Compaction, measured again"). Run on the 4-CPU, 11 GB dev box, against the dev stack, with local disk only. DuckDB is 1.5.6.

## Answer

- **The 12-chart Dashboard:**
  - Its Queries take 5.6 s in all cold, and 3.6–4.2 s warm, run 4 at a time as the browser runs them.
  - In the browser, on a production build of the frontend, the page is drawn in 5.0 s cold and 2.7–3.0 s warm.
  - Charts binned by time come from the bin cache once they've run: 0.7–1.8 s cold, 65–250 ms warm.
  - A stat or table chart isn't binned, so it runs in full every time. Users (`dcount(user.id)`) takes 3.5–4.0 s with three other charts running, and Top paths 2.3 s. Those two set the warm figure.
- **10,000 stream lines stay smooth on a production build:**
  - Wheel scrolling at 10,000 lines holds 60 frames a second (p95 17 ms, max 33 ms), with no long tasks.
  - The DOM stays at about 2,350 nodes from 200 lines to 10,000, as the stream is virtualised.
  - The JS heap grows from 12 MB to 23 MB.
  - Each older page of 200 takes about 195 ms, the same at 10,000 lines as at 1,000.
- **No daily compaction still.** Merged into one file per day, the same Logs answer `count()` faster (48 ms to 5 ms). Every other chart takes the same time, as its cost is reading the data, not opening files. What compaction would save on a bucket (requests per file) is still for the real GCS run, [21](../issues/21-real-gcs-check.md).
- **The development build is no guide to smoothness.** Its scrolling is janky at any stream length (median frame 67 ms), and that comes from React's development-only work. Judge looks-and-feel speed on a production build.

## Data

- **Dummy Logs:** `bin/rails dev:logs COUNT=100000 HOURS=168 SEED=<701–720>`, 20 runs, about 9 s each, on `solid-trace-project`. With the Project's earlier Logs, that is 2.02 million over 7 days, about 12,000 an hour.
- **Exported:** EventStore was restarted, so its start-up export moved them from `logs_late` into hour files. That gave 192 files over 8 day folders: 131 MB, 198 row groups and about 2 million rows.
- **Cold:** EventStore restarted and idle, so its bin cache and DuckDB's caches are empty. The OS page cache stays warm, because dropping it needs root. So "cold" here is a cold process, not a cold disk.

## 1. The 12-chart Dashboard

Dashboard 6, "Perf: 12 charts over 7 days", with a default range of `7d`. The charts and their Queries are in `~/.cache/solid-trace/perf-20/make_dashboard.rb`.

### Straight at EventStore

`EventStore.query_logs` with each chart's Query, 4 at a time as the browser runs them, over a fixed 7-day range. The client is `~/.cache/solid-trace/perf-20/dashboard_runs.rb`. Times are each chart's wall time, in ms; EventStore's `elapsed_ms` is within 10 ms of each.

| Chart | Query | Cold | Warm | Warm again |
|---|---|---|---|---|
| Logs by level (bar) | `count() by bin_auto(time), level` | 750 | 109 | 176 |
| Logs by service (area) | `count() by bin_auto(time), service` | 963 | 71 | 111 |
| Errors by service | `where level in (…) \| count() by bin_auto(time), service` | 677 | 67 | 111 |
| 5xx responses | `countif(toint(http.status) >= 500) by bin_auto(time)` | 1,268 | 117 | 147 |
| p95 duration by service | `percentile(duration_ms, 95) by bin_auto(time), service` | 1,334 | 97 | 66 |
| Mean duration | `avg(duration_ms) by bin_auto(time)` | 1,205 | 80 | 84 |
| Logs (stat) | `count()` | 922 | 674 | 760 |
| Errors (stat) | `where level == "error" \| count()` | 646 | 427 | 1,054 |
| Users (stat) | `dcount(user.id)` | 3,754 | 3,493 | 3,963 |
| Top paths (table) | `count() by http.path \| top 10 by count_` | 2,548 | 2,317 | 2,371 |
| Jobs by name (bar) | `count() by bin_auto(time), job.name` | 1,729 | 109 | 247 |
| Render time by page | `avg(render_ms) by bin_auto(time), page` | 1,777 | 127 | 179 |
| **All 12** | | **5,642** | **3,648** | **4,192** |

- **The bin cache works:** each binned chart is 7–20 times faster warm.
- **Stat and table charts have nothing to cache.** The bin cache holds closed bins, and these charts have no bins. So their warm run costs what their cold one did.
- **The charts share the CPU.** DuckDB already runs one Query on every core, so 4 at once share 4 cores:
  - Run alone, Users takes 1.7 s the first time and 0.6 s after.
  - Run alone, `count()` takes 0.2 s.
  - Run one at a time, as the dev Console serves them (below), the 12 took 2.7 s warm in all, less than 4 at a time.
- **Attributes cost the most.** A Query reads an Attribute from four typed maps (`attrs_string['user.id']` and the other three), so it reads every Log's whole map. Built-in fields such as `level` and `service` are columns.

### In the browser

`console/tmp/perf20/perf.spec.ts`, a throwaway Playwright spec (a copy is in `~/.cache/solid-trace/perf-20/`). It opens the Dashboard and waits until all 12 charts have run, then reloads and times it again. Every chart answered 200; none was refused as busy.

| | Production build | Development build |
|---|---|---|
| After an EventStore restart | 5.0 s | |
| Warm | 2.7–3.0 s | 4.8–5.5 s |
| Long tasks while drawing | 10–12, 1.5–2.0 s in all | 8, 3.4–3.5 s in all |
| Longest frame | 200–350 ms | 730–750 ms |

- **Development Console runs one Puma thread** (`config/puma.rb`), so it serves the charts one at a time. Production has 10 threads, and the browser runs 4 at once. A production page is nearer the 4-at-a-time figures above, plus drawing.
- **Each chart takes a long task of about 150 ms to draw** on the production build, mostly Recharts building its elements. The page answers between them, but scrolling stutters while charts arrive.

## 2. Daily compaction, locally

`event_store/bench/objectstore/compaction_probe_test.go`, which runs when `COMPACTION_PROBE_DIR` names a Project's export folder. It merges the hour files into one file per day, as spec step 7 would: sorted by time, zstd, with row groups of 122,880. It took 4.4 s. Then it times each kind of chart's SQL over both layouts, on a fresh DuckDB (cold) and then again (warm). The Queries are `read_parquet` over the files, without EventStore's view.

| Query | Hour files, cold / warm | Day files, cold / warm |
|---|---|---|
| `count()` | 48 / 39 ms | 5 / 4 ms |
| `count() by bin(1h), level` | 115 / 95 ms | 64 / 80 ms |
| `percentile(duration_ms, 95) by bin(1h), service` | 185 / 168 ms | 175 / 170 ms |
| `dcount(user.id)` | 422 / 399 ms | 453 / 346 ms |
| top 10 `http.path` | 378 / 343 ms | 356 / 329 ms |

The hour files are 192 files, 198 row groups and 131 MB. The day files are 8 files, 20 row groups and 125 MB.

- **Only `count()` gains,** because it reads row counts from each file's footer.
- **Nothing else changes.** The slow charts are slow because they read every Log's Attribute maps, and compaction keeps the same bytes.
- **EventStore adds a little over raw DuckDB:** `count()` takes 0.2 s through EventStore and 48 ms raw, and Users 0.6 s against 0.4 s. That is the `logs` view, which also reads the hot tables, plus the run around it.
- **The call stays "not now".** Compaction would only help on a bucket, where each file costs requests. The [object store probe](object-store-probe.md) put a cold 7-day chart at 1.9 s with 30 ms a request. Run this Dashboard there in 21.

## 3. 10,000 stream lines

The same spec opens the explorer at `?range=24h` (about 290,000 Logs) and scrolls to the end until the stream stops at 10,000. It then wheel-scrolls 60 ticks of 600 px, jumps from top to bottom 6 times, and opens the inspector. Frame gaps come from `requestAnimationFrame`, long tasks from a `longtask` observer, and heap and DOM size from Chrome's `Performance.getMetrics` after a garbage collection. The browser is headless Chromium without a GPU.

| | Production build | Development build |
|---|---|---|
| 200 → 10,000 lines (49 older pages) | 9.6 s | 27.1 s |
| An older page's request, median (EventStore) | 55 ms (32 ms) | 85 ms (54 ms) |
| Frames while loading: p95 / max | 33 / 67 ms | 183 / 267 ms |
| Long tasks while loading | 11, 0.7 s | 153, 16.3 s |
| JS heap at 200 / 10,000 lines | 12.4 / 22.7 MB | 34.6 / 40.6 MB |
| DOM nodes at 200 / 10,000 lines | 2,443 / 2,356 | 2,453 / 2,366 |
| Rows drawn at 10,000 | 48 | 48 |
| Wheel scrolling: frame p50 / p95 / max | 17 / 17 / 33 ms | 67 / 217 / 333 ms |
| Long tasks while wheel scrolling | 0 | 65, 7.7 s |
| Jumping top to bottom: longest frame | 50 ms | 200 ms |
| The inspector opens | 169 ms | 662 ms |

- **The number of lines doesn't matter.** On the development build, wheel scrolling at 1,000 lines was as slow as at 10,000 (12.2 s and 8.9 s of long tasks for the same 60 ticks). Older pages took the same time throughout.
- **Why the development build stutters:** its profile is mostly React's development-only work (`jsxDEV`, `validateProperty`, `runWithFiberInDEV`, `logComponentRender`). There is also one real cost: `LogRow` isn't memoised, so every scroll re-renders every drawn row, about 110 with the overscan.
  - Memoising it, with `widthOf` and `moveFocus` in `useCallback`, halved the development build's long tasks while wheel scrolling (8.9 s to 4.2 s). The patch is `~/.cache/solid-trace/perf-20/memo-logrow.patch`. It isn't applied.
  - The production build is smooth without it.
- **`HistogramStrip`'s bars re-render on every older page,** though the histogram doesn't change: about 100 ms a page in the development profile. That's small.
- **Measuring it:** a role locator polled from the test runs Playwright's selector engine on the page's thread. The spec reads `aria-rowcount` with a plain `querySelector` instead.

### The production build

Vite's dev server was stopped. Then `VITE_RUBY_PUBLIC_OUTPUT_DIR=vite-dev bin/vite build --mode production` built the frontend into `public/vite-dev`, and Console was restarted with `VITE_RUBY_AUTO_BUILD=false`. The development Console then serves the production bundle. Afterwards `public/vite-dev` was deleted, and Console and the dev server were restarted as before.

## Follow-ups (not tickets yet)

- **Stat and table charts from the bin cache.** `count`, `countif`, `sum` and a count by group add up across bins, so a stat or table chart could be summed from cached closed bins instead of run in full. `dcount` and `percentile` can't, unless the cache keeps sketches. This would take the warm Dashboard from about 3.6 s to about the Users chart alone.
- **Drawing 12 charts takes 1.5–2 s of main thread** on the production build, in long tasks of about 150 ms each. That's acceptable now.
- **`LogRow` memo:** optional. The patch is above.
