# 07: Production timing and rollout

Spec: [../spec.md](../spec.md)

**What to build:** nothing new. Check the release on production with the user:

- Time every new route over 7 days on production's data, and opening a trace older than the local cache hours. Keep each run under a minute.
- Watch CPU and Logs read times as Tagalys turns tracing on one app at a time, following the spec's Tagalys rollout. Decide with the user whether to move to 4 vCPUs.

**Blocked by:** 03, 04, 05

**Status:** ready-for-human
