# M2-04: Builder chips, search box and inspector

**What to build:** a member filters Logs by clicking: any value in a line is a tag that can Show only this, Hide this or Copy; filters show as editable chips beside a search box; clicking a line opens an inspector over the stream. Spec: [Logs explorer](../spec.md#logs-explorer-console-frontend) (Builder, Stream tags, Inspector, Libraries); user stories 44–47, 51, 52. Prototypes: branches `prototype/logs-explorer-layout` (variant D) and `prototype/logs-query-modes` (variant B).

**Blocked by:** M2-03.

**Status:** ready-for-agent

- [ ] Every value in a line, including Level and Service, is a tag with Show only this, Hide this and Copy value. A filter built from a tag uses that value's own type.
- [ ] Filters show as chips `field = value` / `field ≠ value`; clicking the operator flips it, × removes it. Hide this keeps Logs that don't have that Field.
- [ ] A search box adds the Query's one `search` stage.
- [ ] The Builder's state is a Query: chips and search are turned into Query text in a plain module, and the Builder reads chips back only from the validate operation (the browser never parses Queries).
- [ ] Inspector: clicking a line opens an overlay on the right of the stream (it doesn't narrow it), with Fields and JSON tabs; every value is a tag; the raw `time` shown in UTC ISO. Uses the fetch-one-Log operation. Choose the drawer to fit the stack; note the choice in this ticket's Comments. Trap from the prototype: base-ui's `DropdownMenuLabel` throws outside a `DropdownMenuGroup`.
- [ ] vitest: chips ⇄ Query text, operator flip, remove, typed values, Hide keeping missing Fields.
