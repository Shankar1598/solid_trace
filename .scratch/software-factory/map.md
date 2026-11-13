# SolidTrace Software Factory

Label: wayfinder:map

## Destination

A spec and first-milestone tickets for the SolidTrace factory. It is an open-source, self-hosted product that other teams, Tagalys first, use as the framework and infrastructure for their own software factories. SolidTrace records and shows every agent run. A factory module in this repo dispatches agents onto a Nomad cluster. Every decision in the spec traces to a resolved ticket on this map, and the hard-to-reverse ones become ADRs. The destination is documents, not code.

## Notes

- **Origin.** Asked for by the user on 2026-11-13. The inspiration is Zach Lloyd's (Warp) talk "How to Build a Software Factory That Improves Itself" (`~/.cache/solid-trace/software-factory-notes/warp-talk.md` (local only)). The map was re-charted the same day from [research/fable-opinion.md](research/fable-opinion.md), the write-up of a session where the user settled the goal, the runtime and the licence. That note's section 3 holds candidate recommendations not yet put to the user; tickets cite it rather than restate it.
- **Two halves.**
  - **The record:** ingest, store, search, score and show. This is a data workload on stored runs, which SolidTrace already does for Logs, Spans and Issues. It belongs in Console and EventStore.
  - **The runtime:** the factory module, on Nomad, never on the SolidTrace instance. SolidTrace itself still runs on one machine with no cluster, as the README says.
- **SolidTrace has no OTLP ingest today.** EventStore takes Sentry envelopes and JSON/NDJSON Logs only (`event_store/handler/routes.go`).
- **Transcripts don't fit a Log as-is.** A Log's strings are cut at 64 KiB and a Log over 1 MiB is dropped. This repo's own transcript lines reach 1,125 KB ([research/fable-opinion.md](research/fable-opinion.md), section 4).
- **The `spans` table is Sentry-shaped.** OTLP spans would need defaults for `group_hash`, `self_time_us` and `sample_rate` ([research/agent-transcripts.md](research/agent-transcripts.md)).
- **Production capacity.** The production VM is 2 vCPU and already busy. Agent telemetry is small next to Tagalys' Logs, but transcripts are large.
- **Dogfooding.** SolidTrace's own agents (`.scratch` tickets, worktree implementers, review agents) can be the record's first data, but building SolidTrace with a factory is not the goal.
- **Skills.** Grilling tickets call `grilling` and `domain-modeling`. Prototype tickets call `prototype`. Research tickets run as subagents calling `research`, with notes in `research/`. Flows and prototypes that touch ClickHouse run on a worktree stack through `run-flow.sh` only.

## Research so far

- Zach Lloyd's talk, `~/.cache/solid-trace/software-factory-notes/warp-talk.md` (local only): the factory is infrastructure (any model, any harness, defined as code, on your own compute and data). Every run is stored and searchable; on top sit an overview, LLM-as-judge evals, self-improvement PRs and benchmarks. "Overkill for an individual"; for "a team of maybe 10 or more engineers".
- [Poteto (pstack) stream](research/pstack-stream.md): verification ("hands and eyes") first, then the environment (lint rules, skills, deterministic parts in a CLI), then the outer loop of triggers.
- [Laurie Voss's talk](research/product-engineer-talk.md): the bottleneck moves from code to review to defining what "good" means. "The last reviewer is production."
- Warp, `~/.cache/solid-trace/software-factory-notes/warp.md` (local only): Warp Factories (Early Access since 2026-08-27), `factory.yaml`, per-run Docker sandboxes, Scorers, self-improvement after 25 failures or 7 days, benchmarks at pinned SHAs. Gaps: a closed control plane that always keeps run metadata, pull-only exports, and a $3/agent-hour fee even on your own workers.
- [Open-source alternatives](research/open-source-alternatives.md): no project to adopt whole. Symphony's SPEC is a useful dispatcher design. Its per-VM runtime picks (OpenHands Agent Server, Rivet) are superseded by Nomad.
- [Agent transcripts](research/agent-transcripts.md): capture in order OTLP logs, then a raw transcript upload, then OTLP traces. Claude's JSONL is internal; `stream-json` is documented. One redaction point on the server. Two Codex points are corrected by [research/transcript-capture.md](research/transcript-capture.md): Codex does emit cost (`codex.turn_cost`), and its `[otel] trace_exporter` key exists.
- [A second opinion](research/fable-opinion.md): the user's decisions, the accepted shape, and candidate recommendations for the open tickets.

## Decisions so far

<!-- one line per resolved ticket: [title](issues/NN-slug.md): gist -->

- [What is the factory, and who is it for?](issues/01-what-the-factory-is-and-who-its-for.md): a Warp Factories-like product for other teams, Tagalys first; open source and self-hosted only. SolidTrace stores and shows (OTLP, sessions, scores, webhooks, the release join); the factory module acts (dispatcher, triggers, secrets, GitHub App). Console is the one UI and sign-in.
- [Where do agents run, and where does the factory's code live?](issues/02-where-agents-run-and-where-the-code-lives.md): never on the SolidTrace instance; on Nomad clients. The factory is a separable module in this repo, its own image, reaching SolidTrace only through ingest, MCP and OAuth. The Nomad server may sit beside Console at the operator's choice.
- [Which licence does SolidTrace take?](issues/03-licence.md): AGPL-3.0 for now, chosen knowing it doesn't forbid selling as a service; the user holds the copyright. `LICENSE` and the README section are in the working tree, not committed.
- [Do any dependencies conflict with AGPL-3.0?](issues/14-agpl-dependency-conflicts.md): nothing linked, loaded or bundled conflicts (113 Go modules, 256 VRL crates, 131 gems, 185 browser packages; `vrl` is MPL-2.0, which combines). One fix: Console's image ships Brakeman (non-commercial licence) because `BUNDLE_WITHOUT="development"` keeps `:development, :test` gems; it should be `development:test`. No third-party notices file yet.
- [How can a run be isolated on Nomad?](issues/09-run-isolation-on-nomad.md): Docker on agent-only clients by default, gVisor (`runsc`) as the stronger option; no maintained microVM driver. One allocation per run: `checkout`, then `agent` with no forge credential, then `publish` pushing with a one-hour single-repo token from a broker. The model key stays in a gateway. Nomad Variables can't hold per-run secrets, and payload and meta are readable with the job. Egress drops the metadata server; an allowlist needs a proxy. Restart and reschedule set to 0; a timeout reads as `complete`.
- [How can a transcript be captured, on a laptop and in a Nomad task?](issues/07-transcript-capture.md): only the on-disk transcript directory is complete (Claude: `<session>.jsonl` plus `subagents/`, including `subagents/workflows/`, and `tool-results/`; Codex: one rollout file per thread). Upload new bytes only. Laptops: hooks (`Stop`, `SubagentStop`, `SessionEnd`, a sweep at `SessionStart`) beside OTLP logs from managed settings. Nomad: an entrypoint wrapper uploads every few seconds and stops the agent with SIGINT. Joins use Claude's `session.id` (fixable with `--session-id`) and Codex's thread id. Codex's `--json` stream omits subagent threads.

## Not yet specified

- **Triggers.** GitHub first? Then Slack, Linear, cron, Monitors and Issues. Which ship with the run stage hangs on the first product and the first team's tracker.
- **Scorers and human grades.** Where a Scorer runs, whose model keys it uses, how runs are sampled, and how a human grades a run. Lloyd says Warp has no human grading.
- **Evidence.** If the first product makes it a concept: what a run's Evidence holds (test results, screenshots, recordings, the spec) and where it's stored.
- **The join to production.** How a release names the runs behind it, and how SolidTrace says whether an Issue stopped after a fix, or which runs brought new ones.
- **Console's factory pages.** The sessions page, a run's transcript view, and the factory Dashboards. Likely prototype tickets once a Run is defined.
- **Self-improvement and benchmarks.** The PR a Monitor on Scorer failures starts, and replays of recorded tasks across models.
- **The README.** Its tagline and its "no orchestration" line once the factory module exists.
- **Remaining words.** Factory, Automation, Scorer, Evidence, Dispatcher, as their tickets reach them.

## Out of scope

- **A hosted service.** Everyone self-hosts ([What is the factory, and who is it for?](issues/01-what-the-factory-is-and-who-its-for.md)).
