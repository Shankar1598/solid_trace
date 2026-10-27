# 03: Traces linked to Logs and Issues, and get_trace

Spec: [../spec.md](../spec.md)

**What to build:** a trace shows its Logs and Issues whether or not it was sampled, large traces read well, and agents get `get_trace`.

- `logs` gains a trace id skip index (new parts only), and `events` a trace id filled by Event processing. The trace route answers the trace's Logs (its Projects, its time ± 5 minutes, or around a time hint when nothing was sampled) and its Events grouped by Issue.
- The page lists the Logs in time order and the Issues, each linked. An unsampled trace opens with a note and its Logs and Issues.
- Repeated Spans collapse into one row with a count and total, and the critical path is marked.
- The Issue page's Event and the Log detail link to the trace page.
- `get_trace` answers from the same builder:
  - repeats collapsed, with self time and the critical path;
  - the top Spans by self time first, about 200 rows at most, with a count of what was left out;
  - the trace's Issues, and the `query_logs` arguments for its Logs;
  - a link to the page.

**Blocked by:** 02

**Status:** ready-for-agent

- [ ] The trace route answers a sampled trace's Logs and Events, and an unsampled trace's around its hint (Go)
- [ ] The page's props for a sampled and an unsampled trace, still rendering Console's part when EventStore fails (Rails controller test)
- [ ] `get_trace` keeps within its budget on a trace of 2,000 Spans and includes Issues and `query_logs` arguments (MCP tool test)
- [ ] A Playwright flow opens a trace from an Issue and from a Log, including an unsampled one
