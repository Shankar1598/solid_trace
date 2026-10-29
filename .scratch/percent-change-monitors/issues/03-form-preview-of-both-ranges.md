# 03: Form preview of both ranges

**What to build:** in change mode, the Monitor form's preview charts the Query over the last 24 hours twice: as it is now, and shifted back by the Baseline's offset. Both go on one chart, with no threshold line, so a member sees how the two usually compare. Under it, the form lists the groups that would fire now, with value, Baseline and change. Then the member can tune the threshold before saving. Spec: [spec.md](../spec.md), user stories 35, 36.

**Blocked by:** 01 (A change Monitor against the range just before)

**Status:** resolved

- [x] In change mode the preview shows two series, Now and Baseline, labelled as such, with the Baseline series shifted onto the same time axis. It has no threshold line. Value mode's preview is unchanged.
- [x] The would-fire list uses the same change rule as a run (direction, threshold, zero Baseline, missing rows and the minimum, if 02 has landed), from the same two-range read a run makes.
- [x] The preview follows edits to the Baseline, direction, threshold and minimum without saving.
- [x] Whichever Baseline the form offers works: the range just before, and 1 day and 1 week once 02 has landed.
- [x] A preview read that fails, times out or is cut says so in place of the chart or list, as value mode's preview does.
- [x] Tests:
  - the Monitors Playwright flow: in change mode, see both series and the would-fire list, change the threshold, see the list change;
  - controller or vitest tests for the would-fire list's rule, matching the run's (the same cases as 01's run tests, at the preview's seam).

**Notes (implementation):** the would-fire list comes from `POST /:org/monitors/preview_groups`, which builds the unsaved Monitor from the form's settings and calls `MonitorEvaluation.preview`: the run's own two-range read (`read_threshold`) and firing rule (`fires?`, i.e. `LogMonitor#breached?`), with no lease and no writes. Its tests are in `test/controllers/monitors_controller_test.rb` (the 01 run cases, at that seam). The chart's Baseline offset comes from `baselineOffsetMinutes` in `lib/monitors/monitor.ts`, a `Record<MonitorBaseline, …>`, so adding a Baseline to the type fails typecheck until its offset is given. When 02's minimum lands, it needs adding to the preview's `fetchGroupsPreview` settings and the groups effect's dependencies in `MonitorForm.tsx` (the server side already takes whatever `monitor_params` permits).
