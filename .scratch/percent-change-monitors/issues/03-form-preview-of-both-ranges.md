# 03: Form preview of both ranges

**What to build:** in change mode, the Monitor form's preview charts the Query over the last 24 hours twice: as it is now, and shifted back by the Baseline's offset. Both go on one chart, with no threshold line, so a member sees how the two usually compare. Under it, the form lists the groups that would fire now, with value, Baseline and change. Then the member can tune the threshold before saving. Spec: [spec.md](../spec.md), user stories 35, 36.

**Blocked by:** 01 (A change Monitor against the range just before)

**Status:** ready-for-agent

- [ ] In change mode the preview shows two series, Now and Baseline, labelled as such, with the Baseline series shifted onto the same time axis. It has no threshold line. Value mode's preview is unchanged.
- [ ] The would-fire list uses the same change rule as a run (direction, threshold, zero Baseline, missing rows and the minimum, if 02 has landed), from the same two-range read a run makes.
- [ ] The preview follows edits to the Baseline, direction, threshold and minimum without saving.
- [ ] Whichever Baseline the form offers works: the range just before, and 1 day and 1 week once 02 has landed.
- [ ] A preview read that fails, times out or is cut says so in place of the chart or list, as value mode's preview does.
- [ ] Tests:
  - the Monitors Playwright flow: in change mode, see both series and the would-fire list, change the threshold, see the list change;
  - controller or vitest tests for the would-fire list's rule, matching the run's (the same cases as 01's run tests, at the preview's seam).
