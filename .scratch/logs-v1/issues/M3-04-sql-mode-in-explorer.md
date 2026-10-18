# M3-04: SQL mode in the explorer

**What to build:** on an instance with SQL queries on, a member switches to SQL mode, starts from the readable SQL of the current Query, and runs it; a shared link with SQL never runs on open. Spec: [Console](../spec.md#console) (Proxying); [Logs explorer](../spec.md#logs-explorer-console-frontend) (Bar, SQL mode, URL); user stories 67, 68, 70, 71, 74.

**Blocked by:** M3-02, M3-03.

**Status:** resolved

- [x] Console's EventStore client gains readable SQL and Run SQL; Console refuses both when the setting is off.
- [x] The switch shows `Builder | Query | SQL` when the setting is on and `Builder | Query` when off.
- [x] SQL mode opens with the readable SQL of the current Query, editable, run only on Run. Chips and histogram are hidden.
- [x] Leaving SQL mode with edits asks before discarding them (one-way).
- [x] The URL carries the SQL text; SQL never runs on page load, only when Run is pressed.
- [x] Console request tests: SQL operations refused and SQL mode hidden while the setting is off.
- [x] vitest: SQL never auto-runs from a URL; the one-way discard prompt.

## Comments

### 2026-10-03 built (logs-v1/m3-04)

What was built:
- **EventStore client:** `EventStore.readable_logs_sql(project_id:, query:, from:, to:)` → `{"sql"}` and `EventStore.run_logs_sql(project_id:, sql:, from:, to:)` → `{"table"}`. Both raise the new `EventStore::SQLQueriesOff` before any request while `InstanceSetting.current.sql_queries_enabled?` is false, and also when EventStore answers 403. A 400 raises `Rejected` (a Query's `errors`, or DuckDB's `error`), other failures `Unavailable`.
- **Console routes** (`LogsController`, behind the membership check):
  - `GET …/logs/readable_sql?query=&from=&to=` (`readable_sql_project_logs_path`).
  - `POST …/logs/sql` with `{sql, from, to}` (`sql_project_logs_path`). POST only, so it needs the page's CSRF token: a link or an `<img>` can't run SQL as whoever opens it.
  - Both answer `403 {error}` while the setting is off, 400 with EventStore's body, 502 when EventStore is down, and 400 for a range that isn't ISO 8601.
  - The explorer page gets the prop `sql_queries_enabled`.
- **Plain modules** (vitest):
  - `explorerState.ts`: `Mode` gains `'sql'`, and `MODES` is `builder, query, sql`. `modesFor(sqlQueriesEnabled)` gives the switch's modes. `ExplorerState.sql` is the SQL text; the URL carries it as `sql` when it isn't empty. `stateFromSearch`/`openExplorer(search, modes)` open a mode not in `modes` (SQL while off) in the Builder, dropping the SQL. `runsOnOpen(explorer)` is false in SQL mode. `typeSql(explorer, sql)` edits the draft.
  - `sqlMode.ts`: `enterSql(explorer, seed)` puts the readable SQL in both `current` and `draft` (so the URL holds it) and gives `replace`. `leaveSql(explorer, seed, mode)` gives `switch` (with `explorer`, `replace`) when neither the draft nor what ran differs from the seed, else `ask`. A seed of `null` (SQL opened from a URL) counts any SQL as edits. `discardSql(explorer, mode)` drops the SQL.
  - `api.ts`: `fetchReadableSql` and `runSql`. `runSql` POSTs with the `csrf-token` meta tag and returns a `LogTable`.
- **Components:** `SqlBox` (the SQL text, Ctrl+Enter runs, a line saying `logs` holds the bar's Project and range, and the **Discard and switch** / **Stay in SQL** prompt). `ModeSwitch`'s `LABELS` gains `SQL`.
- **Page flow** (`Logs/Index.tsx`):
  - Into SQL fetches the readable SQL of the draft Query over the draft's resolved range. It aborts any run, and the result area says "Press Run to run the SQL." If the Query has mistakes, it stays and shows a toast, and in Query mode the mistakes show under the box.
  - A run in SQL mode (Run or Ctrl+Enter) pushes a history entry and runs `runSql`. It doesn't fetch the histogram, and the strip is hidden. Its table shows in `LogTableView` with no pivots.
  - Picking a range in SQL mode only changes the draft, so SQL still runs only on Run.
  - A URL (or Back) in SQL mode runs nothing on mount.
  - Leaving SQL: `leaveSql` decides whether to ask. Leaving drops the SQL, replaces the URL entry and runs the Query again in place of the SQL's result (without pushing a history entry). Leaving for the Builder validates the Query. With chips it lands in the Builder; otherwise it lands in Query mode with M3-02's "The Builder can't show …" prompt.

Choices the ticket left open:
- **The check sits in the client**, not only the controller: any Console caller of `readable_logs_sql`/`run_logs_sql` is refused while the setting is off. EventStore's 403 on Run SQL is the second check.
- **Readable SQL is refused while off too**, as the ticket asks, even though EventStore serves it.
- **"Edits"** means SQL other than the seed, whether typed or already run. SQL that came from a link has no seed, so leaving it always asks.
- **Leaving SQL re-runs the Query** in every case, so the result area never shows an SQL table under the Builder or Query. The Query and draft Query text are untouched while in SQL mode.
- **The SQL is seeded from the draft Query**, so typed but unrun Query text comes along, as Builder → Query does.
- **Run SQL is a POST.** Every other Logs route is a GET. A GET would let a crafted link or embedded image run SQL as whoever loads it (story 74).

Deviations: none. EventStore is unchanged, so the Go suite wasn't run.

Not verified in a browser: this box has no browser. tsc passes, and a throwaway server render of `SqlBox` and `ModeSwitch` (on and off) ran.

Follow-ups:
- **Long SQL in the URL:** Puma limits a query string to 10 KB, so a page reload with very long SQL would fail. Readable SQL is usually far shorter. If it matters, keep the SQL in history state instead of the URL.
- `docs/logs/sql.md` says "Times are shown in UTC", but `LogTableView` shows `datetime` columns in local time, with UTC in the hover. Worth a check against what Run SQL's column types give.
