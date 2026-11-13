# Poteto (pstack) on shipping thousands of PRs a month

Source: [youtube.com/live/MN9dGgmLyso](https://www.youtube.com/watch?v=MN9dGgmLyso), "LIVE: Poteto (creator of pstack) on shipping 1,000's of PR's a month at SpaceX" (about 65 minutes). Lauren Tan (poteto) works on Grok Bot at "Cursor, which is now SpaceX AI". The host is the author of the `grill-me` and `wayfinder` skills, which this repo uses.

The notes come from the stream's English subtitles, fetched with yt-dlp on 2026-11-13; the captions read like a machine translation. pstack itself is [cursor/plugins/pstack](https://github.com/cursor/plugins/tree/main/pstack) (MIT).

## What she says

- **Verification is "hands and eyes"** (16:21). The agent can "run and interact with code like a normal user", and debug, take traces and snapshots. Here traces means Chrome DevTools performance traces and memory snapshots, not distributed tracing. "If an agent can't actually see the result of its work, it won't be able to iterate." It was the first skill she built at Cursor and the one that let her climb the "ladder of trust". Every app at Cursor/SpaceX AI now has a verification skill that is kept up to date automatically (18:54).
- **Turn the deterministic parts of a skill into a CLI** (19:24–24:25). Her verification CLI wraps Playwright, the Chrome DevTools Protocol and APIs, so agents stop rebuilding their own test harness on every run. Judgment stays with the agent and mechanical work goes to scripts.
- **The environment is the lever** (24:56–32:36). Watch how agents fail and turn each repeated failure into a lint rule, a constraint or a skill change. Her internal framework, "Dune", allows only one way to do each thing.
- **Inner and outer loops** (33:06–39:10). The inner loop is agents implementing. The outer loop brings context in from Slack, Linear, X and email: bots subscribed to channels send bug reports to a cloud **coordinator agent** (Cursor "projects"). It groups related reports, because several slightly different reports point to the real cause, and it delegates to subagents. "Where exactly am I the bottleneck?"
- **A buffer before fixing** (47:18). One routine looks for bad patterns and writes them to a document instead of fixing each one. Reviewing the batch every few days shows that they're "all the same thing".
- **Spot checks, not reviewing everything** (49:18). Sample PRs, look for patterns repeated across agents, and fix the environment, not the single agent.
- **Full autopilot** (51:51). Agents merge their own PRs. Several verification agents per PR launch the app, click around, look for regressions and fix them. She reviews merged PRs in the morning from the commit history.
- **"One-way doors"** (55:52). Merging without review only works where the work can be checked. Domains that are hard to verify can't do it yet.
- **Past transcripts are "a treasure trove"** (1:00:25). Have an agent review your transcripts for where you had to step in, and turn that into lint rules or skills. pstack's `recall` skill does this.

## For SolidTrace

- Her "hands and eyes" are mostly the agent driving the app (Playwright, CDP), which is a repository skill, not a backend. The "eyes" a backend gives an agent are Logs, traces and errors from a real environment, and SolidTrace's MCP already serves those.
- Her outer loop is bug reports from people. SolidTrace's Issues are bug reports from the system, already grouped by fingerprint: the same grouping she wants the coordinator to do.
- Her transcript review (`recall`, `reflect`, `correct`) is the improve stage done by hand on one laptop's history. It is what the map's stage 3 would do over every agent's runs.
