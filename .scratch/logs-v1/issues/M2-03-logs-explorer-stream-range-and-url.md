# M2-03: Logs explorer page with stream, range and URL state

**What to build:** an Organization member opens a Logs page for a Project, picks a time range, and scrolls a live, dense stream of that Project's Logs, with the state in the URL. Spec: [Console](../spec.md#console) (Logs explorer page, Proxying); [Logs explorer](../spec.md#logs-explorer-console-frontend) (Bar, Stream, Time, URL, Errors, Libraries); [Testing Decisions](../spec.md#testing-decisions) (Seams 2 and 3); user stories 39–43, 52, 55, 72, 73, 75. Prototype: branch `prototype/logs-explorer-layout` (variant D).

**Blocked by:** M2-01.

**Status:** resolved

- [x] A Logs page per Project under the existing Organization and Project routes, behind the existing membership check, with a "Logs" entry next to Issues.
- [x] Console's EventStore client gains the run, validate and fetch operations, mirroring the query endpoints.
- [x] Bar: Project and time-range selects first. The mode switch and Builder controls come in later tickets.
- [x] Stream: dense terminal-style lines, newest first: local time, `[service]`, Level, message, a few `key=value` Fields; errors and warnings get a coloured left edge. Loads 200, then 200 older per scroll, up to about 10,000, then "Narrow the time range to see older Logs". Virtualised and smooth at 10,000 lines. Choose the virtualised list to fit shadcn (base-ui), React 19, Inertia 2 and Tailwind; note the choice in this ticket's Comments.
- [x] URL holds the time range as given (relative stays relative, absolute as UTC ISO), the mode and the Query text; each run pushes a history entry, typing doesn't; Back undoes the last run.
- [x] Run errors replace the result area; a Cancel button shows while a Query runs.
- [x] vitest is set up in Console (new). URL ⇄ explorer-state logic lives in plain modules, tested without rendering: relative and absolute ranges, mode and Query text, one history entry per run and none per keystroke.
- [x] Console request tests (WebMock stubbing EventStore): only Organization members open the explorer; the Project and time range are passed through to EventStore.
- [x] `GLOSSARY.md` gains **Logs explorer**: the Console page where a Project's Logs are searched and read.

## Comments

### 2026-10-03 built (logs-v1/m2-03)

What was built:
- **Routes and controller** (`console/app/controllers/logs_controller.rb`). The membership check is the usual `current_user.organizations.find_by!` plus `@current_org.projects.find_by!`, so non-members and other Organizations' Projects get 404.
  - `GET /:org_slug/projects/:project_slug/logs` (`project_logs_path`) renders the Inertia page `Logs/Index` with props `project` and `projects` (`{id, name, slug}`, sorted by name).
  - `GET /:org_slug/projects/:project_slug/logs/query` (`query_project_logs_path`) takes `query`, `from`, `to` (ISO 8601, any offset) and an optional `cursor`. It forwards them to EventStore with `from`/`to` in UTC, and answers EventStore's page as is. A Query with mistakes gives 400 `{errors}` (EventStore's body). A bad range gives 400 `{error}` without calling EventStore. EventStore down gives 502 `{error}`.
  - `GET /:org_slug/logs` (`logs_path`) is the sidebar's **Logs** entry, placed after Issues. It redirects to the newest Project's explorer, or to Projects with a notice when there are none.
- **EventStore client** (`app/services/event_store.rb`):
  - `EventStore.query_logs(project_id:, query:, from:, to:, cursor: nil, limit: nil)` sends times as `iso8601(3)` UTC.
  - `EventStore.validate_logs_query(project_id:, query:)`.
  - `EventStore.get_log(project_id:, log_id:, time: nil)` returns nil on 404.
  - A 400 raises the new `EventStore::Rejected` with `#body` (parsed JSON). Other failures raise `Unavailable`.
- **Frontend plain modules** (`app/frontend/lib/logs/`), tested by vitest:
  - `explorerState.ts`:
    - `ExplorerState {range, mode, query}`.
    - `stateFromSearch`, `searchOf`, `resolveRange(range, now)`, and `RANGE_PRESETS`/`spanLabel`.
    - `Explorer {current, draft}` with `openExplorer(search)`, `typeQuery`, `pickRange` (draft only, so no history entry) and `runExplorer`. `runExplorer` returns `{explorer, push}`, where `push` is the search to push, or null.
    - URL format: `?range=1h&mode=builder&q=…` for a relative range, `?from=<UTC ISO>&to=<UTC ISO>&mode=…&q=…` for an absolute one. Mode is `builder|query`, defaulting to builder. A range or mode it can't read falls back to the defaults (last hour, Builder).
  - `stream.ts`: the `Log` type (EventStore's JSON) and `startStream`/`addOlderPage`/`streamEnd` (`more|end|capped`, with `MAX_STREAM_LOGS = 10_000`).
  - `api.ts`: `fetchLogPage({…, signal})`, and `RunError` with `problems` (line and column).
- **Page** (`pages/Logs/Index.tsx`, `pages/Logs/components/LogStream.tsx`):
  - The bar holds the Project select (a server visit to the other Project, keeping the range, mode and Query), the range select (runs on pick), a read-only view of the URL's Query text when there is one, and Run, which turns into Cancel while the first page loads.
  - Run errors and "cancelled" replace the result area.
  - Lines show `MMM dd HH:mm:ss.SSS` in local time (raw UTC in the hover title), `[service]`, Level, message, `environment=` and the first 4 Attributes. warn, error and fatal get a coloured left edge.
- **vitest:** 3.2 (vitest 4 needs Vite 6, and Console is on Vite 5). Its config is `console/vitest.config.mts`, which leaves out the Ruby plugin. It runs `app/frontend/**/*.test.ts` in node. The new `package.json` scripts are `bun run test` (`vitest run`) and `bun run typecheck`.

Choices the ticket left open:
- **Virtualised list: `@tanstack/react-virtual` 3.14.** It is headless: it renders our own Tailwind markup, so it fits shadcn on base-ui with no styling of its own. Its peers include React 19, and it doesn't touch Inertia. Lines have a fixed 22 px height, so 10,000 lines cost about 60 DOM rows. `useFlushSync: false` avoids React 19's flushSync-in-render warning. react-virtuoso was the alternative. It is heavier, styles itself, and has paid add-ons for some features.
- **History:**
  - A run pushes `router.push({url, preserveState: true})`, Inertia 2's client-side visit, with no server round trip. A run that leaves the URL unchanged (Run again on the same state) pushes nothing, so Back never appears to do nothing.
  - On Back, Inertia restores the page without preserved state, so the page remounts and reads `window.location.search`. That re-runs the earlier state.
  - The first load doesn't rewrite a bare `/logs` URL to the canonical one.
- **Relative ranges** are resolved in the browser when a run starts. Older pages reuse that run's `from`/`to` with `next_cursor`, so paging stays in one window.
- **Cancel** aborts the browser's fetch, and with it Console's wait. EventStore's scan finishes on its own, as M2-01 noted. Cancel shows only for the first page. Loading an older page is aborted by the next run, but has no button of its own.
- **Proxy as GET:** running a Query is a read, so GET keeps it free of CSRF and easy to abort. Rails POSTs JSON on to EventStore.
- **Capped** is shown when 10,000 Logs are loaded and EventStore still gave a cursor. EventStore gives a cursor for any full page, so a range of exactly 10,000 Logs also shows the "Narrow the time range" note.

Deviations: none. The bar has no Query input yet (Builder controls come in M2-04). A Query in a shared URL still runs and is shown read-only in the bar, so the user isn't left with filtered Logs and no sign of why.

Follow-ups:
- **M2-04:**
  - Add Console routes for validate and fetch-by-id. The client methods exist. Pass the Log's own `time` as the fetch hint.
  - Wire the search box and chips to `typeQuery` (typing) and to `runExplorer` via the page's `commit` (Run).
  - The tag menu and inspector go on `LogLine` in `LogStream.tsx`. Remember the base-ui `DropdownMenuLabel`-needs-a-Group trap.
- **M2-05:** the histogram strip sits between the bar and the result area in `Logs/Index.tsx`, using the same run's `from`/`to`.
- **M3-02 / M3-04:**
  - Extend `Mode` and `MODES` in `explorerState.ts` (add `sql`, and the `sql` text param).
  - SQL must never run on load: guard the mount effect in `Logs/Index.tsx`, which runs `explorer.current` today.

### 2026-10-03 code review

- **The stream fetches on by itself** while EventStore answers short of a page (`fillStream`, `keepsFetching`, `wantOlder` in `lib/logs/stream.ts`).
- **Cancel keeps the Logs found so far.**

See the spec's 2026-10-03 code review entry, item 7.
