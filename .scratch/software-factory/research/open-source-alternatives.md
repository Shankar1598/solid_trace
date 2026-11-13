# Open-source alternatives to a software factory: orchestration and runtime

Research note, 2026-11-13. Scope: open-source projects that run many coding agents in parallel, on many machines or sandboxes, and orchestrate them. Warp itself and transcript/telemetry formats are covered by other notes.

Method: stars, license, last push and latest release come from the GitHub REST API (`/repos/{owner}/{repo}` and `/releases/latest`), read on 2026-11-13. Other claims cite the README or docs inline. "Unconfirmed" marks anything I could not verify from a primary source.

## Summary

Findings:

- Several projects have changed a lot since mid-2025. OpenHands' main repo is now "Agent Canvas", a control centre over one or more Agent Servers ([README](https://github.com/OpenHands/OpenHands)). Coder removed Tasks and deprecated AgentAPI in favour of its own Go agent ([migration](https://coder.com/docs/ai-coder/agents/tasks-to-chats-migration.md), [AgentAPI](https://github.com/coder/agentapi)). Daytona moved to a private codebase in June 2026 ([README](https://github.com/daytonaio/daytona)). Vibe Kanban's company shut down in April 2026 ([announcement](https://www.vibekanban.com/blog/shutdown)). Terragon shut down and published a snapshot ([README](https://github.com/terragon-labs/terragon-oss)).
- Projects are converging on four layers: (1) an issue- or schedule-driven dispatcher, (2) a per-host agent server that drives CLI agents (often through ACP or an HTTP wrapper), (3) a sandbox runtime (worktree, container, microVM or Kubernetes pod), and (4) telemetry, which usually means OTLP from the agent CLIs themselves.
- No open-source project here combines multi-VM orchestration of Claude Code and Codex with a warehouse of transcripts. The nearest are OpenHands (Agent Server + automation, MIT) and Multica (daemons on your machines + execution log, but source-available with a no-embedding clause).
- Telemetry already exists at the agent layer. Claude Code ([docs](https://code.claude.com/docs/en/monitoring-usage)), Codex ([docs](https://learn.chatgpt.com/docs/config-file/config-advanced)), Goose ([docs](https://goose-docs.ai/docs/tutorials/laminar)), the OpenHands SDK ([docs](https://docs.openhands.dev/sdk/guides/observability)) and Gas Town ([design](https://github.com/gastownhall/gastown/blob/main/docs/design/otel/otel-architecture.md)) all export OTLP. I found no OTLP receiver in SolidTrace's `event_store` source (grep for `otlp` matched only indirect deps in `go.mod`). Ingesting OTLP is therefore the common prerequisite, whichever orchestrator is chosen.

| Project | License | Runtime | Agents driven | Orchestration | Telemetry export | Activity (stars, latest release) |
|---|---|---|---|---|---|---|
| [OpenHands](https://github.com/OpenHands/OpenHands) (Agent Canvas + [SDK/Agent Server](https://github.com/OpenHands/software-agent-sdk) + [automation](https://github.com/OpenHands/automation)) | MIT. Cloud Helm charts are PolyForm Free Trial | Local, Docker per conversation, any VM running an Agent Server. Kubernetes via the commercial Cloud | Own agent. Claude Code, Codex, Gemini, OpenCode, Pi via ACP | Automation service: schedules, webhooks, Slack/GitHub/Linear | OTLP traces. REST/WebSocket API | 93.4k, v1.40.0 2026-11-12 |
| [Coder](https://github.com/coder/coder) | AGPL-3.0 core. AI Gateway is Premium | Workspaces from Terraform templates | Coder Agents (own Go agent). CLI agents only as workspace modules | Chats API, parallel child agents. Tasks removed | Chats in the Coder DB. AI Gateway (Premium) audit | 17.4k, v2.38.7 2026-11-10 |
| [OpenAI Symphony](https://github.com/openai/symphony) | Apache-2.0 | Per-issue workspace dirs, local or over SSH | Codex app-server | Polls Linear/GitHub/Jira/Asana/GitLab. Concurrency caps, retries | Structured logs, JSON API, dashboard | 28.0k, v0.0.3 2026-09-15 |
| [Multica](https://github.com/multica-ai/multica) | Source-available (Apache-2.0 + conditions) | Daemon on your machines | Claude Code, Codex, Cursor, Copilot, OpenCode, others | Issue board, cron autopilots, review gates, retries | Execution log + usage in Postgres | 56.0k, v0.9.0 2026-11-13 |
| [Terragon OSS](https://github.com/terragon-labs/terragon-oss) | Apache-2.0 | Cloud sandboxes (E2B, Daytona, Docker providers) | Claude Code, Codex, Amp, Gemini | GitHub, Slack, automations, MCP | Postgres. No export found | 261, snapshot 2026-01-16, unmaintained |
| [Open SWE / Robin](https://github.com/langchain-ai/open-swe) | MIT | LangSmith sandboxes by default | Own (Deep Agents) | GitHub, Slack, Linear, schedules | LangSmith tracing | 11.2k, desktop-v0.3.16 2026-11-09 |
| [Paperclip](https://github.com/paperclipai/paperclip) | MIT | Node server + agent adapters | OpenClaw, Codex, Claude, Cursor, HTTP | Goals, org chart, budgets | Cost tracking (export unconfirmed) | 113.6k, v2026.1106.0 2026-11-06 |
| [Agent Orchestrator](https://github.com/OrchestratorInc/agent-orchestrator) | Apache-2.0 | Local worktrees + daemon. Hosted cloud option | Many CLIs | Planner agent, worker per worktree, CI/review loop | Unconfirmed | 14.6k, v0.15.0 2026-11-13 |
| [Gas Town](https://github.com/gastownhall/gastown) | MIT | Local tmux + git worktrees | Claude Code, Copilot, Codex, Gemini | Mayor agent, Beads ledger, merge queue | OTLP metrics + logs | 18.8k, v1.2.1 2026-06-06 |
| [Vibe Kanban](https://github.com/BloopAI/vibe-kanban) | Apache-2.0 | Local worktrees | 10+ CLIs | Kanban, diff review, PR | Local only | 28.7k, 2026-04-24. Company shut down |
| [Claude Squad](https://github.com/smtg-ai/claude-squad) | AGPL-3.0 | Local tmux + worktrees | Claude Code, Codex, Gemini, OpenCode, Amp | TUI | None | 8.8k, v1.0.20 2026-08-20 |
| [Imbue Sculptor](https://github.com/imbue-ai/sculptor) | MIT | Local worktrees. Docker/remote experimental | Claude Code, Pi, any terminal agent | Desktop app | Unconfirmed | 246, v0.49.0 2026-10-21 |
| [Claude Code](https://github.com/anthropics/claude-code) + [Agent SDK](https://github.com/anthropics/claude-agent-sdk-python) + [Action](https://github.com/anthropics/claude-code-action) | CLI proprietary. Action MIT | Local, headless, GitHub runners | Claude Code | SDK, `@claude` triggers in Actions | OTLP metrics/events, traces (beta). JSONL. SessionStore | 155.5k, v2.1.323 2026-11-12 |
| [Codex CLI](https://github.com/openai/codex) | Apache-2.0. Codex cloud hosted only | Local, `codex exec`, app-server | Codex | `exec --json`, GitHub Action | OTLP log events. Session files | 135.2k, rust-v0.169.0 2026-11-13 |
| [opencode](https://github.com/anomalyco/opencode) | MIT | Local, `opencode serve` HTTP | Own agent | HTTP API + SSE | Experimental OTel flag (unconfirmed) | 226.6k, v1.19.5 2026-10-28 |
| [Goose](https://github.com/aaif-goose/goose) | Apache-2.0 (AAIF) | Local desktop/CLI/API | Own agent | Recipes, API | OTLP | 56.5k, v1.59.0 2026-11-10 |
| [mini-SWE-agent](https://github.com/SWE-agent/mini-swe-agent) / [SWE-ReX](https://github.com/SWE-agent/SWE-ReX) | MIT | Local, Docker/Podman, Singularity, Modal, Fargate | Own | Batch benchmark runs | Trajectory files | 9.6k, v2.4.6 2026-07-23 |
| [E2B runtime](https://github.com/e2b-dev/runtime) + [SDK](https://github.com/e2b-dev/E2B) | Apache-2.0 | Firecracker microVMs. Cloud, single-host Embed, Terraform, Kubernetes | Any (sandbox only) | None (sandbox API) | OTel. Lifecycle events in ClickHouse | 1.8k/14.8k, 2026.30 2026-09-10 |
| [kubernetes-sigs/agent-sandbox](https://github.com/kubernetes-sigs/agent-sandbox) | Apache-2.0 | Kubernetes CRD, gVisor/Kata | Any (sandbox only) | Warm pools, claims | Kubernetes-native | 4.6k, v1.0.11 2026-11-12 |
| [OpenSandbox](https://github.com/opensandbox-group/OpenSandbox) | Apache-2.0 (AAIF) | Docker, Kubernetes, Firecracker | Any (sandbox only) | Batch create, pools | Unconfirmed | 17.3k, release-1.2.1 2026-11-13 |
| [Rivet Sandbox Agent](https://github.com/rivet-dev/agents) | Apache-2.0 | Runs inside any sandbox or locally | Claude Code, Codex, OpenCode, Cursor, Amp, Pi | HTTP API + TS SDK | Session event log via pluggable driver | 1.6k, v0.5.6 2026-11-05 |
| [Dagger container-use](https://github.com/dagger/container-use) | Apache-2.0 | Containers + git branch per agent (MCP) | Any MCP agent | None | Command history per env | 4.1k, v0.4.2 2025-08-19 (stalled) |
| [Daytona](https://github.com/daytonaio/daytona) | AGPL-3.0 (last public tag) | OCI sandboxes | Any (sandbox only) | None | n/a | Archived. Moved private June 2026 |
| [Modal Sandboxes](https://modal.com/docs/guide/sandboxes) | Hosted. [Client](https://github.com/modal-labs/modal-client) Apache-2.0 | gVisor containers or VM runtime | Any | None | n/a | Hosted service only |

## Factory control planes

### OpenHands (Agent Canvas, Agent Server, automation)

- What: [Agent Canvas](https://github.com/OpenHands/OpenHands) is "the self-hosted developer control center for coding agents and automations". The repo is split: [software-agent-sdk](https://github.com/OpenHands/software-agent-sdk) owns the SDK and the Agent Server REST API, and [automation](https://github.com/OpenHands/automation) owns "scheduling, webhooks, run history, and dispatching" ([README, repository boundaries](https://github.com/OpenHands/OpenHands)). All three are MIT. The [self-hosted Cloud Helm charts](https://github.com/OpenHands/OpenHands-Cloud) are under PolyForm Free Trial ("NOT an open source license", 30 days/year). The extracted [sandbox-server](https://github.com/OpenHands/sandbox-server) is MIT outside `enterprise/`.
- Activity: 93.4k stars, v1.40.0 on 2026-11-12. SDK v1.62.0 on 2026-11-11. Very active, but the architecture was reorganised recently (the old app server moved to `sandbox-server` and `legacy`).
- Runtime: an Agent Server is "a REST API for running multiple agents on a single machine". Canvas can connect to several Agent Servers on a laptop, a VM or OpenHands Cloud ([README, Architecture](https://github.com/OpenHands/OpenHands)). `OH_CONVERSATION_RUNTIME=docker` runs "each new conversation in its own Docker container, with its own Agent Server" ([README, Option 3](https://github.com/OpenHands/OpenHands)). SDK workspaces are Local, Docker, and RemoteAPI, which connects to an existing server ([workspace docs](https://docs.openhands.dev/sdk/arch/workspace)). Kubernetes-scale sandboxing is in the commercial Cloud/Enterprise charts.
- Agents: its own agent, plus Claude Code (`@agentclientprotocol/claude-agent-acp`), Codex (`@zed-industries/codex-acp`), Gemini CLI, OpenCode and Pi over ACP. The Agent Server "owns the subprocess and the credentials" ([ACP docs](https://docs.openhands.dev/openhands/usage/agent-canvas/acp-agents)).
- Orchestration: automations run "on a schedule or in response to webhook events", with Slack, GitHub, Linear and others ([README](https://github.com/OpenHands/OpenHands)). Multi-host dispatch is per backend: each Agent Server is one host:port ([README, Architecture](https://github.com/OpenHands/OpenHands)).
- Telemetry: OpenTelemetry via Laminar instrumentation. It is configured with standard `OTEL_EXPORTER_OTLP_*` variables, sends to "any OTLP-compatible backend", and traces LLM calls, tool calls, `agent.step` and conversation lifecycle with `conversation_id` and `session_id` attributes ([observability docs](https://docs.openhands.dev/sdk/guides/observability)). The Agent Server persists data under `workspace/conversations` and `workspace/bash_events`. File format unconfirmed ([agent-server docs](https://docs.openhands.dev/sdk/arch/agent-server)). Whether ACP agents' internal steps appear in OpenHands traces: unconfirmed ([ACP docs](https://docs.openhands.dev/openhands/usage/agent-canvas/acp-agents) do not say).
- Self-host and external store: single-VM self-hosting is documented ([SELF_HOSTING.md](https://github.com/OpenHands/OpenHands/blob/main/docs/SELF_HOSTING.md)). Traces go to SolidTrace if SolidTrace accepts OTLP. Conversation events are reachable over the REST/WebSocket API.

### Coder (Coder Agents, removed Tasks, deprecated AgentAPI, Xum)

- What: [coder/coder](https://github.com/coder/coder) is "secure environments for developers and their agents". AGPL-3.0, 17.4k stars, v2.38.7 on 2026-11-10.
- Change: "Coder Tasks was deprecated in Coder v2.34 and has been removed, including the Tasks API", and `coder task` is gone ([migration guide](https://coder.com/docs/ai-coder/agents/tasks-to-chats-migration.md)). [AgentAPI](https://github.com/coder/agentapi) (MIT; an HTTP wrapper over Claude Code, Codex, Goose, Aider, Gemini, Amp and others) is archived and "deprecated and no longer maintained".
- Runtime: workspaces provisioned by templates. For Coder Agents, "the workspace itself has no knowledge of AI" and tools run in it over the workspace connection ([Coder Agents](https://coder.com/docs/ai-coder/agents)).
- Agents: Coder Agents is a Go agent and "not a wrapper around third-party agent tools like Claude Code or Codex" ([docs](https://coder.com/docs/ai-coder/agents)). Third-party CLIs can still be installed into workspaces through [Coder Registry](https://registry.coder.com) modules ([AgentAPI README](https://github.com/coder/agentapi)), but they are outside Coder's orchestration API.
- Orchestration: Chats API (`POST /api/v2/chats`, messages, interrupt) ([migration guide](https://coder.com/docs/ai-coder/agents/tasks-to-chats-migration.md)). "The root agent can spawn child agents to work on independent tasks in parallel" ([docs](https://coder.com/docs/ai-coder/agents)).
- Telemetry: "All chat state is stored in the Coder database" ([docs](https://coder.com/docs/ai-coder/agents)). AI Gateway (formerly AI Bridge) records prompts, token usage and tool invocations, and is Premium ([AI Gateway](https://coder.com/docs/ai-coder/ai-gateway)). Export path: unconfirmed.
- [Xum](https://github.com/coder/xum) (renamed from Mux): AGPL-3.0 desktop "coding agent multiplexer" with local, worktree and SSH runtimes and its own agent loop. 2.1k stars, v0.32.2 on 2026-11-10.

### OpenAI Symphony

- What: "Symphony turns project work into isolated, autonomous implementation runs" ([README](https://github.com/openai/symphony)). It ships as a language-neutral [SPEC.md](https://github.com/openai/symphony/blob/main/SPEC.md) (2.3k lines) plus an experimental Elixir reference implementation. Apache-2.0, 28.0k stars, v0.0.3 on 2026-09-15. It is described as "a low-key engineering preview for testing in trusted environments".
- Runtime: per-issue workspace directories on the local filesystem ([SPEC §3.3, §9](https://github.com/openai/symphony/blob/main/SPEC.md)). An optional SSH worker extension keeps one orchestrator and runs workers on a pool of SSH hosts, with `worker.max_concurrent_agents_per_host`. The Codex app-server is launched "over SSH stdio" ([SPEC Appendix A](https://github.com/openai/symphony/blob/main/SPEC.md)).
- Agents: "Coding-agent executable that supports the targeted Codex app-server mode" ([SPEC §3.3](https://github.com/openai/symphony/blob/main/SPEC.md)). Codex only, as specified.
- Orchestration: polls the tracker. Adapters exist for Linear, GitHub Issues, Jira Cloud, Asana and GitLab ([Elixir README](https://github.com/openai/symphony/blob/main/elixir/README.md)). It has an issue state machine, `max_concurrent_agents` (default 10) and per-state caps, retries with backoff, reconciliation, and workspace hooks ([SPEC §7-9](https://github.com/openai/symphony/blob/main/SPEC.md)). Tracker secrets are stripped from the agent's environment.
- Telemetry: "structured logs" at minimum. The spec "does not prescribe where logs are written" ([SPEC §2.1, §13](https://github.com/openai/symphony/blob/main/SPEC.md)). The Elixir version has a JSON API and dashboard ([Elixir README](https://github.com/openai/symphony/blob/main/elixir/README.md)).

### Multica

- What: "a source-available workspace where you assign work to AI coding agents the way you'd assign it to a teammate" ([README](https://github.com/multica-ai/multica)). 56.0k stars, v0.9.0 on 2026-11-13.
- License: Apache-2.0 "subject to the additional conditions". You may not use it "to provide a hosted service to third parties, or embed Multica as a component of a product or service that is sold". Internal use is allowed ([LICENSE](https://github.com/multica-ai/multica/blob/main/LICENSE)). This rules out embedding it in SolidTrace.
- Runtime: "a daemon on your laptop or cloud box". Architecture: Next.js web, a Go backend (Chi + WebSocket), PostgreSQL 17, and the agent daemon connected over WebSocket ([README, Architecture](https://github.com/multica-ai/multica)).
- Agents: Claude Code, Codex, Cursor, Copilot, Kimi, OpenCode and more. Steering a running agent works for Claude Code, Codex, Grok and Kiro ([README](https://github.com/multica-ai/multica)).
- Orchestration: assign an issue to an agent, cron "Autopilots", squads, review gates, retries and timeouts, CLI and API ([README](https://github.com/multica-ai/multica)).
- Telemetry: "Execution log → Replay every tool call, command, and error, timestamped", and per-run cost analytics ([README](https://github.com/multica-ai/multica)). Stored in Postgres. No OpenTelemetry in the code (GitHub code search for "opentelemetry" returned 0 hits). Self-host via Docker Compose or Helm.

### Terragon OSS

- "An open-source snapshot of Terragon at the time of shutdown … as-is, with no guarantees of maintenance" (January 16, 2026) ([README](https://github.com/terragon-labs/terragon-oss)). Apache-2.0, 261 stars.
- It ran Claude Code, Codex, Amp and Gemini in isolated cloud sandboxes, one branch per task with automatic PRs, and was triggered from GitHub, Slack, automations and an MCP server ([README](https://github.com/terragon-labs/terragon-oss)). Sandbox providers in code: [e2b, daytona, docker](https://github.com/terragon-labs/terragon-oss/tree/main/packages/sandbox/src/providers). It needs Postgres, Redis, R2, a GitHub App and Stripe to run. Useful only as reference code.

### Open SWE, now "Robin" (LangChain)

- "An open-source software factory built on Deep Agents". MIT, 11.2k stars, desktop-v0.3.16 on 2026-11-09 ([README](https://github.com/langchain-ai/open-swe)). Not accepting issues or contributions. "Deploying it as is requires Managed Deep Agents", which is LangSmith-hosted.
- It uses its own agent, not Claude Code or Codex. Triggers are the dashboard, GitHub issues/PRs, Slack, Linear and schedules. "LangSmith is the default sandbox and tracing provider" ([README](https://github.com/langchain-ai/open-swe)). Learn-only for SolidTrace.

### Paperclip

- "Open-source orchestration for teams of AI agents": a Node.js server and React UI with org charts, budgets, goals and cost tracking. Any agent that "can receive a heartbeat" plugs in through adapters ([README](https://github.com/paperclipai/paperclip)). MIT, 113.6k stars, v2026.1106.0 on 2026-11-06. It works at business level, not as a coding-runtime layer. Telemetry export: unconfirmed.

## Local parallel-agent managers

These run agents in git worktrees on one machine, some with SSH remotes. They are useful UX references, but none is a multi-VM runtime.

### Vibe Kanban (BloopAI)

- Apache-2.0, 28.7k stars. Latest release 2026-04-24, commits continue (last push 2026-11-12). The README says "Vibe Kanban is sunsetting" ([README](https://github.com/BloopAI/vibe-kanban)). bloop shut down on 2026-04-10. The project continues "community maintained", and remote features (issues, comments, projects, organisations) were removed in favour of "a fully local architecture" ([announcement](https://www.vibekanban.com/blog/shutdown)).
- Workspaces give each agent "a branch, a terminal, and a dev server". It drives Claude Code, Codex, Gemini CLI, Copilot, Amp, Cursor, OpenCode, Droid, CCR and Qwen Code, with inline diff review and PR creation ([README](https://github.com/BloopAI/vibe-kanban)). Storage is local (Rust + sqlx). Exact DB and export: unconfirmed. OpenTelemetry appears only in the `crates/remote` cloud-service crate (GitHub code search), which is the part being retired.

### Claude Squad

- AGPL-3.0, 8.8k stars, v1.0.20 on 2026-08-20. A terminal app managing Claude Code, Codex, Gemini, OpenCode and Amp. "Each task gets its own isolated git workspace". Needs tmux and gh ([README](https://github.com/smtg-ai/claude-squad)). Local only. No telemetry.

### Imbue Sculptor

- Now open source: MIT, 246 stars, v0.49.0 on 2026-10-21. A "desktop app for running coding agents in parallel" and an "experimental research preview" ([README](https://github.com/imbue-ai/sculptor)). Workspaces are isolated worktrees. It integrates Claude Code and Pi and can run "any terminal-based agents". A Docker/remote "Container Backend" is experimental ([README](https://github.com/imbue-ai/sculptor)). Telemetry: unconfirmed.

### Agent Orchestrator (Orchestrator.inc, formerly ComposioHQ/agent-orchestrator)

- Apache-2.0, 14.6k stars, v0.15.0 on 2026-11-13. A desktop app plus daemon. A main agent "plans the work, starts a worker for each piece in its own worktree". "Failed checks, review comments, and merge conflicts go back to the worker that owns the branch". Optional reviewer agent. Hosted "Cloud Agents" ([README](https://github.com/OrchestratorInc/agent-orchestrator)). Its CI-failure and review feedback loop is the useful idea. Telemetry: unconfirmed.

### Gas Town

- MIT, 18.8k stars, v1.2.1 on 2026-06-06 (pushed 2026-11-12). "Multi-agent orchestration system for Claude Code, GitHub Copilot, and other AI agents with persistent work tracking". A "Mayor" Claude Code instance coordinates; "Polecats" are worker agents. Git-worktree "hooks" and a Beads ledger persist work. It claims to "scale comfortably to 20-30 agents" ([README](https://github.com/gastownhall/gastown)).
- Telemetry: OTLP HTTP metrics and logs, "backend-agnostic", enabled with `GT_OTEL_METRICS_URL` / `GT_OTEL_LOGS_URL`. Session start/stop events are emitted. `GT_LOG_AGENT_OUTPUT=true` streams "Claude conversation turns to logs" ([OTel design](https://github.com/gastownhall/gastown/blob/main/docs/design/otel/otel-architecture.md)). This is the clearest example of an orchestrator emitting transcripts as OTLP logs.

### Others in this class (brief)

- [Emdash](https://github.com/generalaction/emdash): Apache-2.0, 6.2k stars, v1.2.11 on 2026-11-06. Desktop app, worktree per task, "local projects and remote machines over SSH". Auto-detects provider CLIs.
- [Paradev](https://github.com/ParadevAI/Paradev) (redirect target of `superset-sh/superset`): MIT, 16.3k stars. Worktree per task. Remote hosts and automations are on a paid Pro plan.
- [Crystal](https://github.com/stravu/crystal): MIT, "now Nimbalyst". Last push 2026-02-26.
- [claude_code_agent_farm](https://github.com/Dicklesworthstone/claude_code_agent_farm): "20+ Claude Code agents in parallel" via tmux with lock-based coordination. MIT with an "OpenAI/Anthropic Rider" (not plain MIT).
- [ruflo](https://github.com/ruvnet/ruflo) (formerly claude-flow): MIT, 81.6k stars. A swarm harness on top of Claude Code. Not evaluated in depth.

## Agent CLIs with headless or server modes

These are the units an orchestrator runs on each VM. Their native telemetry is the main data source.

### Claude Code, Claude Agent SDK, Claude Code GitHub Action

- License: the Claude Code repo and the TypeScript Agent SDK repo say "© Anthropic PBC. All rights reserved. Use is subject to Anthropic's Commercial Terms" ([claude-code LICENSE.md](https://github.com/anthropics/claude-code/blob/main/LICENSE.md), [TS SDK](https://github.com/anthropics/claude-agent-sdk-typescript)). The [Python SDK](https://github.com/anthropics/claude-agent-sdk-python) repo is tagged MIT but bundles the Claude Code CLI ([README](https://github.com/anthropics/claude-agent-sdk-python)). [claude-code-action](https://github.com/anthropics/claude-code-action) is MIT and "executes entirely on your own GitHub runner".
- Activity: claude-code v2.1.323 on 2026-11-12. Python SDK v0.2.181 on 2026-11-12.
- Telemetry: OTel metrics (`claude_code.session.count`, `token.usage`, `cost.usage`, etc.) and log events (`claude_code.user_prompt`, `assistant_response`, `tool_result`, `tool_decision`, `api_request`, …). Distributed tracing is beta (`CLAUDE_CODE_ENHANCED_TELEMETRY_BETA=1`). Prompt and tool content is redacted unless `OTEL_LOG_USER_PROMPTS` / `OTEL_LOG_TOOL_DETAILS` are set. `session.id` and `prompt.id` are present, plus custom `OTEL_RESOURCE_ATTRIBUTES` ([monitoring docs](https://code.claude.com/docs/en/monitoring-usage)). The Agent SDK's `SessionStore` adapter mirrors the JSONL transcripts (default `~/.claude/projects/`) to an external backend. Reference adapters exist for S3, Redis and Postgres ([session storage docs](https://code.claude.com/docs/en/agent-sdk/session-storage)). A SolidTrace adapter would fit there.

### Codex CLI

- Apache-2.0, 135.2k stars, rust-v0.169.0 on 2026-11-13 ([repo](https://github.com/openai/codex)). Codex cloud is a hosted product. Its base image [codex-universal](https://github.com/openai/codex-universal) is public (no license file; last push 2026-05-02).
- Headless: `codex exec --json` streams JSONL (`thread.started`, `turn.*`, `item.*`, `error`). `codex exec resume <SESSION_ID>`. `--ephemeral` skips rollout files. For CI it points to `openai/codex-action` ([non-interactive docs](https://learn.chatgpt.com/docs/non-interactive-mode.md)). The app-server mode is what Symphony drives ([Symphony Elixir README](https://github.com/openai/symphony/blob/main/elixir/README.md)).
- Telemetry: the `[otel]` table with `otlp-http` or `otlp-grpc` exporters. Events are `codex.api_request`, `codex.tool_decision`, `codex.user_prompt` (redacted unless `log_user_prompt`) and `codex.tool_result`. Transcripts are saved under `CODEX_HOME` ([advanced config](https://learn.chatgpt.com/docs/config-file/config-advanced)).

### opencode (now anomalyco/opencode)

- MIT, 226.6k stars, v1.19.5 on 2026-10-28. `sst/opencode` redirects to [anomalyco/opencode](https://github.com/anomalyco/opencode).
- `opencode serve` is a headless HTTP server with an OpenAPI 3.1 spec at `/doc`. Endpoints include `POST /session`, `/session/:id/prompt_async`, `/session/:id/abort` and `GET /session/status`, and an SSE stream at `GET /event`. Basic auth via `OPENCODE_SERVER_PASSWORD` ([server docs](https://opencode.ai/docs/server/)). Whether many sessions can run in one server is not stated (unconfirmed).
- OTel: `experimental.openTelemetry` config flag ([PR #4978](https://github.com/anomalyco/opencode/pull/4978)). Laminar notes the flag differs in V2 and builds spans from the event stream instead ([Laminar](https://laminar.sh/docs/tracing/integrations/opencode)). Unconfirmed in current official docs.

### Goose (now aaif-goose/goose)

- Apache-2.0, 56.5k stars, v1.59.0 on 2026-11-10. Part of the Agentic AI Foundation at the Linux Foundation. Desktop app, CLI and API. Can use Claude/ChatGPT/Gemini subscriptions via ACP ([README](https://github.com/aaif-goose/goose)).
- OTel: standard `OTEL_EXPORTER_OTLP_ENDPOINT`. The official tutorials send traces to [Laminar](https://goose-docs.ai/docs/tutorials/laminar) and [MLflow](https://goose-docs.ai/docs/tutorials/mlflow). Metrics and logs coverage: unconfirmed.

### SWE-agent, mini-SWE-agent, SWE-ReX

- [SWE-agent](https://github.com/SWE-agent/SWE-agent): MIT, last release v1.1.0 on 2025-05-22. The team now points to [mini-SWE-agent](https://github.com/SWE-agent/mini-swe-agent) (MIT, v2.4.6 on 2026-07-23). mini-SWE-agent is a ~100-line agent that runs in "local environments, docker/podman, singularity/apptainer, bubblewrap", and its trajectory equals the message list ([README](https://github.com/SWE-agent/mini-swe-agent)).
- [SWE-ReX](https://github.com/SWE-agent/SWE-ReX) (MIT, v1.4.0 on 2025-08-14) runs shell sessions "locally or remotely in Docker containers, AWS remote machines, Modal", with "100 agents in parallel". Built for benchmarks. Not a team factory and does not drive Claude Code or Codex.

## Sandbox runtimes

### E2B (runtime + SDK)

- [e2b-dev/runtime](https://github.com/e2b-dev/runtime) (formerly `e2b-dev/infra`): Apache-2.0, written in Go, 1.8k stars, release 2026.30 on 2026-09-10. It is "the complete backend that powers E2B Cloud": API, per-node Firecracker orchestrator, in-VM `envd`, edge proxy, template builder. SDK [e2b-dev/E2B](https://github.com/e2b-dev/E2B): Apache-2.0, 14.8k stars.
- Runtime: one Firecracker microVM per sandbox, resumed from a snapshot. Pause/resume, forking, and egress allow/deny lists ([README](https://github.com/e2b-dev/runtime)). It needs Linux with KVM.
- Self-host: "E2B Embed" is "the whole stack … on a single Linux host with KVM" via docker compose, also as Terraform for GCP/AWS/Azure and a Kubernetes manifest. "Embed is one machine by design". Multi-node production is offered as an E2B enterprise deployment ([README](https://github.com/e2b-dev/runtime)).
- Telemetry: "Everything exports OpenTelemetry. Sandbox lifecycle events, host stats, and metrics land in ClickHouse" ([README](https://github.com/e2b-dev/runtime)). It is the same data store as SolidTrace. E2B does not drive agents; you run Claude Code or Codex inside the VM.

### kubernetes-sigs/agent-sandbox

- Apache-2.0, SIG Apps, 4.6k stars, v1.0.11 on 2026-11-12 ([repo](https://github.com/kubernetes-sigs/agent-sandbox)). It provides a `Sandbox` CRD for "isolated, stateful, singleton workloads", with `SandboxTemplate`, `SandboxClaim` and `SandboxWarmPool`. Isolation is delegated to gVisor or Kata via `RuntimeClass`. Go SDK. Requires Kubernetes 1.25+, 1.29+ recommended ([README](https://github.com/kubernetes-sigs/agent-sandbox)). It is a sandbox orchestrator only, with no agent or task layer.

### OpenSandbox

- Apache-2.0, an AAIF project (formerly under Alibaba), 17.3k stars, release-1.2.1 on 2026-11-13. "Start locally with Docker and deploy on Kubernetes through a unified sandbox API". It has a Firecracker "Fast Sandbox" runtime with pause/resume, per-sandbox egress policies, a credential vault, SDKs in five languages, and a CLI and MCP server ([README](https://github.com/opensandbox-group/OpenSandbox)). Telemetry export: unconfirmed.

### Rivet Sandbox Agent

- Apache-2.0, in [rivet-dev/agents](https://github.com/rivet-dev/agents) (v0.5.6 on 2026-11-05). "Run coding agents in sandboxes. Control them over HTTP." It supports Claude Code, Codex, OpenCode, Cursor, Amp and Pi. Deploy targets include Local, E2B, Daytona, Docker, Modal, Vercel and Cloudflare. The API covers sessions (ACP), filesystem, processes and MCP config ([docs](https://rivet.dev/sandbox-agent/docs)).
- Persistence: a `SessionPersistDriver` stores a session record and an event log (`id, eventIndex, sessionId, sender, payload, createdAt`). Drivers exist for in-memory, SQLite, Postgres, Rivet and custom ([session persistence](https://rivet.dev/sandbox-agent/docs/session-persistence)). A custom driver could write straight to SolidTrace. It fills the niche Coder's AgentAPI left, so the same churn risk applies.

### microsandbox

- Apache-2.0, 9.1k stars, v0.7.15 on 2026-11-12 ([repo](https://github.com/superradcompany/microsandbox)). "Fast, local microVMs for untrusted workloads". Linux needs KVM. Single host. No orchestration.

### Dagger container-use

- Apache-2.0, 4.1k stars. Last release v0.4.2 on 2025-08-19, last push 2026-09-21, so activity has slowed. An MCP server: "each agent gets a fresh container in its own git branch", with command history and logs, and you can drop into an agent's terminal ([README](https://github.com/dagger/container-use)). Local Dagger engine. No queue, no export.

### Daytona (dropped)

- "As of June 2026, Daytona's core development has moved to a private codebase. This repository will receive no further updates" ([README](https://github.com/daytonaio/daytona)). The repo is archived. The last public tag v0.190.0 (2026-06-23) is [AGPL-3.0](https://github.com/daytonaio/daytona/blob/v0.190.0/LICENSE). Not a candidate.

### Modal Sandboxes (hosted only)

- "Secure containers for executing untrusted code on Modal", gVisor by default or a VM runtime ([docs](https://modal.com/docs/guide/sandboxes)). Only the [client SDK](https://github.com/modal-labs/modal-client) is open (Apache-2.0). Not self-hostable.

## Fit for SolidTrace

The ask is coding agents on many VMs, transcripts and telemetry in SolidTrace's ClickHouse, and SolidTrace analysing and orchestrating them. Ranked:

1. **Claude Code and Codex native telemetry: integrate as the data source (do this regardless of orchestrator).** Both export OTLP events and metrics, with session and prompt ids ([Claude Code](https://code.claude.com/docs/en/monitoring-usage), [Codex](https://learn.chatgpt.com/docs/config-file/config-advanced)). Both write local transcripts. The Agent SDK has a `SessionStore` hook for shipping transcripts elsewhere ([docs](https://code.claude.com/docs/en/agent-sdk/session-storage)). Every orchestrator below just launches these CLIs, so this layer does not depend on the orchestrator choice. Prerequisite: SolidTrace needs an OTLP logs/metrics receiver, or an OTel Collector exporting into Log ingest. None exists in `event_store` today.

2. **OpenHands Agent Server + automation (MIT): the best candidate to adopt as the per-VM runtime, behind a trial.** It is already "an Agent Server anywhere" with REST/WebSocket and Docker-per-conversation isolation ([README](https://github.com/OpenHands/OpenHands)). It drives Claude Code and Codex over ACP ([docs](https://docs.openhands.dev/openhands/usage/agent-canvas/acp-agents)), has schedule and webhook automations ([automation](https://github.com/OpenHands/automation)), and emits OTLP traces ([docs](https://docs.openhands.dev/sdk/guides/observability)). It is very active and permissively licensed. Risks: its architecture moved repos within the last few months. Multi-node or Kubernetes deployment is in the non-open-source Cloud charts ([OpenHands-Cloud](https://github.com/OpenHands/OpenHands-Cloud)). It is unconfirmed whether ACP agents' steps appear in its traces. SolidTrace would orchestrate many Agent Servers itself, one per VM.

3. **OpenAI Symphony: learn from (or reimplement its spec) for the orchestration layer.** The SPEC is a compact, language-neutral design for exactly the dispatcher SolidTrace would need ([SPEC](https://github.com/openai/symphony/blob/main/SPEC.md)). It covers the tracker → per-issue workspace → agent-run state machine, concurrency caps globally, per state and per SSH host, retries and reconciliation, secret stripping, and an SSH worker pool across VMs. Apache-2.0. The reference code is Elixir and Codex-only, and it is a "low-key engineering preview", so adopting the code makes less sense than following the spec in SolidTrace's Go/Rails stack with a pluggable agent runner.

4. **E2B runtime (Apache-2.0): adopt later as the sandbox layer, only if per-task microVM isolation becomes a requirement.** Firecracker snapshots, pause/resume, fork, egress control, and its own events already in ClickHouse ([README](https://github.com/e2b-dev/runtime)). The open self-host path (Embed) is single-host and needs KVM, and multi-node is an enterprise deployment. Plain VMs or containers with SSH (Symphony's model) are enough to start. [kubernetes-sigs/agent-sandbox](https://github.com/kubernetes-sigs/agent-sandbox) is the alternative if SolidTrace ever runs on Kubernetes.

5. **Multica and Gas Town: learn from only.** Multica is the closest product to the stated vision: issues assigned to agents, daemons on your own machines, a per-run execution log, per-run cost, review gates ([README](https://github.com/multica-ai/multica)). Its license forbids embedding it in a commercial product or hosting it for third parties ([LICENSE](https://github.com/multica-ai/multica/blob/main/LICENSE)), so treat it as a UX and data-model reference. Gas Town shows an orchestrator emitting OTLP metrics plus opt-in conversation turns as logs ([OTel design](https://github.com/gastownhall/gastown/blob/main/docs/design/otel/otel-architecture.md)). Its attribute design is a good template for what SolidTrace should index.

A runner-up for item 2, if OpenHands proves too heavy: [Rivet Sandbox Agent](https://rivet.dev/sandbox-agent/docs) (Apache-2.0). It is a narrower per-sandbox HTTP adapter over Claude Code, Codex and OpenCode, with a pluggable `SessionPersistDriver` that could write events to SolidTrace ([docs](https://rivet.dev/sandbox-agent/docs/session-persistence)). It is younger (v0.5), and Coder's equivalent was deprecated ([AgentAPI](https://github.com/coder/agentapi)).

Not recommended: Coder (orchestration now drives only Coder's own agent, and the AI audit layer is Premium), Daytona (closed), Modal (hosted), Vibe Kanban and Terragon (company shutdowns), and the local worktree managers (single-machine).
