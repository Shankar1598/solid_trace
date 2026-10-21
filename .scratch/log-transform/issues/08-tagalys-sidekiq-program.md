# 08: The `tagalys` `sidekiq.log` program

**What to build:** With the user, write the `tagalys` Project's Log transform for `sidekiq.log` job lines, then rewrite Charts 23 to 26 on Dashboard 2 ("Sidekiq — job lines") to use its Attributes and move them back to Dashboard 1. See the spec's "Further Notes".

**Blocked by:** 07, and a release with the Log transform deployed to production

**Status:** ready-for-human

- [x] The program is previewed on recent arrivals, then saved by an admin.
- [ ] Charts 23 to 26 read the new Attributes and are back on Dashboard 1, which refreshes every minute without saturating the disk.
- [ ] Failures (`isnotnull(solidtrace.transform_error)`) are checked after a day.

## Comments

**2026-10-21, version 1 saved at 22:14:37 UTC.** The user's change to the Indexer VMs' Vector (infra `9c2d5e1`, not yet deployed) parses `sidekiq.log` itself, so the program gives the same Attributes, by the same regexes: `pid` (a number), `tid`, `worker_class`, `jid`, `bid`, `job_status` (`start`, `done` or `fail`) and `duration_seconds` (a number), with `time` and `level` from the line and `message` kept whole. A Log that arrives with `tid` was parsed by Vector and is left as it is, so the program does nothing once Vector's change is deployed and can then be emptied. Lines that aren't Sidekiq's (backtraces) are left as they are.

Previewed on 8 Logs given by hand (`done`, `fail`, `start`, a `WARN` with and without a worker, a backtrace line, a Log already parsed as Vector will, a `production.log` line), all as expected, and on 100 recent arrivals (none from `sidekiq.log`): no failures. Two minutes after saving: 23,841 Logs processed, none failed, 15.1 µs a Log; every `start` and `done` line in them had `job_status` and `tid`.

Charts 23 to 26 still search `message`. They can move to `job_status` once a Dashboard's range holds only Logs processed since the save (6 h for Dashboard 2's default range), and best once `job_status` is in a day's promoted set, which is picked from the day before's counts: from 2026-10-23 at the latest.
