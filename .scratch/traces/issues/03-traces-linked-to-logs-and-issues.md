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

**Status:** resolved

- [x] The trace route answers a sampled trace's Logs and Events, and an unsampled trace's around its hint (Go)
- [x] The page's props for a sampled and an unsampled trace, still rendering Console's part when EventStore fails (Rails controller test)
- [x] `get_trace` keeps within its budget on a trace of 2,000 Spans and includes Issues and `query_logs` arguments (MCP tool test)
- [ ] A Playwright flow opens a trace from an Issue and from a Log, including an unsampled one (written, `console/e2e/flows/traces.spec.ts`; not run, as the dev stack runs main's code with its EventStore down)

Notes:

- Logs are read ± 5 minutes around the trace's Spans, or around the hint. With neither, the route finds the trace's Events at any time (their trace id has a bloom filter) and reads Logs ± 5 minutes around the first. So a pasted id of an unsampled trace still finds its Logs.
- The answer is capped at 500 Logs (`more_logs`) and 200 Events. Events are grouped by Issue fingerprint, and Console maps each fingerprint to its Issue.
- Repeats collapse when sibling Spans under one parent share a group. The row's count is the number of Spans, its durations and self times are totals, and its children are all of theirs.
- The critical path works back from the trace's end. It takes the child that ended last, then the last to end before that child started, and recurses into each.
- ClickHouse migration 2301 adds the `logs` trace id index, plus the `events` trace id column and its index.
