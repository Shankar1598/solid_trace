# 08: The `tagalys` `sidekiq.log` program

**What to build:** With the user, write the `tagalys` Project's Log transform for `sidekiq.log` job lines, then rewrite Charts 23 to 26 on Dashboard 2 ("Sidekiq — job lines") to use its Attributes and move them back to Dashboard 1. See the spec's "Further Notes".

**Blocked by:** 07, and a release with the Log transform deployed to production

**Status:** ready-for-human

- [ ] The program is previewed on recent arrivals, then saved by an admin.
- [ ] Charts 23 to 26 read the new Attributes and are back on Dashboard 1, which refreshes every minute without saturating the disk.
- [ ] Failures (`isnotnull(solidtrace.transform_error)`) are checked after a day.
