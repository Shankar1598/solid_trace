# Logs explorer

The Logs explorer is the Console page where a Project's Logs are searched and read. A user makes a Query with the Builder (filter chips and a search box), types one in Query mode, or writes a SQL query in SQL mode. The histogram and the stream answer for the chosen time range. A Chart draws a Query that summarizes. The Live tail shows Logs as they arrive.

## Sub-features

- `logs-builder`: a Where filter narrows the stream and the histogram. A Summarize shows a table. More adds a search, a sort and a limit (`logs-v1-builder.spec.ts`).
- `logs-query`: `Query` mode runs by `Run` and by Ctrl+Enter. A bad Query shows its line and column. The way back to `Builder` asks before it drops edits (`logs-v1-query.spec.ts`).
- `logs-sql`: SQL mode runs a SELECT on the Project's `logs`, `events` and `spans`, and refuses SQL that changes data or reads other tables (`logs-v1-sql.spec.ts`).
- `logs-histogram`: the histogram counts Logs by time and zooms in (`logs-v1-histogram.spec.ts`).
- `logs-inspector`: a Log opens in the inspector with its Fields (`logs-v1-inspector.spec.ts`, `inspector-extras.spec.ts`).
- `logs-time-range`: the time-range picker's quick ranges and custom range (`time-range.spec.ts`).

## How to get to it (user POV)

- Sidebar navigation `Main`, link `Logs`, which opens `/<org>/projects/<project>/logs`.
- The mode buttons `Builder`, `Query` and `SQL` on the explorer's bar. The `Run` button, or Ctrl+Enter.
- The button `Time range: <range>`.
- A shipper, an application or a Collector that sends Logs to ingest. Its routes are in [Log ingest](./log-ingest.md).

## Driving it with verify

Preconditions:

- The baseline stack is up, and `"$V" doctor` is all `ok`.
- The seed Project has Logs in each hour of the last 24 hours. The first `verify flow` sends them. Without a flow, run `"$V" run -- bin/rails dev:logs PROJECT=solid-trace-project COUNT=20000 HOURS=24 SEED=42`.

- **Builder.** `"$V" flow -- e2e/flows/logs-v1-builder.spec.ts`. Pass: each test passes.
- **Query mode.** `"$V" flow -- e2e/flows/logs-v1-query.spec.ts`. Pass: each test passes. The trace shows the `Query` textbox, the run, and the error with its line and column.
- **SQL mode.** `"$V" flow -- e2e/flows/logs-v1-sql.spec.ts`. Pass: a SELECT runs, and changes or reads of other tables are refused.
- **Proof.** The traces in `<run>/evidence/flow-<time>/playwright/`, or the `snap` PNG and ARIA files.

## Gotchas

- Log processing moves stored Log batches into ClickHouse about once a second. Wait for the text or poll. Do not use a fixed sleep.
- Right after a flow or a `dev:logs` top-up, the page can say `Not counted: Console is busy running other Queries and charts.` and the stream can be late. That is the heavy-read limit, not a fault. `snap --wait-text` waits 30 s. If the text is still missing, run `snap` again before you report a failure.
- A relative range moves with the clock. For counts that must not change between runs, use a fixed range (`last24h()` in `e2e/support/logs.ts`).
- In Query mode, a run starts only after the validate step answers. Wait for the histogram response (`runBy` in `e2e/support/logs.ts`), not for the button.
- The dummy Logs all have a `region` Attribute. To see only them, filter with `isnotnull(region)`. A probe Log that you send by hand does not change what the flows count.
