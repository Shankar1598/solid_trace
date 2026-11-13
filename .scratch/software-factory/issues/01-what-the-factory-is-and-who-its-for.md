# 01: What is the factory, and who is it for?

Type: grilling
Status: resolved
Map: [SolidTrace Software Factory](../map.md)

## Question

Is the software factory a way of developing SolidTrace, or a product? Who uses it, and how do they get it? Is SolidTrace only the factory's record and analysis, or does it also dispatch agents, and where does that line fall? (The first map's Fog 1, Scope, and Fog 2, Audience.)

## Answer

Resolved 2026-11-13 (grilling, in the session written up in [research/fable-opinion.md](../research/fable-opinion.md), sections 1 and 2).

**A product for other teams.**

> "I am not talking about building solid trace further using factories. I am talking about building a product like Warp.dev so that other teams, eg Tagalys and many other larger teams can use this as a framework and infra layer for their own factories."

- Tagalys is the first team to use it. SolidTrace's own development can use it too, but that isn't the goal.
- **Open source and self-hosted only:** "we are not gonna offer a hosted service right. this is gonna be open source.." and "everyone will self host this". The licence is in [Which licence does SolidTrace take?](03-licence.md).

**One rule divides the work: SolidTrace stores and shows; the factory acts.** The user answered "makes sense" to this shape.

- **SolidTrace gains observability features,** each worth having with no factory: an OTLP receiver; agent sessions as a fourth signal beside Issues, Logs and traces, with a sessions page; scores as stored data with starter Dashboards; a webhook Integration and scoped tokens; the join from a release to the runs behind it.
- **The factory module holds anything that acts on another system or runs code:** triggers, the dispatcher, secrets, the GitHub App, the factory repository sync, and the Scorer and self-improvement jobs that call a model.

**Three pieces:**

| Piece | Runs on | Module | Holds |
|---|---|---|---|
| Agent allocations: one per run, with a harness such as Claude Code | Nomad clients | Factory | The run's secrets, for its lifetime |
| Dispatcher: triggers, GitHub App, factory repository sync. Nomad is the queue. | One small service, beside the Nomad server | Factory | Run state and secrets, in its own small database |
| Record and UI: sessions, transcripts, cost, scores, Dashboards, Monitors | The SolidTrace instance | Console and EventStore | Everything recorded; users, Organizations and sign-in |

**Console is the one UI and the one sign-in.**

- The factory has no dashboard, users or Organizations of its own. It signs users in through SolidTrace, which OAuth for the MCP server makes possible.
- Console gets the Warp-like pages, the viewing side first. Runs, transcripts, cost and scores are stored data, and they appear when a factory is connected.
- Starting, cancelling and approving runs go through GitHub, Slack and the dispatcher's API first.
- The user was told the trade-off: if Console later gets start and cancel buttons, a compromised Console could start runs, though it would still hold no repository credentials.
