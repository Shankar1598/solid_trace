# M3-02: Query mode in the explorer

**What to build:** a member switches from Builder to Query, edits the Query text, sees tables for tabular Queries, pivots from them, and switches back without losing work by surprise. Spec: [Logs explorer](../spec.md#logs-explorer-console-frontend) (Bar, Query mode, Non-Log results, Errors); user stories 56, 57, 61–65. Prototype: branch `prototype/logs-query-modes` (variant B).

**Blocked by:** M2-05, M3-01.

**Status:** ready-for-agent

- [ ] The bar gains a `Builder | Query` switch after the Project and time range, opening in Builder.
- [ ] Query mode opens with the Builder's Query as editable text.
- [ ] Query → Builder switches silently when validate returns chips; otherwise it names the stages the Builder can't show and offers **Discard and switch** or **Stay in Query**.
- [ ] Show only this and Hide this in Query mode add a `where` stage after the leading filters, before any `summarize`, and re-run.
- [ ] A tabular result shows as a table in place of the stream; the histogram stays and counts the filtered Logs. No charts.
- [ ] Compile errors show under the Query box with line and column, keeping the last good result.
- [ ] vitest: the mode-switch rules (silent switch with chips, Discard/Stay otherwise); pivot insertion before `summarize`.
