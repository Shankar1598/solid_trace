# 01: A change Monitor against the range just before

**What to build:** a member makes a Threshold monitor that compares the change instead of the value, either on the Monitor form or over MCP. It compares against the range just before and fires when the number rises, falls or changes by at least X%. Run now shows what it found. The Monitor's page shows each group as "120 (baseline 60, +100%)". Slack, email and PagerDuty say what changed, and Slack and email link to both ranges. Switching a Monitor between value and change resolves its firing groups. Spec: [spec.md](../spec.md), user stories 1–4, 7–10, 13–16, 18–21, 25–34, 38, 39, 41–43.

**Blocked by:** None (can start immediately)

**Status:** ready-for-agent

- [ ] Prefactor first: in a Threshold run, groups are kept per row before the worst is chosen, with today's behaviour unchanged and today's tests green.
- [ ] Schema: Monitors gain `compare` (default `value`) and `baseline`; group states gain `last_baseline`. Existing Monitors are unchanged.
- [ ] Saving accepts `compare = change` with `baseline = previous`, a direction (`rises`/`falls`/`changes`) and a threshold percent greater than 0. It refuses value operators in change mode, change operators in value mode, and a change Monitor without a Baseline.
- [ ] A run reads the current range, then the Baseline range (the current one shifted back by its own length). The second Query gets what's left of the 30 s budget. A failure, timeout or cut table on either side is an error run that changes no state.
- [ ] Change rule as in the spec:
  - the threshold is met (`>=`) for each direction;
  - a zero Baseline is an infinite rise or fall, and 0 → 0 is no change;
  - a row now with none in the Baseline uses a Baseline of 0;
  - a row in the Baseline with none now is ok;
  - negative numbers divide by `|baseline|`;
  - with notify by group off, the worst change decides;
  - alert on no data looks at the current table only.
- [ ] `last_baseline` is kept per group, and the Monitor's page shows value, Baseline and change. The summary sentence describes the change rule.
- [ ] Messages:
  - firing and resolved text per user stories 29–31, with "up from 0" for a zero Baseline;
  - the fields Value, Baseline, Change and Threshold;
  - Slack and email link to Now and Baseline;
  - PagerDuty keeps one link and its dedup keys, and adds the Baseline and change to its custom details.
- [ ] Switching between value and change resolves firing groups with a reason of its own. Changing the direction or threshold resolves nothing.
- [ ] Monitor form: a Compare setting (The value / The change), and in change mode the direction and the threshold as a percent. Baseline offers only "the range just before" in this ticket.
- [ ] MCP: create, update and get take and return `compare`, `baseline` and the change operators. Get returns `last_baseline` per group, and Run now's answer gives each group's change. Tool descriptions and the MCP docs say so.
- [ ] The Monitors doc gains "Comparing the change": the settings, the zero-Baseline rule, missing rows (and that "falls" misses a group that stops sending, with what to use instead), two Queries per run, and the switch resolve.
- [ ] Tests at the agreed seams:
  - Monitor runs with EventStore stubbed per range: every case above;
  - message builders: wording, "up from 0", both links, PagerDuty details, the switch resolve text;
  - saving over MCP and the controller: round trip, validations, the switch resolve and no resolve on other edits;
  - the Monitors Playwright flow: make a change Monitor, Run now, see the page's Baseline and change;
  - vitest for the change formatting and summary helpers.
