# 04: CPU profiles from production

**Status:** ready-for-agent

**What to do:** Idea 10 of [performance-ideas.md](../research/performance-ideas.md), taken in [ticket 01](01-review-performance-findings.md). Let EventStore write CPU profiles to disk on production, so that ideas 2, 6, 11, 12 and 13 are weighed on measured figures, not estimates.

## Design

- **Off by default.** A setting in `event_store.yml`, such as `cpu_profile_dir`, turns it on.
- **When.** Twice an hour, a 30-second CPU profile (`runtime/pprof`): one starting just after hh:00, while the export runs, and one at hh:30, in steady ingest. Each is written as `cpu-<UTC time>.pprof`, and only the newest 48 are kept.
- **How to read one.** The directory sits on the data volume. Fetch a profile over SSH (`/home/shankar/Tagalys/apps/api/ssh 10.164.0.2 'sudo cat …'`) and read it locally with `go tool pprof -top`. There's no HTTP endpoint, so nothing new listens.
- **Not with a profile already running.** `pprof.StartCPUProfile` fails while another profile runs; then the profile is skipped and logged.

## Acceptance

- [ ] With the setting, a test sees a profile written and the oldest past the limit removed; without it, nothing runs
- [ ] The setting is documented in `deploy/event_store.yml`'s comments
- [ ] Once released and turned on, ticket 01's Comments get the top functions of an export-hour profile and a steady one, sorted by flat time and by cumulative time, and say which "Ask" or "Later" ideas the figures change

## Comments
