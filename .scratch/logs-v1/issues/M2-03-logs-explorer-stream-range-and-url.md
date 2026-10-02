# M2-03: Logs explorer page with stream, range and URL state

**What to build:** an Organization member opens a Logs page for a Project, picks a time range, and scrolls a live, dense stream of that Project's Logs, with the state in the URL. Spec: [Console](../spec.md#console) (Logs explorer page, Proxying); [Logs explorer](../spec.md#logs-explorer-console-frontend) (Bar, Stream, Time, URL, Errors, Libraries); [Testing Decisions](../spec.md#testing-decisions) (Seams 2 and 3); user stories 39–43, 52, 55, 72, 73, 75. Prototype: branch `prototype/logs-explorer-layout` (variant D).

**Blocked by:** M2-01.

**Status:** ready-for-agent

- [ ] A Logs page per Project under the existing Organization and Project routes, behind the existing membership check, with a "Logs" entry next to Issues.
- [ ] Console's EventStore client gains the run, validate and fetch operations, mirroring the query endpoints.
- [ ] Bar: Project and time-range selects first. The mode switch and Builder controls come in later tickets.
- [ ] Stream: dense terminal-style lines, newest first: local time, `[service]`, Level, message, a few `key=value` Fields; errors and warnings get a coloured left edge. Loads 200, then 200 older per scroll, up to about 10,000, then "Narrow the time range to see older Logs". Virtualised and smooth at 10,000 lines. Choose the virtualised list to fit shadcn (base-ui), React 19, Inertia 2 and Tailwind; note the choice in this ticket's Comments.
- [ ] URL holds the time range as given (relative stays relative, absolute as UTC ISO), the mode and the Query text; each run pushes a history entry, typing doesn't; Back undoes the last run.
- [ ] Run errors replace the result area; a Cancel button shows while a Query runs.
- [ ] vitest is set up in Console (new). URL ⇄ explorer-state logic lives in plain modules, tested without rendering: relative and absolute ranges, mode and Query text, one history entry per run and none per keystroke.
- [ ] Console request tests (WebMock stubbing EventStore): only Organization members open the explorer; the Project and time range are passed through to EventStore.
- [ ] `CONTEXT.md` gains **Logs explorer**: the Console page where a Project's Logs are searched and read.
