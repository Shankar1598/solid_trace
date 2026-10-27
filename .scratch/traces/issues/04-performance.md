# 04: Performance

Spec: [../spec.md](../spec.md)

**What to build:** a Project's **Performance** page and a Transaction's page, with `list_transactions` and `get_transaction` over MCP.

- **Performance** lists one row per Transaction name, sorted by time consumed by default and sortable by any column. Each row has:
  - throughput scaled by sample rate, labelled as an estimate;
  - p50, p95 and p99;
  - weighted error rate;
  - time consumed;
  - each number's change from the previous period.

  Raw-URL names fold into "Unparameterised". The range and environment pickers work as on Logs and are kept in the URL. A Project with no Transactions links to the Traces docs. The sidebar gains Performance under each Project.
- **A Transaction** shows:
  - p50 and p95 over the range, by release;
  - self time by op and by group;
  - repeated groups with their median and maximum repeats;
  - its slowest traces, each linking to the trace page.
- Two Query API routes answer them. Their reads carry the Overview's read tags, so they appear in its Health. One builder per view feeds both the page and its tool.

**Blocked by:** 02

**Status:** resolved

- [x] Weighted throughput, error rate and time consumed, with the previous period, from Transactions at two sample rates (Go)
- [x] A Transaction's breakdown, release series, repeats and slowest traces (Go)
- [x] Each page's props agree with its MCP tool (Rails controller + MCP tool tests)
- [x] A Playwright flow opens Performance, sorts it, opens a Transaction and then one of its slowest traces

Notes:

- An error is a segment whose status was sent and isn't `ok`. A segment sent with no status (`""`) isn't an error, since the SDK said nothing about how it ended.
- The flow (`console/e2e/flows/performance.spec.ts`) is written but wasn't run: the dev stack had no EventStore up when this was built.
- The empty page links to `docs/traces.md`, which ticket 06 writes.
