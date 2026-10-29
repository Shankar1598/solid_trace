# 02: 1-day and 1-week Baselines, and a minimum

**What to build:** a member can compare with the same range 1 day or 1 week earlier, so daily and weekly cycles don't fire the Monitor. They can also set a minimum, so small numbers stay quiet. Saving refuses Baselines that would overlap the current range or that reach past the Project's Retention, with the reason shown on the form and over MCP. A run fails with an error, and doesn't fire, when Retention has since been lowered below what it needs. Spec: [spec.md](../spec.md), user stories 5, 6, 11, 12, 17, 22–24, 37, 44.

**Blocked by:** 01 (A change Monitor against the range just before)

**Status:** resolved

- [x] Schema: Monitors gain `minimum` (nullable).
- [x] Saving accepts `baseline` = `day` or `week`. The Baseline range is the current range shifted back exactly 1 day or 1 week.
- [x] Saving refuses `day` with a range over 1 day. It also refuses range + offset longer than the Project's Log Retention (the Project's own, else the instance default), saying which and why.
- [x] A run whose Baseline reaches past the Project's current Retention is an error run that says so, changes no state and sends nothing.
- [x] Minimum: a group is compared only when `max(|value|, |baseline|) >= minimum`, and is ok otherwise. A drop from 1,000 to 0 with a minimum of 10 still fires a "falls" Monitor. No minimum compares every group. Changing the Baseline or the minimum resolves nothing.
- [x] Monitor form: Baseline offers the range just before, 1 day earlier and 1 week earlier, plus an optional minimum. Saving's refusals show beside the fields they concern.
- [x] Messages and the summary sentence name the Baseline ("from 60 1 day earlier", "…from the same 15 minutes 1 week earlier").
- [x] MCP: create, update and get take and return `minimum` and the new Baselines. A refused save is a tool error with the reason.
- [x] The Monitors doc gains the Baseline choices, the minimum and the two refusals.
- [x] Tests at the agreed seams:
  - Monitor runs: the exact Baseline range for day and week, the minimum cases, the Retention error;
  - saving over MCP and the controller: both refusals, the minimum round trip;
  - the Monitors Playwright flow: a refused save (1 day with a 2-day range), then a saved "falls by 50% from 1 day earlier, minimum 10";
  - message builder wording for day and week.
