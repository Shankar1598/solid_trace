# 13: Threshold Monitors with Integration notifications

**What to build:** Users create a Monitor with an aggregating Query, a comparator and threshold, a frequency and range in minutes, a no-data policy and a target Integration. A recurring Console job evaluates due Monitors, tracks state (ok, alerting, no data) and sends notifications on state changes through the existing Integration delivery path; the notifiers render Monitor payloads for Slack, PagerDuty and email.

**Blocked by:** 05

**Status:** wontfix

- [ ] `log_monitors` and `log_monitor_events` models; settings CRUD pages with Query validation requiring a `summarize` result
- [ ] `LogMonitorEvaluationJob` runs every minute from the recurring schedule, evaluates Monitors due by frequency over `[now - range, now]`, records an evaluation event and updates state
- [ ] Notification path generalised so a Notification's subject can be an Issue or a Monitor; new `log_monitor_triggered` and `log_monitor_resolved` payloads in all three notifiers
- [ ] No-data handling and the shared per-Integration rate limit documented in the UI
- [ ] Job tests with WebMock-stubbed EventStore responses cover ok→alerting→ok and no-data transitions; notifier payload tests

## Comments

2026-09-27: Closed as wontfix. This plan was superseded by the [logs map](../../logs/map.md). The file stays here as reference only.
