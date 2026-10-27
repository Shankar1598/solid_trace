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

**Status:** ready-for-agent

- [ ] Weighted throughput, error rate and time consumed, with the previous period, from Transactions at two sample rates (Go)
- [ ] A Transaction's breakdown, release series, repeats and slowest traces (Go)
- [ ] Each page's props agree with its MCP tool (Rails controller + MCP tool tests)
- [ ] A Playwright flow opens Performance, sorts it, opens a Transaction and then one of its slowest traces
