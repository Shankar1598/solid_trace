# 20: The looks pass (Phase 6 polish)

**What to do:** Spec Phase 6. The user asked for features first and looks later, and wants to iterate on looks and feel together. Turn the screens suite back on (`bun run e2e:screens`, both sizes, both themes), work through the Phase 6 list in [carry-over.md](../carry-over.md) and the rough edges in [research/v1-click-through.md](../research/v1-click-through.md), add skeleton loading states, check focus order, aria labels and chart colours, and measure 10,000 stream lines and a 12-chart Dashboard on a 7-day range (and decide compaction from that).

**Square corners everywhere** (user, 2026-10-17): drop rounded corners across the whole Console so it is consistent. Most of it is already square (`rounded-none` in `components/ui/*`), but the mode switch, chart switch, Builder and Query boxes, Dashboard and Monitor cards and others use `rounded-md` and friends. Likely one structural change: zero the `--radius*` tokens in `css/globals.css` (Tailwind's own `rounded`/`rounded-xs` too), then clear the arbitrary ones (`rounded-[2px]`, `rounded-[1px]`, `rounded-xl`, `rounded-lg`), the chart bars' `radius` in `QueryChart.tsx` and sonner's `--border-radius`. Decide with the user whether true circles (status dots, avatars) stay round.

**Long numbers in tables** (seen 2026-10-17): an avg shows every digit (`405.21802759325504`); round table numbers for display.

**Scope and direction** (user, 2026-10-17): the looks pass covers the whole Console, not only Logs. Circles (status dots, avatars) stay round. The Console mixes Sentry, Axiom and Better Stack, whose style languages differ; the goal is one consistent design language, chosen before any page is polished. A tour of every page (`console/e2e/screens/tour.spec.ts`, set `tour-before`) found: one monospace face for everything, four page-header patterns, three surface treatments, two brand marks, mixed corners and case, a stray violet accent, loose form spacing. Three directions (A all-mono, B sans for the interface and mono for the data, C all-sans) are compared at https://claude.ai/artifact/K6onrwpFyjBpuhWtW599HH; The user picked **B** (2026-10-17): IBM Plex Sans for the interface (titles, nav, buttons, labels, prose), JetBrains Mono for data (timestamps, values, log messages, queries, IDs, DSNs), plus the shared rules on that page (square corners, neutral palette with semantic colour only, one page header, outlined panels, sentence case, rounded tabular numbers). Every page is reviewed with seeded data before polishing.

**Progress:** the [review](../research/looks-review.md) lists the work in three steps. Step 1, the foundation, is done (`b822f9d`, 2026-10-17): fonts, square corners, one neutral mark, a dark sign-in panel, and rounded table numbers. All 69 flows pass. Before and after screenshots are on the direction page. Step 2, the shared components, is done (2026-10-17):
- one `PageHeader` with breadcrumbs, used on every signed-in page;
- one table style, used by every index;
- a `Badge` with neutral, info, warn, error, ok and count tones, and a dot for status;
- a `caps-label` section label;
- outlined panels;
- a `Segmented` switch for Mode, Chart, Viz and Kind;
- a sentence-case sweep.

All 69 flows pass. The direction page shows each page before, after step 1 and after step 2.

Step 3, page by page at desktop size, is done (2026-10-18):
- **Logs:**
  - plain service names, a message column that fills the width, and a time column clear of its menu button
  - skeleton rows and bars while a run loads
  - one prompt style for leaving Query or SQL mode
  - mono ligatures off
- **Dashboards:**
  - a chart names its Project only on a Dashboard that reads several
  - value axes carry the chart's unit, a stat drops its column name, and the run time shows once
  - time labels stay inside the chart
- **Monitors:** the form in `FormSection`s at `max-w-4xl`.
- **Issues:** the comment box framed once, with its placeholder, and without taking focus on open.
- **Projects:** one column with the DSN first.
- **Log in and Register:** no card, and a page-size title.
- **Access:**
  - every page names its browser tab
  - the Logs stream is a keyboard grid (new flow in `shortcuts.spec.ts`)
  - icon buttons have labels

The language is written down in [docs/design-language.md](../../../docs/design-language.md), and the direction page shows four stages for 14 pages. 70 flows pass.

Phones are a separate effort, [phone-layout 01](../../phone-layout/issues/01-phone-layout.md) (user, 2026-10-18).

Left in this ticket:
- the bugs in the review (after the looks work, as the user asked)
- the Phase 6 carry-over list and the remaining click-through rough edges

**Status:** ready-for-human

## Comments

### 2026-10-18 measurements

Both measurements are done, on `main` with about 2 million dummy Logs over 7 days. The details are in [research/dashboard-and-stream.md](../research/dashboard-and-stream.md).

- **10,000 stream lines stay smooth** on a production build of the frontend. Wheel scrolling at 10,000 lines holds 60 frames a second with no long tasks. The DOM stays at about 2,350 nodes, and the JS heap grows from 12 MB to 23 MB. The development build stutters at any length; that comes from React's development-only work.
- **The 12-chart Dashboard over 7 days:**
  - Its Queries take 5.6 s cold and 3.6–4.2 s warm, 4 at a time.
  - In the browser it takes 5.0 s cold and 2.7–3.0 s warm.
  - Binned charts come from the bin cache once they've run. The stat and table charts run in full every time, and `dcount(user.id)` (3.5 s with three others running) sets the warm figure.
- **Compaction stays "not now".** One file per day speeds up only `count()` locally. The bucket side is for 21, with this Dashboard (Dashboard 6 in dev).

Not done, for the user to decide:
- serving stat and table charts from the bin cache when their aggregates add up
- memoising `LogRow`: the patch is in `~/.cache/solid-trace/perf-20/`, and it only helps the development build

