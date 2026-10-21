# 04: CPU profiles from production

**Status:** ready-for-human

**What to do:** Idea 10 of [performance-ideas.md](../research/performance-ideas.md), taken in [ticket 01](01-review-performance-findings.md). Let EventStore write CPU profiles to disk on production, so that ideas 2, 6, 11, 12 and 13 are weighed on measured figures, not estimates.

## Design

- **Off by default.** A setting in `event_store.yml`, such as `cpu_profile_dir`, turns it on.
- **When.** Twice an hour, a 30-second CPU profile (`runtime/pprof`): one starting just after hh:00, while the export runs, and one at hh:30, in steady ingest. Each is written as `cpu-<UTC time>.pprof`, and only the newest 48 are kept.
- **How to read one.** The directory sits on the data volume. Fetch a profile over SSH (`/home/shankar/Tagalys/apps/api/ssh 10.164.0.2 'sudo cat …'`) and read it locally with `go tool pprof -top`. There's no HTTP endpoint, so nothing new listens.
- **Not with a profile already running.** `pprof.StartCPUProfile` fails while another profile runs; then the profile is skipped and logged.

## Acceptance

- [x] With the setting, a test sees a profile written and the oldest past the limit removed; without it, nothing runs
- [x] The setting is documented in `deploy/event_store.yml`'s comments
- [ ] Once released and turned on, ticket 01's Comments get the top functions of an export-hour profile and a steady one, sorted by flat time and by cumulative time, and say which "Ask" or "Later" ideas the figures change

## Comments

### Built (2026-10-21, `feature/export-outside-write-lock`)

**What's built.**
- `pkg/cpuprofile` writes a 30-second profile at hh:00:30, while the export's COPY runs, and at hh:30:30, as `cpu-<UTC start>.pprof`. It keeps the newest 48.
- `cpu_profile_dir` in `event_store.yml` turns it on (`config.CPUProfileDir`). `main.go` starts it only when set, and stops it on shutdown, ending a profile in progress.
- A profile that can't start, for example because another is running, is skipped and logged.
- `deploy/event_store.yml` documents the key, commented out.

**What a profile can show.** DuckDB's own C++ threads appear as `runtime._ExternalCode`, because Go can't read their frames. A profile therefore splits CPU between DuckDB, and Go by function: JSON, msgpack, VRL calls and GC. That split is what ideas 2, 6, 11, 12 and 13 need.

**Tests.** The `pkg/cpuprofile` tests cover:
- the schedule;
- a profile written while only the newest are kept, leaving other files alone;
- no file left when another profile is running;
- Stop ending a profile in progress.

The config test covers off by default and on with the key.

**For the user, after a release.**
1. Add `cpu_profile_dir: /storage/profiles` to production's `/home/tglsadmin/solid_trace/deploy/config/event_store.yml`, and restart EventStore.
2. An hour later, fetch a profile over SSH (`/home/shankar/Tagalys/apps/api/ssh 10.164.0.2 'sudo cat /home/tglsadmin/solid_trace/deploy/data/storage/profiles/cpu-….pprof' > cpu.pprof`).
3. Read it with `go tool pprof -top cpu.pprof`.

The last box above waits on that.
