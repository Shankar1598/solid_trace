# M2-04: Builder chips, search box and inspector

**What to build:** a member filters Logs by clicking: any value in a line is a tag that can Show only this, Hide this or Copy; filters show as editable chips beside a search box; clicking a line opens an inspector over the stream. Spec: [Logs explorer](../spec.md#logs-explorer-console-frontend) (Builder, Stream tags, Inspector, Libraries); user stories 44–47, 51, 52. Prototypes: branches `prototype/logs-explorer-layout` (variant D) and `prototype/logs-query-modes` (variant B).

**Blocked by:** M2-03.

**Status:** ready-for-agent

- [x] Every value in a line, including Level and Service, is a tag with Show only this, Hide this and Copy value. A filter built from a tag uses that value's own type.
- [x] Filters show as chips `field = value` / `field ≠ value`; clicking the operator flips it, × removes it. Hide this keeps Logs that don't have that Field.
- [x] A search box adds the Query's one `search` stage.
- [x] The Builder's state is a Query: chips and search are turned into Query text in a plain module, and the Builder reads chips back only from the validate operation (the browser never parses Queries).
- [x] Inspector: clicking a line opens an overlay on the right of the stream (it doesn't narrow it), with Fields and JSON tabs; every value is a tag; the raw `time` shown in UTC ISO. Uses the fetch-one-Log operation. Choose the drawer to fit the stack; note the choice in this ticket's Comments. Trap from the prototype: base-ui's `DropdownMenuLabel` throws outside a `DropdownMenuGroup`.
- [x] vitest: chips ⇄ Query text, operator flip, remove, typed values, Hide keeping missing Fields.

## Comments

### 2026-10-03 built (logs-v1/m2-04)

What was built:
- **Console routes** (`LogsController`, both behind the membership check, tested in `logs_controller_test.rb`):
  - `GET /:org_slug/projects/:project_slug/logs/validate?query=` (`validate_project_logs_path`) answers EventStore's `{errors, chips}` as is; 502 `{error}` when EventStore is down.
  - `GET /:org_slug/projects/:project_slug/logs/:id?time=` (`project_log_path`) answers one Log's JSON, 404 when EventStore has none, and 404 without calling EventStore when the id isn't a UUID (EventStore would answer 400).
- **Plain modules** (`app/frontend/lib/logs/`, vitest):
  - `builder.ts`: `Builder {filters: Chip[], search}`, `Chip {field, op: '=='|'!=', value: string|number|boolean}`, `Chips` (validate's shape). `queryOf(builder)` writes one `where` stage per chip in order, then `search`, joined by `\n| `. `builderOf(chips)`, `flipFilter`, `removeFilter`, `search`, `showOnly`, `hide`, `canFilter(value)`.
    - Names are bare when every dotted part is a plain identifier and not `and`/`or`/`not`/`true`/`false`; otherwise `['quoted']`. Strings use double quotes with `\\ \" \n \t \r` escapes. Numbers and bools are written bare, so a filter keeps its value's type.
    - Checked once with a throwaway Go probe (not committed): every Query text the vitest cases expect compiles, and `Chips()` gives back the same chips.
  - `inspector.ts`: `inspectorFields(log)` (built-ins the Log has, then Attributes by name), `utcTime(time)` and `inspectorJson(log)`.
  - `api.ts`: `validateQuery({…, query})` and `fetchLog({…, id, time})` (null on 404).
- **Components** (`pages/Logs/components/`):
  - `TagMenu.tsx`: `TagMenuProvider({onFilter})` and `Tag({field, value})`. One base-ui menu for the whole page; each tag is a detached `Menu.Trigger` with `handle` and `payload` (base-ui 1.1 `Menu.createHandle`), so 10,000 virtualised lines mount no menu root per value. The label sits inside a `DropdownMenuGroup` (the prototype's trap). `onFilter` null disables Show only this and Hide this; `time` and null/array/object values offer Copy only.
  - `BuilderBar.tsx`: chips (click `=`/`≠` to flip, × to remove) and the search box (Enter runs). `BuilderView` is `reading | ready | unshowable`.
  - `LogInspector.tsx`: the inspector.
  - `LogStream.tsx`: every value in a line (time, `[service]`, Level, message, `environment=` and the inline Attributes) is a `Tag`; clicking elsewhere on the line opens the inspector (not when text is being selected); the open Log's line is highlighted. `LEVEL_TEXT` is exported.
- **Page flow** (`Logs/Index.tsx`): chip changes and tag filters run at once (a history entry each); typing in the search box only edits the draft, and Enter or Run commits it. On mount (also after Back, which remounts) a non-empty Query is sent to validate, and the chips come only from its answer.

Choices the ticket left open:
- **Inspector drawer: no library.** A non-modal `aside` positioned absolutely over the right of the result area (460 px, `shadow-2xl`, Escape or × closes), as in prototype variant D. A shadcn Sheet or base-ui Dialog is modal: its backdrop would block clicking another line or a tag behind it, and it would cover the whole page rather than the stream. Tabs are the existing shadcn `Tabs` (base-ui). It shows the stream's copy of the Log at once and swaps in the fetched one, passing the Log's own `time` as the hint. It stays open across runs until closed.
- **Show only this** replaces any chip on the same value and the Field's other `==` chips (two `==` on one Field match nothing); it keeps the Field's `!=` chips. **Hide this** replaces any chip on the same value. Values of different types are different values (`500` vs `"500"`).
- **A Query the Builder can't show** (from a shared URL, or validate unreachable) is shown read-only with "Clear it", which empties the Builder and runs. Tag filters are disabled then; Query mode (M3-02) takes over pivots on such Queries.
- **Hide keeping missing Fields** is the language's `!=` rule (M2-01, tested in Go). The vitest checks that Hide is written as `!=`.

Deviations: none.

Not verified in a browser: this box has no browser. tsc passes, and a throwaway server render of the tag menu, inspector and Builder bar ran without errors. The open menu (portal) wasn't rendered.

Follow-ups:
- **M3-02:** Query mode can reuse `queryOf`/`builderOf` and `BuilderView`; the mount-time validate in `Index.tsx` is where Query to Builder's silent switch belongs. In Query mode `onFilter` is null today, so pivots there need their own handler.
- **M2-05:** per-Level counts that filter on click can call `showOnly(builder, 'level', level)` through the page's `changeBuilder`.

