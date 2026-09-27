# 14: Match Monitors

**What to build:** A Monitor kind that notifies for each Log matching a filter-only Query since the last evaluation, capped at 10 notifications per minute and 500 per day, and refuses creation when the filter matched more than 500 Logs in the last 24 hours.

**Blocked by:** 13

**Status:** wontfix

- [ ] Match Monitors accept only filter/extend/project/parse operators (validated through the validate endpoint)
- [ ] Evaluation uses the time-ordered id cursor to avoid duplicates across runs
- [ ] Caps enforced and surfaced in the UI; creation guard implemented
- [ ] Job and controller tests

## Comments

2026-09-27: Closed as wontfix. This plan was superseded by the [logs map](../../logs/map.md). The file stays here as reference only.
