# 01: Probe for Log read speed

**What to build:** A probe that times the reads this effort changes, so each later ticket can show its before and after. It is gated by an environment variable, like the storage layout probes, and isn't part of the suite. It builds about 1.5M synthetic Logs for one Project: one hour in the hot table, and the same hour as a Log file listed in the manifest. The Logs have long `message`s, a few promoted Attributes, some Attributes only in the maps and a rare term in a few messages. On 2 DuckDB threads, it times what EventStore actually runs, through the LogStore and the Query compiler, not hand-written SQL:

- the newest page (200 Logs, no Query) over the hour;
- `search "term"`, `where message contains "term"` and `where message contains_cs "term"`;
- `where message contains "term" and <map Attribute> == "x"`, written in both orders.

See the [spec](../spec.md)'s Testing Decisions and Further Notes.

**Blocked by:** None (can start immediately).

**Status:** ready-for-agent

- [ ] The probe runs in under a minute on the dev box, setup included, and is skipped unless its environment variable is set
- [ ] It reads the hot table and the Log file through the same code paths a Query run uses
- [ ] Each timing is the best of a few runs, printed with the number of Logs it found
- [ ] Today's figures are recorded in this ticket's Comments as the baseline for 02 to 04
