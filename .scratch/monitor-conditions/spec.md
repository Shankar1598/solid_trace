# Monitor conditions

**Status:** ready-for-agent

Planned with the user on 2026-11-03/04. A Monitor can be muted by another Monitor, or notify only while another is firing, in the way Alertmanager's inhibition rules and Datadog's composite monitors work. The user ruled out running VRL (or any script) to decide. Instead the conditions are declarative, and each one is an ordinary Threshold monitor. Terms: **Monitor**, **Threshold monitor**, **Match monitor**, **Firing** and **ok**, **Notification** and **Integration** in [GLOSSARY.md](../../GLOSSARY.md). Monitors run as [ADR 0006](../../docs/adr/0006-monitors-evaluated-by-console-through-the-query-api.md) describes, and this effort keeps to it: Console evaluates every Monitor, and a run's states, Notifications and run record are written in one transaction.

## Problem Statement

A Monitor decides from one Query and one threshold, but whether something is worth a message often depends on something else:

- An error rate that doubles means nothing while traffic is near zero at 3 am.
- A spike in 5xx responses during a deploy is expected.
- A slow checkout matters to the on-call person only while checkout is actually busy.

None of that fits in one Query. Members either live with messages they learn to ignore, or raise thresholds until real incidents slip by. Wanting "fire on this, unless that" or "fire on this, but only while that" is common, and today the only answer is to bend the Query.

## Solution

A Monitor gains **conditions**. Each condition names another Threshold monitor in the same Organization, in one of two kinds:

- **Mute while** B is firing: the Monitor sends no firing message while B fires.
- **Only while** C is firing: the Monitor sends a firing message only while C fires as well.

A condition can be matched by columns, such as "for the same `service`". Then only a group of B (or C) with the same `service` value counts for a group of the Monitor.

Conditions decide only whether a Monitor **notifies**. The Monitor's groups still move between firing and ok exactly as they do today. A firing group that its conditions hold back is **muted**: the Monitor's page shows it as firing (muted by B), and its runs count it. When the mute lifts and the group is still firing, the firing message is sent then, saying how long the group has been firing. A resolve is always sent for a group whose firing message went out, muted or not, so a PagerDuty incident never stays open. A group that resolves without ever having notified sends nothing.

A condition that can't be read counts as not firing: its Monitor is off, or its last run failed. A broken mute rule therefore never silences a real alert. A broken only-while rule keeps the Monitor muted, and the Monitor's page warns about it.

A check like "Checkout traffic is low" or "Deploy in progress" is an ordinary Threshold monitor with no Integrations. It keeps its own state and history and sends nothing.

## User Stories

1. As a member, I want to add a "mute while" condition to a Monitor naming another Threshold monitor, so that it stops sending firing messages while that Monitor fires.
2. As a member, I want to add an "only while" condition to a Monitor naming another Threshold monitor, so that it sends firing messages only while that Monitor also fires.
3. As a member, I want to add several conditions to one Monitor, so that I can combine "mute while a deploy is in progress" with "only while traffic is high".
4. As a member, I want any firing mute condition to mute the Monitor, and every only-while condition to be needed, so that combined rules mean what they say.
5. As a member, I want to match a condition by one or more columns, such as `service`, so that low traffic on one service mutes only that service's group.
6. As a member, I want a condition without matched columns to count whenever any group of its Monitor fires, so that one "Deploy in progress" Monitor can mute every group.
7. As a member, I want to pick conditions from any Project in my Organization, so that a deploy Monitor on one Project can mute Monitors on others.
8. As a member, I want to make a Threshold monitor with no Integrations to use as a condition, so that it keeps state without sending anything.
9. As a member, I want a muted group to stay firing in the Monitor's state, so that what the Monitor sees and what it sends are kept apart.
10. As a member, I want the Monitor's page to show each muted group as firing and muted, with the Monitor(s) muting it, so that I can see why no message came.
11. As a member, I want a group's firing message sent when its mute lifts while it is still firing, so that an incident that outlasts a deploy still reaches me.
12. As a member, I want that late firing message to say when the group started firing and what muted it, so that I know it isn't new.
13. As an on-call engineer, I want a resolve sent for every group whose firing message was sent, even while the group is muted, so that PagerDuty incidents always close.
14. As an on-call engineer, I want no resolve for a group that never notified, so that I don't get resolves for things I never heard about.
15. As a member, I want a mute that starts while a group has already notified to send nothing, so that the open incident stays open until the group resolves.
16. As a member, I want a condition whose Monitor is off, or whose last run failed, to count as not firing, so that a broken mute never silences a real alert.
17. As a member, I want the Monitor's page to warn when one of its conditions is off or failing, so that I notice a broken only-while condition holding every message back.
18. As a member, I want each run to record how many firing groups it muted, so that the run history shows muting as well as firing.
19. As a member, I want Run now to apply the conditions as a scheduled run does, so that I can check them at once.
20. As a member, I want Run now's notice to say how many groups are firing and how many of them are muted, so that the result is clear.
21. As a member, I want the form's preview of groups to say which groups the conditions would mute right now, so that I can check a rule before saving it.
22. As a member, I want a Match monitor to take conditions too, so that "errors matching X" can be muted during deploys.
23. As a member, I want a muted Match run to send nothing for the Logs it read and still move on, so that the Logs aren't sent in a burst when the mute lifts.
24. As a member, I want the condition's Monitor page to list the Monitors that use it as a condition, so that I know what I affect when I change it.
25. As a member, I want saving to refuse a condition naming the Monitor itself, so that I can't make a Monitor mute itself.
26. As a member, I want saving to refuse a condition that would make a cycle through other Monitors' conditions, so that conditions always form a clear chain.
27. As a member, I want saving to refuse a Match monitor as a condition, so that I only name Monitors that can be firing.
28. As a member, I want saving to refuse a Monitor from another Organization, so that conditions stay inside the Organization.
29. As a member, I want saving to refuse matched columns unless both Monitors notify by group, so that matching has groups to match.
30. As a member, I want saving to refuse a matched column that either Monitor's Query doesn't group by, when that can be known, so that a typo doesn't make a rule that never matches.
31. As a member, I want saving to refuse naming the same Monitor twice in one Monitor's conditions, so that one Monitor can't both mute and be required.
32. As a member, I want saving a condition's Monitor to be refused when the change would break a rule using it (making it a Match monitor, turning off notify by group, or dropping a matched column from its Query), so that rules don't break silently.
33. As a member, I want deleting a Monitor that is a condition of others to be refused, naming those Monitors, so that an only-while rule can't hold a Monitor back forever.
34. As a member, I want to turn a condition's Monitor off even while it is in use, so that I can pause it; it then counts as not firing.
35. As an admin, I want deleting a Project to remove the conditions naming its Monitors from Monitors in other Projects, so that deleting a Project isn't blocked by rules elsewhere.
36. As a member, I want changing a Monitor's conditions to leave its groups' states alone, so that editing a rule doesn't resolve open incidents.
37. As a member, I want a group that is firing and muted to notify on the next run after its mute is removed by editing, so that removing a rule takes effect at once.
38. As a member, I want the no-data group of a Monitor that alerts on no data to follow the same conditions, with matched columns never matching it, so that no data behaves predictably.
39. As an AI agent, I want `create_monitor` and `update_monitor` to take a Monitor's conditions, so that I can set up muting over MCP.
40. As an AI agent, I want `get_monitor` to give a Monitor's conditions, the Monitors using it as a condition, and which groups are muted and by what, so that I can explain why an alert didn't fire.
41. As an AI agent, I want `run_monitor` to say how many groups are muted, so that I can check a rule over MCP.
42. As an AI agent, I want `delete_monitor` to say why deleting a condition's Monitor was refused, so that I can remove the rules first.
43. As a member, I want the Monitors list to show a muted count beside a Monitor's firing count, so that a muted Monitor doesn't look quiet.
44. As a member, I want Monitors that existed before conditions to keep sending exactly what they sent before, so that nothing changes until I add a rule.

## Implementation Decisions

**Where it runs.** Everything stays in Console's Monitor evaluation, as ADR 0006 says. A run reads its conditions' Monitors' states from Console's database, inside the transaction it already writes in. It doesn't run them and asks EventStore nothing more, so a run's 30 s budget is unchanged. A condition's state is as fresh as its own last run, so a condition that must be quick, such as "Deploy in progress", wants a short interval. Nothing re-runs a Monitor when one of its conditions changes; the change takes effect at the Monitor's next run.

**Schema.**
- A new `monitor_conditions` table: the Monitor, the condition's Monitor, its kind (`mute_while` or `only_while`), matched columns (a JSON array of column names, empty for none), a position for display, and timestamps. Each (Monitor, condition's Monitor) pair is unique. The condition's Monitor is a foreign key that restricts deletion; the Monitor's own rows go when it is deleted.
- `monitor_states` gains `notified` (whether this firing group's firing message was sent; false by default) and `muted_by` (the condition Monitors holding a firing group back, as their ids, empty when not muted). The migration sets `notified` to true on every firing state, so existing incidents resolve as before (story 44).
- `monitor_runs` gains `muted`: how many firing groups a Threshold run muted, or for a Match run, whether its matched Logs were muted.
- At most 10 conditions per Monitor.

**When a condition counts as firing, for one group of the Monitor.** The condition's Monitor must be on, its last run must have no error, and it must have a firing state. Without matched columns, any firing state counts. With matched columns, the state's group must have the same value for every matched column as the Monitor's group. Group keys are already the group's columns, name to value, as JSON, so both sides parse back into columns. A group with no columns (the one group of a Monitor that doesn't notify by group, or no data) never matches a condition with matched columns.

**Whether a firing group notifies.** It notifies when no mute-while condition counts as firing and every only-while condition does. Otherwise it is muted, and `muted_by` records the mute-while Monitors that count, and the only-while Monitors that don't.

**State moves.** Firing and ok are decided exactly as today; conditions change only what gets sent:
- Ok → firing: send monitor firing if it notifies (`notified` = true); otherwise record it as firing and muted (`notified` = false).
- Firing, not notified, now notifies: send monitor firing (`notified` = true). Its payload carries the group's `since` and the Monitors that had muted it, so the message can say it has been firing since then.
- Firing and notified: nothing is sent, whether or not it is muted now (story 15). Only `muted_by` is updated, for display.
- Firing → ok, or the group's row is gone: send monitor resolved only if `notified`.
- Turning a Monitor off, deleting it, making it a Match monitor or switching its comparison resolves its firing groups as today. Each sends a resolve only if it notified.
- Saving a Monitor's conditions doesn't resolve anything (story 36). The save still changes the Monitor's version, so a run that was going while it saved writes nothing, as today.

**Match monitors.** A Match monitor's conditions have no matched columns, because it has no groups. Saving refuses matched columns on one. A run whose conditions hold it back writes no monitor matched Notification, still moves its cursor, and records `muted`. A `logs_skipped_at` waiting to be reported stays for the next message that is sent.

**Refusals at save** (the Monitor's validations, as its other rules are):
- The condition's Monitor must be in the Monitor's Organization, must be a Threshold monitor, and must not be the Monitor itself.
- No cycle: following conditions from the condition's Monitor must never reach the Monitor.
- No Monitor named twice.
- Matched columns need both Monitors to notify by group. Each matched column must be a by column of both Monitors' summarize, when EventStore's Query validation can tell (the check the Monitor page already makes for by columns). When it can't tell, the columns are taken as given.
- Saving a Monitor that is a condition of others checks each of their rules again (story 32) and refuses with the rules it would break.

**Deletion.** Deleting a Monitor that is a condition of others is refused, naming them; this was decided with the user. Turning it off is allowed. Deleting a Project first removes every condition naming the Project's Monitors, including those on Monitors in other Projects, so the Project's Monitors can then be deleted as today.

**Messages.** The monitor firing message for a group that was muted says it has been firing since its `since` and names the Monitors that muted it. The other messages are unchanged. Slack, PagerDuty and email all read this from the payload. PagerDuty dedup keys are unchanged.

**Console surfaces.**
- *Form:* a Conditions section. Each row picks "Mute while" or "Only while", a Threshold monitor of the Organization (by name, with its Project), and optionally "for the same" columns, offered from the Monitor's by columns. The form's preview of groups (`preview_groups`) says, for each group that would fire, whether it would be muted and by what, from the conditions' current states.
- *Show page:* the conditions, each with its Monitor's current status (linked), and a warning on one that is off or whose last run failed. The groups table shows "Firing · muted by B". There is a "Used as a condition by" list, and the run history shows muted counts.
- *Index:* a muted count beside firing.
- *Run now notice:* the run's `found` text, such as "3 groups firing, 1 muted" or "4 Logs matched, muted".

**MCP.**
- `create_monitor` and `update_monitor` take `conditions`: a list of `{monitor_id, kind, match_columns}` that replaces the Monitor's conditions when given and leaves them alone when left out.
- `get_monitor` gives the conditions, "used as a condition by", and each group's `muted_by`. `list_monitors` gives the muted count.
- `run_monitor`'s answer includes the muted count, and `delete_monitor` passes the refusal on.

**Docs.**
- GLOSSARY gains **Condition** (a rule on a Monitor naming another Threshold monitor: mute while or only while, optionally matched by columns) and **Muted** (a firing group, or a Match run, that its conditions hold back from notifying).
- The **Firing / ok** entry says conditions don't change it.
- `docs/logs/monitors.md` gains a Conditions section.
- No new ADR is needed: evaluation stays where ADR 0006 put it.

## Testing Decisions

A good test runs a Monitor, or saves one, the way a member or an Integration would see it. It looks at the Monitor's states, its runs, the Notifications written and what saving or deleting answers, never at how evaluation is put together. The seams were agreed with the user:

- **`MonitorEvaluation.run` with EventStore stubbed** carries most of the behaviour, in the existing Monitor evaluation test (WebMock, `MonitorTestHelper`, the fixed clock, `notifications` and `assert_state`). A condition's state is set by running the condition's Monitor through the same seam, or by writing its state directly when only its state matters. Cases:
  - mute-while and only-while, alone and combined
  - matched columns: matching, not matching, and a group without the columns
  - a condition that is off, or whose last run failed
  - the mute lifting while the group is still firing (a late firing message with `since` and `muted_by`)
  - a resolve while muted after notifying, and a resolve without ever notifying
  - a mute starting after notifying
  - no data
  - a Match monitor muted, with its cursor still moving
  - Monitors without conditions sending exactly as before
- **Saving and deleting a Monitor** for the refusals, in the existing `LogMonitor` model test: another Organization, a Match monitor as a condition, itself, a cycle of two and of three, a Monitor named twice, matched columns without notify by group or not among the by columns, a condition's Monitor saved into breaking a rule, a refused delete, and a Project delete removing the rules.
- **The existing MCP Monitor tools test** for `conditions` on create and update, what `get_monitor` gives, and `delete_monitor`'s refusal.
- **One Playwright flow** in `monitors.spec.ts`: make a "Deploy in progress" Monitor with no Integrations and a Monitor muted while it fires, Run now both, and see the group shown firing and muted with no Slack message. Then raise the condition's threshold, Run now both, and see the firing message arrive.
- Also prior art: the Integration notification Monitor test for what the late firing message says, and the Monitors controller test if the controller gains anything beyond passing conditions through.

Run targeted tests; the user runs the full suites.

## Out of Scope

- Running VRL or any script to decide whether to notify (ruled out by the user).
- Expressions over several Queries' results, as in PromQL's `and` / `unless` with vector matching.
- Extra check Queries defined inside a Monitor. A check is always its own Monitor.
- Re-running a Monitor when one of its conditions changes state.
- Mute windows by time, for planned maintenance.
- Anomaly detection (a later effort, as the % change spec noted).
- Conditions on Issue notifications (new Issue, threshold reached).
- Mute or only-while conditions naming a Match monitor.

## Further Notes

- Alertmanager's inhibition rules are the model for muting, and Datadog's composite monitors for "only while". Unlike Alertmanager, muting here never withholds a resolve for a firing message that was sent, because PagerDuty incidents must close.
- An only-while condition whose Monitor is off or failing mutes the Monitor. That's the price of failing open on mute-while. The Show page warning (story 17) is how a member finds out.
- Because a condition is read at the Monitor's run, a firing message can trail the mute lifting by up to the Monitor's interval, plus the Integration's one-message-a-minute rate limit.
- A good first ticket split: (01) mute-while and only-while on Threshold monitors end to end through `MonitorEvaluation.run`, with the schema, the refusals, the Show page and MCP; (02) matched columns; (03) Match monitors taking conditions; (04) the form's preview and the late firing message's wording. 02 to 04 are blocked by 01 only.
