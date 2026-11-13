# Software factory: decisions and a second opinion (Fable, 2026-11-13)

This note is the handoff from a session with the user on 2026-11-13. It is written for a wayfinder session that starts from it with no other context. It holds:

1. what the user decided, in their words;
2. recommendations the user accepted;
3. recommendations not yet put to the user;
4. the evidence behind them;
5. where [the map](../map.md) is now out of date;
6. the open questions.

It replaces `~/.cache/solid-trace/handoff-software-factory-opinion.md`, whose "form an independent view" task is done.

**How sure each part is.** Section 1 is the user's own decisions. Section 2 was answered "makes sense". Section 3 is my judgment and is still to be grilled. In section 4, "checked" means I ran or read it myself in this session; everything else comes from the earlier session's research notes and was not checked again.

## 1. What the user decided

**The goal.** It is a product, not a way of developing SolidTrace.

> "I am not talking about building solid trace further using factories. I am talking about building a product like Warp.dev so that other teams, eg Tagalys and many other larger teams can use this as a framework and infra layer for their own factories."

The original request, from the earlier session:

> "We should become a repository where we deploy multiple agents into multiple VMs and all the data is collected, all the transcripts are flowing to our system stored in ClickHouse and we can analyze that and basically orchestrate a factory."

**Agents never run on the SolidTrace instance.**

> "The solid trace instance will be busy ingesting and processing logs from prod. we cannot run its harness in the same instance. we will need to run this on a cluster of pods."

**One repository, and Nomad.**

> "we should put the factory module into this repo itself and use it as a mono repo. we should use Nomad for the dev cluster instead of K8s. people can choose to run Nomad server along with solid trace console or separately based on what the cpu load comes out to be."

**Open source, self-hosted only.**

> "we are not gonna offer a hosted service right. this is gonna be open source.."
>
> "everyone will self host this"

**Licence: AGPL-3.0, for now.**

> "i dont want someone to take this project and sell it as a service to others"
>
> "lets do agpl for now"

- I told the user that AGPL discourages selling it as a service but doesn't forbid it: a host must only publish their changes. The Functional Source License (Sentry's) would forbid it, but isn't open source. The user chose AGPL knowing that, so it may be revisited.
- Done in the working tree, not committed: `LICENSE` (the AGPL-3.0 text from gnu.org) and a "Licence" section at the end of `README.md`, with "Copyright © 2025–2026 Shankar Thyagarajan."

**Ownership.** The user holds the copyright personally. They work at Tagalys, where SolidTrace is deployed; Tagalys is the first user, not the owner.

## 2. Recommendations the user accepted

The user answered "makes sense" to the shape below, then made the monorepo and Nomad decisions on top of it.

**One rule divides the work: SolidTrace stores and shows; the factory acts.**

- **SolidTrace gains observability features,** each worth having with no factory:
  - an OTLP receiver;
  - agent sessions as a fourth signal beside Issues, Logs and traces, with a sessions page;
  - scores as stored data, with starter Dashboards;
  - a webhook Integration and scoped tokens;
  - the join from a release to the runs behind it.
- **The factory module holds anything that acts on another system or runs code:** triggers, the dispatcher, secrets, the GitHub App, the factory repository sync, and the Scorer and self-improvement jobs that call a model.

**Three pieces:**

| Piece | Runs on | Module | Holds |
|---|---|---|---|
| Agent allocations: one per run, with a harness such as Claude Code | Nomad clients | Factory | The run's secrets, for its lifetime |
| Dispatcher: triggers, GitHub App, factory repository sync. Nomad is the queue. | One small service, beside the Nomad server | Factory | Run state and secrets, in its own small database |
| Record and UI: sessions, transcripts, cost, scores, Dashboards, Monitors | The SolidTrace instance | Console and EventStore | Everything recorded; users, Organizations and sign-in |

**Console is the one UI and the one sign-in.**

- The factory has no dashboard, users or Organizations of its own. It signs users in through SolidTrace, which the planned OAuth effort makes possible.
- Console gets the Warp-like pages, the viewing side first: runs, transcripts, cost and scores are stored data. They appear when a factory is connected.
- Starting, cancelling and approving runs go through GitHub, Slack and the dispatcher's API first.
- A trade-off the user was told about: if Console later gets start and cancel buttons, a compromised Console could start runs. It would still hold no repository credentials.

**What keeps the module separable inside one repo:**

- It imports nothing from `console/` or `event_store/`, and reaches SolidTrace only through public ingest, MCP and OAuth.
- It ships as its own image, off unless the operator turns it on, in its own top-level directory.
- No Nomad client runs on the SolidTrace machine, or agents would be placed there.
- Co-locating the Nomad server with Console joins them at the machine level: that server holds the agents' secrets and can run code on every client. It stays the operator's choice, and the docs should say so.

## 3. Recommendations not yet put to the user

These are mine. Treat each as a candidate decision.

**Enter through the record, not the runtime.**

- Most of what Lloyd shows on screen is queries over stored runs: the run list, cost and cycle time, scores, self-improvement PRs, the cost-against-quality chart. Orchestration gets one diagram.
- A team gets value before it changes how it works: point every engineer's Claude Code and Codex at SolidTrace through managed settings, and leadership sees sessions, cost and tool use. That is the governance problem Lloyd opens with.
- It is open ground. Warp doesn't track local third-party CLI sessions as runs, and keeps run metadata on its own cloud. Agent vendors will keep absorbing the runner, but won't offer a neutral record across each other's agents on the customer's machine.

**Build order.**

1. **Record.**
   - OTLP/HTTP receiver, logs first, then traces.
   - Full transcript capture beside it, because OTLP cuts tool input at 512 characters.
   - A Run entity, a sessions page with the transcript, starter Dashboards, MCP tools, redaction.
2. **Measure.**
   - A GitHub App: run to PR to merge.
   - Scorers and human grades. Lloyd says Warp hasn't added human grading.
   - Cost per PR, cycle time and autonomy as Dashboards; the release join.
3. **Run.** The factory module starts here.
   - Nomad jobs for agents, the dispatcher, triggers (webhook, cron, GitHub, Monitor, Issue), secrets, the factory repository sync.
   - Evidence on runs.
4. **Improve.**
   - Self-improvement PRs, then benchmarks that replay recorded tasks across models.

**Hardening is part of the product, not a later phase.** The product holds repository write credentials and transcripts full of secrets, for larger teams, on a 0.x codebase with one maintainer. OAuth with scoped, expiring machine tokens, SSO and an audit log should land before stage 3 ships to a team.

**What Nomad takes off the build.**

- **A run is a dispatch of a parameterized batch job.** The job is the agent's template (image, resources, harness command), and `nomad job dispatch` with meta is one run. Nomad queues, places, retries and times it out.
- **Cron automations are periodic jobs, and secrets are Nomad Variables.** Run state comes from Nomad's event stream.
- **There is no worker to write.** The Nomad client is the worker. The task's entrypoint runs the harness, points its OTLP at SolidTrace and uploads the transcript when it ends.
- **Limit (checked in Nomad's docs):** a dispatch payload is at most 16 KiB, so a run gets its ticket or prompt by reference.
- **Keep the Nomad calls behind one small interface** (dispatch a run, watch its state, store a secret). Nomad is source-available, not open source, and many larger teams standardise on Kubernetes. Don't build a second backend now.

**Don't build what has an owner.**

- No agent or harness: drive the vendors' documented headless modes (`claude -p`, `codex exec`).
- No inference gateway: teams bring keys or their own gateway, and cost comes from what the harness reports.
- No task tracker: GitHub Issues and Linear are the intake.
- No live steering of a running agent in the first product.
- No definition format where one exists: agents and skills stay in the harnesses' own files.

**The improvement layer is mostly composition.**

- A Scorer is a sampled LLM judge. Its verdicts are stored like other data, so Dashboards chart them and Monitors fire on them.
- A Monitor firing is a trigger, and a trigger starts a run. Self-improvement is "a Monitor on Scorer failures starts an agent that opens a PR against the factory repository".

**The join to production is the reason to do this in SolidTrace.**

- An Issue or a Monitor starts a triage run. The agent arrives with a scoped MCP token for that Project, so it sees production from its first turn.
- After the release, SolidTrace says whether the Issue stopped, and which runs' changes brought new ones. Events already carry `release`. Warp's numbers stop at the merge.

**Verification is a product concept.** A run carries Evidence: test results, screenshots, recordings, the spec it was checked against. All three speakers call this the gate, and the map has no place for it.

**Transcripts as Logs, to be prototyped.** Store transcript lines as Logs in an agent Project, with oversized values in the bucket by reference. That keeps Retention, tiering, grants, the Query language, Dashboards and Monitors for free. The earlier note wants new `agent_messages` tables instead. Decide with real transcripts through both.

**Licence follow-ups.**

- The README says "version 3", not "version 3 or later".
- A footer link to the source in Console, so a modified install can meet AGPL's offer of source.
- A scan of dependencies for licences that conflict with AGPL. Not done.
- MIT for the files that run beside a team's own code (the run entrypoint, example job files, templates), when the module adds them.
- A contributor agreement before outside contributions, so the user stays able to relicense.
- I suggested the user confirm that their employment agreement with Tagalys doesn't assign this work to Tagalys.

## 4. Evidence

**The talks, read from the original-English captions.**

- The earlier talk notes were made from YouTube's `en` track, a machine translation. The `en-orig` track is clean. My copies are in `~/.cache/solid-trace/software-factory-transcripts/` (outside the repo, since they are others' words).
- **Lloyd (Warp)**, points the earlier notes missed:
  - Who it is for (21:23): "overkill for an individual", who should use a Claude Code subscription; valuable for "a team of maybe 10 or more engineers".
  - Verification (18:06): the failure mode is a pile-up of PRs. His answers are spec-driven development and computer-use verification.
  - His closing idea (19:13): "set up something that you can measure and improve".
- **Poteto** never mentions a telemetry backend or a fleet. Her order: verification ("hands and eyes"), then the environment (lint rules, one way to do a thing, deterministic parts moved into a CLI), then the outer loop of triggers, then agents merging their own work while she samples merged PRs. "The prerequisite to a very high volume of pull requests is the environment" (33:23).
- **Voss:** the bottleneck moves from code to review to defining what good means. Review only high blast-radius code, automate what is cheap to verify, put judgment into specs and evals, and "the last reviewer is production".
- **Read together:** Lloyd describes the platform. Poteto and Voss describe what a team must do on it. The product has to make those things easy; it can't do them for the team.

**This repo's own agent transcripts (checked).** From `~/.claude/projects/*solid-trace*`, kept 30 days, so since about 2026-10-21:

- 608 files, 534 of them subagent transcripts; 1.7 GB; 232,318 lines.
- 5,956 lines (2.6%) are over 64 KiB, 2,199 over 100 KiB, and one is over 1 MiB (the largest is 1,125 KB).
- About half a gigabyte a week for one heavy user. Fifty such engineers would add about 100 GB a month to production's 6.7 TiB.

**SolidTrace's code (checked).**

- No OTLP route: `event_store/handler/routes.go` has Sentry `store` and `envelope`, and JSON Logs.
- Log ingest flattens nested objects to dotted Attributes up to 10 levels, reads `timestamp` as the time, keeps arrays as JSON, cuts a string at 64 KiB and drops only a Log over 1 MiB (`event_store/ingest/normalise/normalise.go`). I did not run transcripts through it.
- Integrations are email, Slack and PagerDuty (`console/app/models/integration.rb`). They only notify out.
- API tokens have no scopes or expiry (`docs/mcp.md`).
- Console already schedules and runs Monitor evaluations as jobs (`console/app/jobs/`).
- No SSO and no audit log. No test CI: `.github/workflows/` has `release.yml` and `scan-images.yml`.

**Nomad's licence (checked, licence file on `main`).** Business Source License. Production use is allowed unless you offer Nomad "to third parties on a hosted or embedded basis in order to compete with IBM Corp's paid version(s)". A "competitive offering" is a product "offered to third parties on a paid basis … that significantly overlaps with the capabilities" of paid Nomad. A free, self-hosted factory is neither, so it is not a blocker. Each version becomes MPL 2.0 after four years. Don't ship Nomad inside our images.

**From the research notes, not checked again.**

- Claude Code's OTel events, content limits and transcript format: [agent-transcripts.md](agent-transcripts.md).
- Warp's products, Scorers, pricing and gaps: `~/.cache/solid-trace/software-factory-notes/warp.md` (local only).
- The orchestrator field and its churn (Coder removed Tasks, Vibe Kanban and Terragon shut down, Daytona went private): [open-source-alternatives.md](open-source-alternatives.md).

## 5. Where the map is out of date

I did not edit [map.md](../map.md) or `STATUS.md`. Against section 1:

- **Notes, "Two halves":** it calls the runtime "new ground" that "runs against the README". The runtime is now a factory module on Nomad, so SolidTrace itself still runs on one machine with no cluster.
- **Fog 1 (scope):** resolved. SolidTrace records and shows; a factory module in this repo dispatches, through Nomad.
- **Fog 2 (audience):** resolved. A product for other teams, Tagalys first, self-hosted by each.
- **Fog 4 (VMs and isolation):** partly resolved. Nomad clients; the isolation of a run is still open.
- **Fog 6 (data):** partly resolved. Agents never run on the SolidTrace instance, and the record's load on it is small.
- **Proposed shape, stage 4:** "the OpenHands Agent Server or a thin Go worker following Symphony's SPEC" is superseded by Nomad.
- **Research summary, "Per-VM runtime: trial the OpenHands Agent Server":** superseded.
- **Not on the map at all:** the licence, the monorepo, Console as the one UI, Evidence, and hardening.

## 6. Where I differ from the earlier session's conclusions

**Agree:**

- Record, measure, improve, run as the order of dependency, and that the record works with no dispatcher.
- OTLP logs first, then transcripts, then traces.
- The production-outcome angle as SolidTrace's own.
- No project to adopt whole, and nothing writes to ClickHouse around EventStore.

**Differ:**

- **Transcript storage:** Logs before new tables, as above.
- **Verification and Evidence** are absent from its plan.
- **Scope:** it lists Warp's whole surface as "what SolidTrace would have to build", including a steering channel and a definition validator. I'd cut steering and the inference layer from the first product.
- **Hardening:** it treats this as a question of order against OAuth. I'd treat it as part of the product.
- **A withdrawn view of mine:** my first draft read the goal as developing SolidTrace with a factory, and said SolidTrace should never dispatch. The user corrected the goal, and that conclusion is gone.

## 7. Open questions

Candidate tickets for the map, most plan-changing first.

1. **Tagalys as design partner.** How many engineers, which agents, which forge, and do they run agents in the cloud yet or only on laptops? Stage 1 should be built to the answers.
2. **Transcript storage.** Logs in an agent Project, or new tables? A prototype with real transcripts.
3. **Transcript capture.** Session-end upload, tailing, or the Agent SDK's session store? It differs between laptops and Nomad tasks.
4. **The Run and the session.** What a Run is, how a laptop session and a dispatched run relate, and where run state lives when no factory is connected.
5. **Agents.** Claude Code only at first, or Codex too?
6. **Isolation of a run on Nomad.** The Docker driver, or something stronger for code an agent wrote? Secrets injection and network egress.
7. **The dispatcher.** Its language, its database, how it signs in to SolidTrace, and how Console reads from it.
8. **The factory's definition.** Nomad job files directly, or a format of our own that renders to them?
9. **Triggers.** GitHub first? Then Slack, Linear, Monitors and Issues.
10. **Scorers.** Where they run, whose model keys they use, how runs are sampled.
11. **Evidence.** What it holds and where it is stored.
12. **Redaction and Retention** of transcripts, which hold whatever the agent read.
13. **Order against OAuth for the MCP server,** and when SSO and an audit log land.
14. **Words.** Run, Session, Factory, Automation, Scorer, Evidence, Dispatcher: glossary terms to settle.
15. **The README.** Its tagline and "no orchestration" line once the factory module exists.
16. **Licence follow-ups** from section 3.
