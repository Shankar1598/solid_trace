# 12: How do the Builder, APL and SQL modes relate?

Type: prototype
Status: resolved
Blocked by: 06, 11
Map: [SolidTrace Logs](../map.md)

## Question

How does the visual query builder work, and how does a query move between Builder, APL and SQL modes (round-trip, one-way, or separate)? React to a rough prototype in the explorer layout.

Input from [How should the Logs explorer be laid out?](06-logs-explorer-layout.md) (2026-10-02): the chosen layout drives queries from filter chips and a search box, with the query text hidden behind a toggle. This ticket decides where the Builder, APL and SQL modes sit in that bar.

Input (2026-10-02, from [What query language(s) do users write?](11-query-language.md)): the modes are the KQL-style query language and SQL queries (only when the instance setting is on). Filter chips and search already compile to query text, which is always the real query. Decide whether chips can represent any Query or only a subset, and how an edited Query that chips can't show is presented. There is no APL mode by that name.

## Prototype

Built 2026-10-02 on ticket 06's chosen layout (variant D). Captured on the throwaway branch `prototype/logs-query-modes` (commit `7d19a45`, off `prototype/logs-explorer-layout`). To view: check out that branch, run `bin/rails db:seed`, sign in as `admin@solidtrace.local` / `password123`, then open `/solid-trace-org/logs-prototype?variant=A|B|C` in development. `&sql=off` shows an instance with SQL queries turned off.

- Code: `console/app/frontend/pages/LogsPrototype/` (`ModesA/B/C.tsx`, shared `modes.tsx`, and `query.ts`, a toy parser, evaluator and SQL printer for the ticket-11 language that runs over fake Logs in memory). The prototype cannot run DuckDB, so an edited SQL query shows a placeholder.
- **A, Mirror (two-way):** the query text is the only state. A `where f ==|!= v` stage is a chip, `search` fills the search box, and any other stage is a dashed text chip. The text applies as you type; chip edits re-print the text and lose the user's formatting. SQL is a tab in the same editor.
- **B, Modes (one-way out):** a Builder | Query | SQL switch, with a structured Builder (chips, search, and a Summarize row).
- **C, Split:** chips and search own the filters; an always-visible `|` box owns the rest, so chips never show an arbitrary Query. SQL is a separate console page.

## Answer

Resolved 2026-10-02 (prototype). The user chose **variant B**, with its Summarize row removed. Glossary: **Builder** added to `GLOSSARY.md`.

- **Three modes:** a segmented `Builder | Query | SQL` switch at the start of the bar, after the Project and time-range selects. The explorer opens in Builder. With the instance's SQL setting off, the switch shows only `Builder | Query`.
- **The Builder is filters only:** filter chips (`field = value` / `field ≠ value`) and the search box, exactly as ticket 06 laid them out. It has no Summarize, group-by or sort controls; anything else is written in Query mode. So the Builder can show a Query only if every stage is a `where field ==|!= literal` or a single `search`.
- **Builder → Query** always works: Query mode opens an editable query text box holding the Builder's Query. This replaces ticket 06's read-only "show query" toggle; Query mode is where the text is seen and edited.
- **Query → Builder:** if the Builder can show the Query, it switches silently. Otherwise it names the stages it can't show and offers **Discard and switch** (back to the last Builder state) or **Stay in Query**.
- **Pivots in Query mode:** Show only this and Hide this add a `where` stage after the leading filters (before any `summarize`) and re-run, so a pivot from a summarized table narrows the Logs that feed it.
- **SQL mode** opens with the **compiled SQL of the current Query**, editable, and runs as written. It is one-way: leaving SQL with edits asks to discard them. In SQL mode the chips and histogram are gone, and the Project and time range do not apply.
- Queries whose last stage is not a Log (`summarize`, `project`, `distinct`, `count`) show a table in place of the stream; the histogram keeps counting the Logs that passed the filters. This was prototype behaviour the user did not object to, not a separate decision.

Notes for later tickets:
- Seeding SQL from the compiler means the compiler must be able to print readable SQL against the documented `logs` view (ADR 0003), even if what it runs targets the storage layout directly. The prototype printed one CTE per stage.
- Not decided: whether the mode and the Query text live in the URL, and how a summarized table is charted (for example `summarize … by bin(time, …)` as a chart).
- Rejected, kept on the branch: A's chips mirrored from arbitrary text, C's pipe box and separate SQL console, and B's Summarize row.

## Comments

### 2026-10-02 from ticket 10 (second pass)

[How are logs stored and retained?](10-storage-architecture-and-retention.md) changed what SQL mode sees: the `logs` view is built per query for the explorer's Project and time range, so in SQL mode the Project and time range now apply and users no longer filter `project_id` themselves. It is a convenience, not a boundary: the SQL still runs unvalidated and can read the raw tables ([ADR 0003](../../../docs/adr/0003-unrestricted-sql-queries-behind-an-instance-setting.md), [ADR 0005](../../../docs/adr/0005-logs-per-project-hot-tables-promote-at-ingest.md)).
