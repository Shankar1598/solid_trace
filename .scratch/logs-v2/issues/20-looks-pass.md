# 20: The looks pass (Phase 6 polish)

**What to do:** Spec Phase 6. The user asked for features first and looks later, and wants to iterate on looks and feel together. Turn the screens suite back on (`bun run e2e:screens`, both sizes, both themes), work through the Phase 6 list in [carry-over.md](../carry-over.md) and the rough edges in [research/v1-click-through.md](../research/v1-click-through.md), add skeleton loading states, check focus order, aria labels and chart colours, and measure 10,000 stream lines and a 12-chart Dashboard on a 7-day range (and decide compaction from that).

**Square corners everywhere** (user, 2026-10-17): drop rounded corners across the whole Console so it is consistent. Most of it is already square (`rounded-none` in `components/ui/*`), but the mode switch, chart switch, Builder and Query boxes, Dashboard and Monitor cards and others use `rounded-md` and friends. Likely one structural change: zero the `--radius*` tokens in `css/globals.css` (Tailwind's own `rounded`/`rounded-xs` too), then clear the arbitrary ones (`rounded-[2px]`, `rounded-[1px]`, `rounded-xl`, `rounded-lg`), the chart bars' `radius` in `QueryChart.tsx` and sonner's `--border-radius`. Decide with the user whether true circles (status dots, avatars) stay round.

**Long numbers in tables** (seen 2026-10-17): an avg shows every digit (`405.21802759325504`); round table numbers for display.

**Status:** ready-for-human
