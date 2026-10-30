# 03: The Logs explorer shows the refusal, with Run anyway

**What to build:** when EventStore refuses an explorer Query for its size (02), the Logs explorer shows the refusal in place of results:

- the estimate and the budget;
- the largest range that fits, as a button that sets it;
- the busiest files, each as a button that adds the `file` filter;
- a Run anyway button that runs the same Query with `force`.

See the [spec](../spec.md).

**Blocked by:** 02

**Status:** resolved

- [x] A refused Query shows the refusal, not an error.
- [x] The range button sets the range and runs again.
- [x] A file button adds `file == "<file>"` to the Query (as a chip, when the Builder can show it) and runs again.
- [x] Run anyway runs the Query with `force`. If the forced run still times out, the explorer shows the timeout message (01).
- [x] The histogram follows the Query: refused when the Query is, and run with `force` when it is.
- [x] A Playwright flow covers a refusal, its file button, and Run anyway (`console/e2e`).

## Comments

**2026-10-30, built on `query-timeouts/03-refusal-in-the-explorer`, merged into `feature/query-timeouts` (`c8533689`).**
- **The answer:** `fetchRun` and `fetchHistogram` return a refusal as its own kind of answer.
- **The panel:** `RefusalNotice` shows the message's first line, the estimate against the budget as a bar, a largest-range button, buttons for the busiest files, and Run anyway.
  - A file listed as "no file" is shown but isn't a button, since `file == ""` would miss those Logs.
  - The narrower Query isn't shown separately, because the buttons cover it.
- **A busy refusal (04)** shows the message only. A missing `busy` counts as false.
- **The histogram strip** says "Not counted: the Query wasn't run."
- **Run anyway** forces the run, its histogram and its older pages. Auto-refresh pauses on a refused run and after Run anyway, so a forced read doesn't repeat.
- **Tests:**
  - vitest `app/frontend/lib/logs` (242 passed, including the new `api.test.ts` and `refusal.test.ts`) and `bun run typecheck`;
  - the flow `console/e2e/flows/logs-refusal.spec.ts`, 3/3, before and after merging 04. Dev data is too small to be refused, so the flow stubs the refusal for unforced searches over 3 h with no file filter; the file, range and forced runs reach the real stack.
- **An older failure, not from this branch:** `logs-v1-builder.spec.ts:43` fails because `getByText('billing').first()` finds a hidden SVG `<title>` in the bar chart, which has been on main since b23c1d35.
- **Screenshots:** `~/.cache/solid-trace/screenshots/query-timeouts-03/` (refusal and busy, desktop dark). Both render.
