# 05: What is the first product, and in what order is the rest built?

Type: grilling
Status: open
Blocked by: 04
Map: [SolidTrace Software Factory](../map.md)

## Question

Which stage ships first, what each stage holds, and what the product never builds. The candidate answer, not yet put to the user, is [research/fable-opinion.md](../research/fable-opinion.md), section 3:

- **Enter through the record, not the runtime.** A team points its engineers' Claude Code and Codex at SolidTrace and sees sessions, cost and tool use before it changes how it works.
- **Build order:** record (OTLP receiver, transcript capture, a Run entity, a sessions page, starter Dashboards, MCP tools, redaction); measure (GitHub App, Scorers and human grades, cost per PR, cycle time, the release join); run (the factory module on Nomad, triggers, secrets, Evidence); improve (self-improvement PRs, benchmarks).
- **Hardening is part of the product:** scoped, expiring machine tokens, SSO and an audit log before the run stage ships to a team.
- **Don't build what has an owner:** no agent or harness, no inference gateway, no task tracker, no live steering, no definition format where the harnesses have one.
- **The improvement layer is composition:** a Scorer is a sampled LLM judge whose verdicts are stored data; a Monitor firing is a trigger; a trigger starts a run.
- **The join to production:** an Issue or Monitor starts a triage run with a scoped MCP token; after the release, SolidTrace says whether the Issue stopped and which runs brought new ones.
- **Evidence is a product concept:** a run carries test results, screenshots, recordings and the spec it was checked against.
- **Nomad behind one small interface** (dispatch a run, watch its state, store a secret), with no second backend built now.

The first map's proposed shape ordered it record, measure, improve, run, with improvement started through an existing runner such as `claude-code-action`. The opinion moves run before improve. Settle the order, the cut list, and which items are concepts in the spec.

A tension from [How can a run be isolated on Nomad?](09-run-isolation-on-nomad.md) (2026-11-13): it keeps the model key out of the run by putting it in a gateway, while the cut list says "no inference gateway". Does the factory require the team's own gateway, ship a thin one, or accept the model key inside the run? The opinion's "secrets are Nomad Variables" also no longer holds for per-run secrets.
