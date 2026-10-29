# % change on Threshold monitors

**Status:** ready-for-agent

Planned with the user on 2026-10-28/29. The user settled round 1 of the grilling and asked the agent to take round 2's calls. Tickets: [01](issues/01-change-monitor-against-the-range-just-before.md) (a change Monitor end to end, against the range just before), [02](issues/02-day-and-week-baselines-and-minimum.md) (1-day and 1-week Baselines, the save refusals, the minimum), [03](issues/03-form-preview-of-both-ranges.md) (the form's preview); 02 and 03 are blocked by 01 only. Terms: **Threshold monitor**, **Baseline**, **Firing** and **ok** in [GLOSSARY.md](../../GLOSSARY.md). Monitors are run as [ADR 0006](../../docs/adr/0006-monitors-evaluated-by-console-through-the-query-api.md) describes, and this effort keeps to it.

## Problem Statement

A Threshold monitor compares a number with a fixed threshold, such as "more than 50 errors in the last 5 minutes". To set one you must already know what normal is, and normal moves with the hour and the day: 50 errors at a weekday peak is fine, while 50 at 3 am is an incident. So members either set the threshold high and miss quiet-hour incidents, or set it low and get messages every busy afternoon.

What members actually want to know is relative: "errors doubled", "traffic halved since yesterday", "latency is 40% up on last week". A Threshold monitor can't say any of that today. There's also no way to see, on a Monitor's page or in its message, what the number was before.

## Solution

A Threshold monitor can compare **the change** instead of **the value**. Each run reads its Query over its range, as today, and again over its **Baseline** range: the same length, either just before it, 1 day earlier or 1 week earlier. A group's change is its number's percent change from its Baseline. The group fires when the number **rises**, **falls** or **changes** (either way) by at least the threshold percent. An optional **minimum** keeps small numbers quiet: a group is compared only when the larger of its number and its Baseline reaches the minimum.

Everything else a Threshold monitor does stays the same: the Query rules, groups and notify by group, alert on no data, firing and ok, Notifications and their dedup keys, Run now and the run history. Messages and the Monitor's page show the Baseline and the change beside the value. The form previews both ranges' numbers on one chart and lists the groups that would fire now.

## User Stories

1. As an Organization member, I want a Threshold monitor to fire when a number rises by a percent, so that I hear about "errors doubled" without knowing the normal error count.
2. As an Organization member, I want a Threshold monitor to fire when a number falls by a percent, so that I hear when traffic or sign-ups drop sharply.
3. As an Organization member, I want a Threshold monitor to fire when a number changes by a percent in either direction, so that one Monitor catches both a spike and a collapse.
4. As an Organization member, I want to compare with the range just before, so that I catch sudden changes within minutes.
5. As an Organization member, I want to compare with the same range 1 day earlier, so that daily cycles don't fire the Monitor.
6. As an Organization member, I want to compare with the same range 1 week earlier, so that weekday and weekend patterns don't fire the Monitor.
7. As an Organization member, I want the Baseline range to be exactly as long as the Monitor's range, so that the two numbers are comparable.
8. As an Organization member, I want to choose between comparing the value and comparing the change, so that one kind of Monitor covers both needs.
9. As an Organization member, I want existing Monitors to keep comparing the value, so that nothing I've set up changes behaviour.
10. As an Organization member, I want to set the threshold as a percent, so that "rises by 50" reads as 50%.
11. As an Organization member, I want an optional minimum, so that a count going from 2 to 4 doesn't fire as a 100% rise.
12. As an Organization member, I want the minimum compared with the larger of the number and its Baseline, so that a drop from 1,000 to 0 still fires a "falls" Monitor.
13. As an Organization member, I want a number that rises from a Baseline of 0 to count as a rise, so that errors appearing where there were none fire the Monitor.
14. As an Organization member, I want 0 then and 0 now to count as no change, so that a quiet service stays ok.
15. As an Organization member, I want a group that has a row now but none in its Baseline to count as rising from 0, so that a new failing service is caught.
16. As an Organization member, I want a group that had a row in its Baseline but has none now to be ok, so that an average with no data isn't read as a drop to zero.
17. As an Organization member, I want the docs to say that "falls" doesn't catch a group that stops sending, and what does, so that I choose alert on no data or an ungrouped Query for that case.
18. As an Organization member, I want alert on no data to keep meaning "the current table is empty", so that it works the same in both modes.
19. As an Organization member, I want notify by group to work in change mode, so that each service fires and resolves on its own change.
20. As an Organization member, I want a Monitor that doesn't notify by group to fire on the worst group's change, so that one message covers "some service doubled".
21. As an Organization member, I want negative numbers to have a sensible change, so that arithmetic Queries such as a balance still work.
22. As an Organization member, I want saving to refuse a 1-day Baseline when the range is longer than a day, so that the two ranges never overlap.
23. As an Organization member, I want saving to refuse a Baseline older than the Project's Retention keeps, so that the Monitor doesn't fire on an empty Baseline every run.
24. As an Organization member, I want a run to fail with a clear error when Retention has since dropped below what the Monitor needs, so that it neither fires nor stays silent without saying why.
25. As an Organization member, I want a run whose Baseline query fails, times out or is cut at 10,000 rows to be an error that changes nothing, so that a half-answered run never fires or resolves a group.
26. As an Organization member, I want a run to stay within today's 30-second budget, so that change Monitors don't hold workers longer than value Monitors.
27. As an Organization member, I want switching a Monitor between value and change to resolve its firing groups, so that a PagerDuty incident opened under the old meaning is closed.
28. As an Organization member, I want changing the Baseline, direction, threshold or minimum to let the next run decide, so that small edits don't send a resolve and a fresh firing.
29. As an Integration recipient, I want a firing message to give the number, its Baseline and the change, such as "count_ was 120 over the last 15 minutes, up 100% from 60 the 15 minutes before", so that I can judge it without opening anything.
30. As an Integration recipient, I want a rise from 0 to read "up from 0", not "up ∞%", so that the message reads naturally.
31. As an Integration recipient, I want a resolved message to give the current change, so that I know how far it came back.
32. As a Slack or email recipient, I want links to both the current range and the Baseline range in the explorer, so that I can compare the Logs side by side.
33. As a PagerDuty user, I want the Baseline and the change in the event's details, so that the incident shows why it fired.
34. As a PagerDuty user, I want the dedup keys unchanged, so that a resolve still closes the incident its trigger opened.
35. As an Organization member, I want the form's preview to chart the current and Baseline series together over the last 24 hours, so that I see how the two usually compare.
36. As an Organization member, I want the form to list the groups that would fire now, with number, Baseline and change, so that I can tune the threshold before saving.
37. As an Organization member, I want the form to show saving's refusals beside the fields they concern, so that I know what to change.
38. As an Organization member, I want the Monitor's page to show each group as "120 (baseline 60, +100%)", so that I can see why it's firing.
39. As an Organization member, I want the Monitor's summary sentence to describe the change rule, such as "fires when count_ rises by 50% or more from the 15 minutes before", so that I can read what it does at a glance.
40. As an Organization member, I want Create monitor from the Logs explorer to keep making a value Monitor, so that the quick path stays simple and I switch on the form.
41. As an AI agent over MCP, I want to make and change a change Monitor with its compare, Baseline, minimum and direction, so that I can set one up for a member.
42. As an AI agent over MCP, I want reading a Monitor to return its change settings and each group's last Baseline, so that I can explain why it fired.
43. As an AI agent over MCP, I want Run now's answer to include each group's change, so that I can test a Monitor before relying on it.
44. As an AI agent over MCP, I want saving's refusals as errors with their reasons, so that I can correct the settings.

## Implementation Decisions

### Model

- A Threshold monitor gets a **compare** setting, either `value` (the default, which is today's behaviour) or `change`. It's a setting, not a third kind of Monitor. Match monitors have neither.
- **Baseline** is one of `previous` (the range just before), `day` or `week`. It's required in change mode. There's no free offset and no absolute-difference mode.
- In change mode the **operator** is `rises`, `falls` or `changes`, and the **threshold** is a percent greater than 0. Value mode keeps `>`, `>=`, `<`, `<=`.
- **Minimum** is an optional number. With none, every group is compared.

### Schema

- Monitors gain `compare` (string, not null, default `value`), `baseline` (string, nullable) and `minimum` (float, nullable).
- The operator column also accepts the three change operators, but only in change mode.
- A Monitor's state per group gains `last_baseline` (float, nullable), beside `last_value`.

### The change rule

A group's change is `(value − baseline) / |baseline| × 100`.

- **Firing:** it fires on `change >= threshold` (rises), `change <= −threshold` (falls) or `|change| >= threshold` (changes).
- **Zero Baseline:** a number above 0 is an infinite rise, and one below 0 an infinite fall. Each counts as the biggest change possible. 0 → 0 is no change.
- **Minimum:** a group is compared only when `max(|value|, |baseline|) >= minimum`. Below that it's ok for the run.
- **Missing rows:** a row now with none in the Baseline uses a Baseline of 0. A row in the Baseline with none now is no row, so the group is ok, as today.
- **Notify by group off:** the change is worked out per row, keyed by its group columns as if notify by group were on. Then the worst change makes the one group: the largest for rises, the smallest for falls, the largest absolute change for changes. The worst value would compare the wrong rows.
- **Alert on no data** looks only at the current table.

### Runs

- The current range stays `[T − range − 60 s, T − 60 s)`.
- The Baseline range is that range shifted back exactly: by the range's own length for `previous`, 1 day for `day`, 1 week for `week`.
- Each run makes two Queries over the query API, current first, sharing the one 30-second budget: the Baseline Query is given what's left.
- A failure, timeout or cut table on either side makes the run an error that changes no state, as one failing Query does today.
- The lease, the version check and the one transaction for states, Notifications and the run are unchanged (ADR 0006).

### Saving

- Saving refuses:
  - `day` with a range over 1 day;
  - a range plus offset longer than the Project's Log Retention (the Project's own, else the instance default).
- A run that finds Retention has since dropped below what's needed fails with an error saying so.
- Switching compare between value and change resolves the firing groups, with a reason of its own. This is the same hook that resolves them when a Monitor becomes a Match monitor or is turned off. Changing the Baseline, direction, threshold or minimum resolves nothing.

### Notifications

- The firing and resolved payloads also carry compare, the Baseline, the change and the Baseline range.
- Messages are worded per user stories 29–31.
- The fields are Value, Baseline, Change and Threshold (e.g. "rises 50%").
- Slack and email gain a second explorer link, for the Baseline range.
- PagerDuty keeps one link and the same dedup keys, and adds the Baseline and change to its custom details.
- The notification kinds are unchanged. The compare-switch resolve adds a resolve reason.

### Console

- The Monitor form gains Compare. In change mode it shows Baseline, the direction, the threshold as a percent and the minimum.
- The preview charts the Query over the last 24 hours for now and for the Baseline offset, on one chart with no threshold line. It lists the groups that would fire now.
- The Monitor's page shows each group's Baseline and change, and the summary sentence describes the change rule.
- Create monitor from the explorer is unchanged.

### MCP

- The create, update and get Monitor tools take and return compare, Baseline and minimum, and accept the change operators.
- Get returns each group's last Baseline, and Run now's answer gives each group's change.
- Tool descriptions and the MCP docs describe change mode.

### Docs

- The Monitors doc gains a "Comparing the change" section covering:
  - the settings;
  - the zero-Baseline and minimum rules;
  - missing rows, and the "falls" limit with what to use instead;
  - the save refusals;
  - two Queries per run.
- Its Runs section adds the compare-switch resolve.

## Testing Decisions

- **Good tests** check what a member and an Integration would see: a Monitor's states, runs and Notifications, the text of messages, what saving accepts and refuses, and what the pages show. They never call the change rule or private helpers directly.
- **Main seam: running a Monitor**, through the one entry point that runs Monitors, with EventStore stubbed over HTTP and the clock fixed. Each test answers the current and Baseline ranges with different tables. Covered here:
  - each direction;
  - zero Baseline, and 0 → 0;
  - the minimum, including the 1,000 → 0 case;
  - both kinds of missing row;
  - notify by group off;
  - negative numbers;
  - the exact Baseline range for each offset;
  - a Baseline Query that fails, times out or is cut;
  - the Retention error;
  - `last_baseline` being kept;
  - the Notification payloads.

  Prior art: the existing Monitor evaluation tests, which already run Monitors this way.
- **Message builders:** wording, "up from 0", the two links, PagerDuty details and the compare-switch resolve text. Prior art: the existing Monitor message tests.
- **Saving through MCP and the controller:** the new settings round-trip, both refusals and the validations by mode. The compare switch resolves firing groups, and other edits don't. Prior art: the Monitor tool tests and the Monitors controller tests.
- **UI:**
  - the Monitors Playwright flow, extended to make a change Monitor, see its preview and its would-fire list, save it, Run now, see the page's Baseline and change, and see a refused save;
  - vitest for the change formatting and summary helpers.
- **Scope:** run only these targeted tests. The user runs the full suites.

## Out of Scope

- Anomaly detection: a band learned from several earlier ranges, such as the same hour on past weeks. It's a later effort, and this design shouldn't block it.
- An absolute-difference mode and free Baseline offsets.
- Charting the percent change itself over time.
- Making Create monitor from the explorer offer change mode.

## Further Notes

- Production had no Monitors as of 2026-10-22. Even so, every existing Monitor gets `compare = value`, so nothing changes for them.
- Two Queries per run roughly doubles a change Monitor's read cost on the 2-vCPU production VM. Keep that in mind when choosing intervals, and it's a reason the anomaly effort should look at one bucketed Query instead of many.
- No ADR: the choices are easy to reverse, and the alternatives are recorded here.
