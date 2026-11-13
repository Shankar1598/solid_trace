# Capturing coding-agent transcripts and telemetry into SolidTrace

Research for the software-factory effort, 2026-11-13. Scope: how sessions of Claude Code, Codex and other coding agents running on many VMs can be captured and shipped into SolidTrace's ClickHouse, and which standard formats exist. Warp and the orchestrators are covered elsewhere.

Claims link to primary sources: vendor docs, upstream source code, or this repo. "Observed" means seen on this dev box (Claude Code 2.1.322 transcripts). "Unconfirmed" means a point I couldn't verify in a primary source.

## Summary

- **Claude Code** has four capture surfaces:
  - **OpenTelemetry:** metrics, log events and beta traces over OTLP. Prompt, response and tool content can be included, but each is behind its own opt-in. ([monitoring](https://code.claude.com/docs/en/monitoring-usage))
  - **Hooks:** 33 events. Each payload carries `session_id` and `transcript_path`, and `http` hooks POST the payload to a URL. ([hooks](https://code.claude.com/docs/en/hooks))
  - **The on-disk JSONL transcript:** complete, but Anthropic calls its format "internal to Claude Code" and says it "changes between versions". ([sessions](https://code.claude.com/docs/en/sessions#where-transcripts-are-stored))
  - **`claude -p --output-format stream-json`:** documented, typed Agent SDK messages, ending in a `result` message that carries cost, usage, turn count and outcome. ([headless](https://code.claude.com/docs/en/headless), [SDK types](https://code.claude.com/docs/en/agent-sdk/typescript))
- **Codex CLI** has the same shape:
  - OTel log events (`codex.*`) configured in `[otel]`, with prompt text redacted by default. ([config-advanced](https://learn.chatgpt.com/docs/config-file/config-advanced))
  - JSONL "rollout" files under `~/.codex/sessions/YYYY/MM/DD/`, which are compressed to `.jsonl.zst` once cold. ([recorder.rs](https://github.com/openai/codex/blob/main/codex-rs/rollout/src/recorder.rs), [compression.rs](https://github.com/openai/codex/blob/main/codex-rs/rollout/src/compression.rs))
  - A documented `codex exec --json` event stream. ([non-interactive mode](https://learn.chatgpt.com/docs/non-interactive-mode))
- **Other agents:** Gemini CLI, Goose and OpenHands all export OTLP. Gemini CLI partly uses `gen_ai.*` names. opencode's OTel support is unconfirmed.
- **The standard is the OpenTelemetry GenAI semantic conventions,** now in their own repo, [semantic-conventions-genai](https://github.com/open-telemetry/semantic-conventions-genai). They are still **Development** status throughout, and the repo has no release yet. They define agent spans (`invoke_agent`, `plan`), tool spans (`execute_tool`) and inference spans, plus `gen_ai.*` metrics, with content capture off by default. Claude Code doesn't set `gen_ai.usage.*` ([monitoring](https://code.claude.com/docs/en/monitoring-usage)), so normalisation is our job.
- **Prior art:** Langfuse is the closest. Its v4 moved to one wide, mostly immutable ClickHouse `events_full` table, with trace attributes (session, user, tags) denormalised onto every row and a truncated `events_core` projection for lists and charts ([blog](https://langfuse.com/blog/2026-03-10-simplify-langfuse-for-scale), [DDL](https://github.com/langfuse/langfuse/blob/main/packages/shared/clickhouse/migrations/canonical/0039_create_events_full.up.sql)). It accepts OTLP over HTTP only, not gRPC ([OTel ingestion](https://langfuse.com/docs/opentelemetry/get-started)).
- **SolidTrace today has no OTLP endpoint.**
  - EventStore takes Sentry envelopes and JSON/NDJSON Logs ([routes.go](../../../event_store/handler/routes.go)).
  - A Log's strings are cut at 64 KiB, and a Log over 1 MiB is dropped ([normalise.go](../../../event_store/ingest/normalise/normalise.go)).
  - Observed transcript lines reach 540 KB and whole files 25 MB, so full transcripts don't fit the Logs table as it is.
- **Recommendation:**
  - First, an OTLP/HTTP logs receiver in EventStore, feeding ordinary Logs. It needs only env vars on the VMs, and serves Claude Code, Codex and Gemini CLI alike.
  - Second, a session-end upload of the raw transcript (JSONL, rollout or stream-json) into new transcript tables, parsed on the server.
  - Then OTLP traces into `spans`.

## 1. Claude Code

### 1.1 OpenTelemetry

**Enabling it.** Telemetry is enabled with `CLAUDE_CODE_ENABLE_TELEMETRY=1`. The other settings are standard OTel variables ([monitoring](https://code.claude.com/docs/en/monitoring-usage)):
- **Exporters:** `OTEL_METRICS_EXPORTER` (`otlp`, `prometheus`, `console`, `none`), `OTEL_LOGS_EXPORTER` and `OTEL_TRACES_EXPORTER`.
- **Protocol:** `OTEL_EXPORTER_OTLP_PROTOCOL` takes `grpc`, `http/json` or `http/protobuf`. It defaults to `http/protobuf` for http(s) endpoints from v2.1.323.
- **Endpoint and headers:** `OTEL_EXPORTER_OTLP_ENDPOINT` and `OTEL_EXPORTER_OTLP_HEADERS`, with per-signal variants of both.
- **Export intervals:** 60 s for metrics, 5 s for logs and 5 s for traces.
- **Resource attributes:** `OTEL_RESOURCE_ATTRIBUTES` adds attributes to every metric and event.
- **Rotating auth:** an `otelHeadersHelper` script can supply auth headers, over HTTP only.
- **Repository settings can't redirect it:** a repository's `.claude/settings.json` "cannot enable telemetry, choose destinations, or capture content". `OTEL_*` variables are not passed to Bash, hooks or MCP servers. ([monitoring](https://code.claude.com/docs/en/monitoring-usage))

**Content gates.** All are off by default ([monitoring](https://code.claude.com/docs/en/monitoring-usage)):
- **`OTEL_LOG_USER_PROMPTS=1`:** prompt text.
- **`OTEL_LOG_ASSISTANT_RESPONSES`:** response text. Text blocks only; thinking and tool-use blocks are excluded. It falls back to the prompts gate when unset.
- **`OTEL_LOG_TOOL_DETAILS=1`:** tool parameters and input. A `tool_input` value is truncated at 512 chars, and the payload is capped near 4K chars.
- **`OTEL_LOG_TOOL_CONTENT=1`:** tool output, as a `tool.output` span event. Needs tracing.
- **`OTEL_LOG_RAW_API_BODIES=1|file:<dir>`:** full request and response bodies. Inline bodies are truncated; `file:` mode writes them whole. Bodies "include full conversation history" and imply consent to all the other gates.
- **Limit:** content attributes are cut at `CLAUDE_CODE_OTEL_CONTENT_MAX_LENGTH`, 60 KB by default.
- **Never shipped as content:** thinking is redacted in raw bodies.
- **Identity:** `user.email` is included under OAuth.

**Metrics** ([monitoring](https://code.claude.com/docs/en/monitoring-usage)):

| Metric | What it counts |
|---|---|
| `claude_code.session.count` | Sessions started |
| `claude_code.lines_of_code.count` | Lines added and removed |
| `claude_code.pull_request.count` | Pull requests created |
| `claude_code.commit.count` | Commits created |
| `claude_code.cost.usage` | USD |
| `claude_code.token.usage` | Tokens; `type` is `input`, `output`, `cacheRead` or `cacheCreation` |
| `claude_code.code_edit_tool.decision` | Edit decisions |
| `claude_code.active_time.total` | Active time |

Cost and token metrics carry `model`, `query_source` (`main`, `subagent` or `auxiliary`), `agent.name`, `skill.name` and `mcp_server.name`. Metric temporality is delta by default.

**Log events.** These are OTel log records named `claude_code.<name>` ([monitoring](https://code.claude.com/docs/en/monitoring-usage)):

| Event | Attributes |
|---|---|
| `user_prompt` | `prompt_length`, `prompt` (gated), `command_name` |
| `assistant_response` | `response` (gated), `model`, `request_id`, `message.uuid` |
| `tool_result` | `tool_name`, `tool_use_id`, `success`, `duration_ms`, `error`, `decision_source`, input and result sizes, `tool_parameters` and `tool_input` (gated). Not emitted for rejected calls. |
| `api_request` | `model`, `cost_usd`, `cost_usd_micros`, `duration_ms`, `input_tokens`, `output_tokens`, `cache_read_tokens`, `cache_creation_tokens` (5m and 1h too), `request_id`, `speed`, `effort`, `query_source` |
| `api_error` | `status_code`, `attempt`, `error` |
| `api_refusal` | Refusal details |
| `tool_decision` | `decision`, `source` (`config`, `hook`, `user_*`), `permission_mode`, `classifier_verdict` |
| `api_request_body` and `api_response_body` | Only with raw bodies on |
| `subagent_completed` | `agent_type`, `total_tool_uses`, `duration_ms`, `model` |

Compaction, hook, MCP connection, plugin, skill and retention-sweep events also exist. `safety_event` is no longer sent from v2.1.312.

**Correlation attributes.**
- **On everything:** `session.id` (replaced by `/clear`), `user.id`, `user.account_uuid`, `organization.id`, `terminal.type`, and optionally `app.version` and `vcs.*`.
- **On events too:** `prompt.id` (a UUID per user prompt), `event.sequence` (a per-process counter), `event.timestamp`, `agent_id`, `parent_agent_id` and `workflow.run_id`.
- **Joining to the transcript:** `message.uuid` "matches the session transcript".

([monitoring](https://code.claude.com/docs/en/monitoring-usage))

**Traces (beta).**
- **Enabling:** `CLAUDE_CODE_ENHANCED_TELEMETRY_BETA=1` plus `OTEL_TRACES_EXPORTER`.
- **Span tree:** `claude_code.interaction` → `claude_code.llm_request`, and `claude_code.tool` → `tool.blocked_on_user` and `tool.execution`, with subagent spans under the Agent tool.
- **Span attributes:** `claude_code.llm_request` carries `model`, token counts, `ttft_ms`, `stop_reason` and a few `gen_ai.*` keys (`gen_ai.system`, `gen_ai.request.model`, `gen_ai.response.id`, `gen_ai.response.finish_reasons`). `claude_code.tool` carries `tool_use_id` and `gen_ai.tool.call.id`.
- **Inbound trace context:** in `-p` and Agent SDK sessions, an inbound `TRACEPARENT` is honoured, so an orchestrator can put a whole agent run inside its own trace. Interactive sessions ignore it.
- **Outbound trace context:** subprocesses get `TRACEPARENT`.
- **Detailed beta tracing:** `ENABLE_BETA_TRACING_DETAILED` adds `claude_code.hook` spans and full content attributes, and is marked not part of the stable schema.

([monitoring](https://code.claude.com/docs/en/monitoring-usage))

**Mapping tokens to GenAI names.** "Claude Code doesn't set `gen_ai.usage.*` attributes". Its `input_tokens` exclude cache reads and writes, whereas `gen_ai.usage.input_tokens` should include them. Map:
- `cache_read_tokens` to `gen_ai.usage.cache_read.input_tokens`;
- `cache_creation_tokens` to `gen_ai.usage.cache_write.input_tokens`.

([monitoring](https://code.claude.com/docs/en/monitoring-usage#map-input-tokens-to-opentelemetry-genai-semantic-conventions))

**Unconfirmed:** what the OTel log record `body` holds for each event, and how many retries the exporter makes before dropping a batch when the endpoint is down.

### 1.2 Hooks

There are 33 hook events, among them `SessionStart`, `UserPromptSubmit`, `PreToolUse`, `PostToolUse`, `PostToolUseFailure`, `PostToolBatch`, `SubagentStart`, `SubagentStop`, `Stop`, `StopFailure`, `PreCompact`, `PostCompact` and `SessionEnd`. ([hooks](https://code.claude.com/docs/en/hooks))

**Common input on every event:**
- `session_id` and `prompt_id`;
- `transcript_path`, which is "written asynchronously and may lag the current turn";
- `cwd`, `permission_mode`, `effort` and `hook_event_name`;
- `agent_id` and `agent_type` inside subagents.

([hooks](https://code.claude.com/docs/en/hooks))

**Payloads per event** ([hooks](https://code.claude.com/docs/en/hooks)):

| Event | Fields |
|---|---|
| `UserPromptSubmit` | `prompt` |
| `PreToolUse` | `tool_name`, `tool_input`, `tool_use_id` |
| `PostToolUse` | `tool_input`, `tool_response` (the tool's structured output), `tool_use_id`, `duration_ms` |
| `PostToolUseFailure` | `error`, `is_interrupt` |
| `PostToolBatch` | `tool_calls[]` with the serialised `tool_response` the model saw |
| `Stop` | `stop_hook_active`, `last_assistant_message`, `background_tasks` |
| `StopFailure` | `error` and the API error text |
| `SubagentStop` | `agent_id`, `agent_type`, `agent_transcript_path`, `last_assistant_message` |
| `PostCompact` | `compact_summary` |
| `SessionEnd` | `reason` (`clear`, `resume`, `logout`, `prompt_input_exit`, `other`) |
| `SessionStart` | `source`, `model`; on resume also `context_tokens` |

No hook payload carries per-request token usage or cost.

**Hook types:**
- `command`: input on stdin. Can run `async: true`.
- `http`: the JSON input is POSTed to a URL. A non-2xx response is a non-blocking error.
- `mcp_tool`, `prompt` and `agent`.

**Limits:**
- Hooks block by default.
- `SessionEnd` hooks have a 1.5 s default budget, raised with `timeout` or `CLAUDE_CODE_SESSIONEND_HOOKS_TIMEOUT_MS`.
- Under `-p`, async hooks still running at teardown are killed.
- `--bare` runs no hooks.

([hooks](https://code.claude.com/docs/en/hooks), [headless](https://code.claude.com/docs/en/headless))

### 1.3 On-disk transcript (`~/.claude/projects/<project>/<session-id>.jsonl`)

**Documented** ([sessions](https://code.claude.com/docs/en/sessions#where-transcripts-are-stored)):
- **Path:** `<project>` is the cwd with non-alphanumerics replaced by `-`. It is truncated with a hash past 200 chars.
- **Line types:** each line is "a message, a tool use or result, a record of a request Claude Code sent the model, or a metadata entry".
- **Stability:** the format is internal and "changes between versions".
- **Retention:** 30 days, set by `cleanupPeriodDays`.
- **Moving or naming the store:** `CLAUDE_CONFIG_DIR` moves it, and `CLAUDE_CODE_PROJECT_DIR_NAME` names the project directory.
- **Turning writes off:** `--no-session-persistence` or `CLAUDE_CODE_SKIP_PROMPT_HISTORY`.

**Observed** in a 4.4 MB session on 2.1.322. Record types, by `type`:

| Type | Notes |
|---|---|
| `user` | Has `message` (Anthropic `MessageParam`), `toolUseResult` (the tool's structured result, e.g. `stdout`, `stderr`, `structuredPatch`, `filePath`, `agentId`), `promptId`, `sourceToolUseID` |
| `assistant` | Has `message` (the Anthropic response, with `id`, `model`, `stop_reason` and `usage`: `input_tokens`, `output_tokens`, `cache_read_input_tokens`, `cache_creation_input_tokens`, `service_tier`, `speed`), `requestId`, `effort` |
| `system` | Subtypes `turn_duration`, `compact_boundary`, `away_summary`, `local_command`, `informational` |
| `attachment` | |
| `api-request`, `api-request-blob`, `api-request-shape` | |
| `queue-operation` | |
| `permission-mode` and `mode` | |
| `session-owner` | `accountUuid`, `organizationUuid` |
| `ai-title` | |
| `last-prompt` | |
| `tools-used` | |
| `file-history-snapshot` and `file-history-delta` | |

Common fields on messages: `uuid`, `parentUuid` (a tree, not a list), `sessionId`, `timestamp`, `cwd`, `gitBranch`, `version`, `entrypoint` and `isSidechain`.

**Gotchas, observed:**
- **One API response spans several lines.** It is written as one `assistant` line per content block: 277 lines for 137 `message.id`s, and up to 9 lines for one id. Each line repeats `usage`, so token sums must deduplicate by `message.id` or `requestId`.
- **Subagents have their own files,** at `<session-id>/subagents/agent-<id>.jsonl`, each with a `.meta.json` beside it (`agentType`, `description`, `toolUseId`, `spawnDepth`). The `SubagentStop` hook's `agent_transcript_path` points at the same layout ([hooks](https://code.claude.com/docs/en/hooks)).
- **Size:** the largest line was 554 KB, and session files reached 25 MB.

### 1.4 Headless output (`claude -p --output-format json|stream-json`)

- **`json`** returns one object: `result`, `session_id`, `total_cost_usd`, a per-model breakdown, `usage` and `structured_output`. Costs are "client-side estimates". ([headless](https://code.claude.com/docs/en/headless))
- **`stream-json`** (with `--verbose`) emits NDJSON SDK messages ([SDK types](https://code.claude.com/docs/en/agent-sdk/typescript)):
  - **`system/init`:** `session_id`, `model`, `cwd`, `tools`, `mcp_servers`, `permissionMode`, `claude_code_version`, `plugins`.
  - **`assistant`:** `message` (a full `BetaMessage` with usage), `parent_tool_use_id`, `agent_id`.
  - **`user`:** `tool_result` blocks, `tool_use_result`.
  - **`stream_event`:** only with `--include-partial-messages`.
  - **`system/api_retry`**, **`system/compact_boundary`**, and **`permission_denied`**.
  - **A final `result`:** `subtype` is `success`, `error_max_turns`, `error_during_execution`, `error_max_budget_usd` or `error_max_structured_output_retries`. It also carries `is_error`, `num_turns`, `duration_ms`, `duration_api_ms`, `total_cost_usd`, `usage`, `modelUsage`, `permission_denials`, `terminal_reason` and `errors[]`.
- **Subagent messages:**
  - They carry `parent_tool_use_id`.
  - Their text and thinking are forwarded only with `--forward-subagent-text`.
  - Without it, only their tool blocks appear. ([headless](https://code.claude.com/docs/en/headless#follow-subagent-messages))
- **Exit codes:** SIGTERM leaves the turn with no result and exit code 143. ([headless](https://code.claude.com/docs/en/headless#stop-a-run-with-sigterm))

## 2. Codex CLI (OpenAI)

**OpenTelemetry.** It is configured in `config.toml` under `[otel]` ([config-advanced](https://learn.chatgpt.com/docs/config-file/config-advanced)):
- **Keys:** `environment` (default `"dev"`), `exporter` (default `"none"`), `log_user_prompt` (default `false`), `metrics_exporter`, `log_agent_responses` and `log_guardian_assessments`.
- **Exporters:** `otlp-http` (`endpoint`, `protocol = "binary"`, `headers`), `otlp-grpc` or `none`. Exporters "batch asynchronously and flush on shutdown".
- **Event metadata:** service name, CLI version, env tag, conversation id, model, and sandbox and approval settings.
- **Events:**

  | Event | Content |
  |---|---|
  | `codex.conversation_starts` | Session settings |
  | `codex.api_request` | Attempt, status, duration |
  | `codex.sse_event` | Token counts on `response.completed` |
  | `codex.websocket_request` and `codex.websocket_event` | WebSocket traffic |
  | `codex.user_prompt` | Length only, unless `log_user_prompt` is set |
  | `codex.agent_response` | Opt-in |
  | `codex.tool_decision` | Approval decisions |
  | `codex.tool_result` | Duration, success, "an output snippet" |

- **Metrics:** counters and `.duration_ms` histograms, e.g. `codex.tool.call`.
- **Redaction:** prompt content is redacted by default, and the `tool` field doesn't include the shell command or patch contents.
- **Traces:** the page doesn't document a trace exporter key. Third-party guides mention `trace_exporter` (unconfirmed).

**`codex exec --json`.** It writes JSONL to stdout ([non-interactive mode](https://learn.chatgpt.com/docs/non-interactive-mode)):
- **Events:** `thread.started` (with `thread_id`), `turn.started`, `turn.completed`, `turn.failed`, `item.started`, `item.updated`, `item.completed` and `error`.
- **Item types:** `agent_message` and `command_execution` are named on the page. It describes reasoning, file changes, MCP tool calls, web searches and plan updates in prose, so their exact type strings are unconfirmed.
- **Usage:** `turn.completed.usage` carries `input_tokens`, `cached_input_tokens`, `output_tokens` and `reasoning_output_tokens`. No cost is given; it must be computed from a price table.
- **Other flags:** `-o` writes the last message to a file, `--output-schema` asks for structured output, `codex exec resume <SESSION_ID>` resumes a session, and `--ephemeral` "avoids persisting session rollout files".

**Rollout files** (from source, not docs):
- **Path:** `~/.codex/sessions/YYYY/MM/DD/rollout-<timestamp>-<thread_id>.jsonl` ([recorder.rs](https://github.com/openai/codex/blob/main/codex-rs/rollout/src/recorder.rs)). Archived sessions go to `archived_sessions` ([lib.rs](https://github.com/openai/codex/blob/main/codex-rs/rollout/src/lib.rs)).
- **Compression:** a background job compresses cold rollouts to `.jsonl.zst` ([compression.rs](https://github.com/openai/codex/blob/main/codex-rs/rollout/src/compression.rs)). A tailer must handle both forms.
- **Line types:** each line is `{type, payload}`. The types are `session_meta`, `dynamic_tools_updated`, `response_item` (OpenAI Responses API items), `inter_agent_communication`, `compacted`, `turn_context`, `token_usage_record`, `world_state`, `retained_context`, `security_risk_score`, `event_msg` and `realtime_item` ([rollout_payload.rs](https://github.com/openai/codex/blob/main/codex-rs/history/src/rollout_payload.rs)).
- **`session_meta`:** `id`, `timestamp`, `cwd`, `originator`, `cli_version`, `source`, `model_provider` and `git` ([protocol.rs](https://github.com/openai/codex/blob/main/codex-rs/protocol/src/protocol.rs)).
- **Token usage:** `TokenUsage` has `input_tokens`, `cached_input_tokens`, `cache_write_input_tokens`, `output_tokens`, `reasoning_output_tokens` and `total_tokens`. `TokenUsageInfo` has `total_token_usage`, `last_token_usage` and `model_context_window` ([protocol.rs](https://github.com/openai/codex/blob/main/codex-rs/protocol/src/protocol.rs)).
- **Stability:** this is an internal format that is still growing variants.
- **Prompt history** is separate, in `~/.codex/history.jsonl` under `CODEX_HOME` ([config-advanced](https://learn.chatgpt.com/docs/config-file/config-advanced)).
- **Hooks:** Codex has a hooks module ([hooks.rs](https://github.com/openai/codex/blob/main/codex-rs/protocol/src/hooks.rs)). Its events and payloads were not checked (unconfirmed).

## 3. Other agents (brief)

**Gemini CLI** ([telemetry.md](https://github.com/google-gemini/gemini-cli/blob/main/docs/cli/telemetry.md)):
- **Config:** `telemetry.*` settings or `GEMINI_TELEMETRY_*` env: `target`, `otlpEndpoint`, `otlpProtocol` (`grpc` or `http`), `outfile` and `traces`.
- **Prompts are logged by default:** `logPrompts` defaults to `true`.
- **Events:** `gemini_cli.user_prompt`, `gemini_cli.tool_call`, `gemini_cli.api_request`, `gemini_cli.api_response`, `gemini_cli.api_error`, `gemini_cli.chat_compression`, `gemini_cli.agent.start` and `gemini_cli.agent.finish`. It also emits the standard `gen_ai.client.inference.operation.details` event.
- **Metrics:** `gemini_cli.*`, plus the standard `gen_ai.client.token.usage` and `gen_ai.client.operation.duration`.
- **Spans:** they use `gen_ai.*` attributes.
- **Chat storage:** where chats are stored on disk isn't in that doc (unconfirmed).

**opencode** (now under `anomalyco/opencode`):
- **Storage:** sessions, messages and parts are JSON under `~/.local/share/opencode/storage/` ([codeburn notes](https://raw.githubusercontent.com/getagentseal/codeburn/main/docs/providers/opencode.md)). A 2026 write-up describes a SQLite `opencode.db` with `session`, `message` and `part` tables ([DevGenius](https://blog.devgenius.io/agent-session-persistence-in-opencode-9c3440448c06)). Both are third-party, and they disagree (unconfirmed).
- **OTel:** the v1.4.5 changelog is titled "Export AI SDK telemetry spans to OTLP trace backends" ([changelog](https://opencode.ai/changelog/v1.4.5.md)). Integration guides set `experimental.openTelemetry: true` ([Langfuse guide](https://langfuse.com/integrations/developer-tools/opencode.md)). Exact keys are unconfirmed.

**Goose** (docs now published by AAIF):
- **Storage:** sessions live in SQLite at `~/.local/share/goose/sessions/sessions.db`. The last 10 LLM requests are kept as `~/.local/state/goose/logs/llm_request.*.jsonl` ([logs](https://goose-docs.ai/docs/guides/logs)).
- **OTel:** it exports over OTLP/HTTP using the standard `OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_TRACES_EXPORTER` and related variables. Message content is added with `OTEL_INSTRUMENTATION_GENAI_CAPTURE_MESSAGE_CONTENT=true` ([MLflow tutorial](https://goose-docs.ai/docs/tutorials/mlflow)).

**OpenHands** (software-agent SDK):
- **Event stream:** `MessageEvent`, `ActionEvent` (tool calls), `ObservationEvent`, `UserRejectObservation`, `AgentErrorEvent`, `SystemPromptEvent`, `CondensationSummaryEvent`, `ConversationStateUpdateEvent` and `PauseEvent`. Each has a `source` of `user`, `agent` or `environment` ([events](https://docs.openhands.dev/sdk/arch/events.md)).
- **Persistence:** `workspace/conversations/<id>/base_state.json` plus `events/event-00000-<id>.json`, one file per event, appended ([persistence](https://docs.openhands.dev/sdk/guides/convo-persistence.md)).
- **Tracing:** spans `conversation`, `agent.step`, `llm.completion` and `tool.execute`, sent to Laminar or to any OTLP backend through `OTEL_EXPORTER_OTLP_TRACES_*` ([observability](https://docs.openhands.dev/sdk/guides/observability.md)).

## 4. Standards

### 4.1 OpenTelemetry GenAI semantic conventions

**Status and location:**
- **Moved:** the opentelemetry.io pages now say "GenAI semantic conventions have moved" to [open-telemetry/semantic-conventions-genai](https://github.com/open-telemetry/semantic-conventions-genai) ([opentelemetry.io](https://opentelemetry.io/docs/specs/semconv/gen-ai/)).
- **No release yet:** the repo was created 2026-05-05 and has none (GitHub API, checked 2026-11-13).
- **Status:** the top-level document and every span, event and attribute in it is **Development** ([README](https://github.com/open-telemetry/semantic-conventions-genai/blob/main/docs/gen-ai/README.md)). Nothing is stable. Expect renames: the cache-write attribute was previously `gen_ai.usage.cache_creation.input_tokens` ([monitoring](https://code.claude.com/docs/en/monitoring-usage)).

**Spans:**
- **Inference** ([client-inference](https://github.com/open-telemetry/semantic-conventions-genai/blob/main/docs/gen-ai/client-inference.md)): named `{gen_ai.operation.name} {gen_ai.request.model}`, kind CLIENT.
- **Agents** ([agent spans](https://github.com/open-telemetry/semantic-conventions-genai/blob/main/docs/gen-ai/gen-ai-agent-spans.md)):
  - `create_agent`;
  - `invoke_agent`, used when an LLM drives the control flow;
  - `invoke_workflow`, used when the application does;
  - `plan`;
  - all named `{operation} {gen_ai.agent.name}`.
- **Tools** ([execute-tool](https://github.com/open-telemetry/semantic-conventions-genai/blob/main/docs/gen-ai/execute-tool.md)): `execute_tool`, with `gen_ai.tool.name`, `gen_ai.tool.call.id`, and opt-in `gen_ai.tool.call.arguments` and `gen_ai.tool.call.result`. It has refinements for agent-as-tool, skill loading and command execution, and cites Anthropic's bash tool as an example of the latter.

**Key attributes** ([registry](https://github.com/open-telemetry/semantic-conventions-genai/blob/main/docs/registry/attributes/gen-ai.md)):
- `gen_ai.conversation.id` (session or thread), the only attribute meant to be propagated to nested spans;
- `gen_ai.agent.name`, `gen_ai.agent.id`, `gen_ai.operation.name`, `gen_ai.request.model` and `gen_ai.response.finish_reasons`;
- `gen_ai.usage.input_tokens`, `gen_ai.usage.output_tokens`, `gen_ai.usage.cache_read.input_tokens` and `gen_ai.usage.cache_write.input_tokens`. Input includes cache.
- Opt-in content: `gen_ai.input.messages`, `gen_ai.output.messages`, `gen_ai.system_instructions`, `gen_ai.tool.definitions`. Each also has a `*_ref` form that holds a URI to external storage, such as `s3://…`.

**Content capture** ([content capture](https://github.com/open-telemetry/semantic-conventions-genai/blob/main/docs/gen-ai/gen-ai-spans.md)):
- It is off by default.
- If set by environment, it MUST use `OTEL_INSTRUMENTATION_GENAI_CAPTURE_CONTENT`, with the values `NO_CONTENT` (the default), `SPAN_ONLY`, `EVENT_ONLY` and `SPAN_AND_EVENT`.

**Events:**
- `gen_ai.client.inference.operation.details` holds the chat history and parameters, at DEBUG severity ([client-inference](https://github.com/open-telemetry/semantic-conventions-genai/blob/main/docs/gen-ai/client-inference.md)).
- `gen_ai.evaluation.result` holds `gen_ai.evaluation.name`, `score.value`, `score.label` and `explanation` ([events](https://github.com/open-telemetry/semantic-conventions-genai/blob/main/docs/gen-ai/gen-ai-events.md)). This is a standard shape for "scores".

**Metrics:**
- `gen_ai.client.operation.duration`;
- agent metrics `gen_ai.invoke_agent.duration`, `gen_ai.invoke_agent.inference_calls` and `gen_ai.invoke_agent.tool_calls`;
- tool metric `gen_ai.execute_tool.duration`;
- token counters `gen_ai.client.inference.usage.*`.

([metrics](https://github.com/open-telemetry/semantic-conventions-genai/blob/main/docs/gen-ai/gen-ai-metrics.md), [token metrics](https://github.com/open-telemetry/semantic-conventions-genai/blob/main/docs/gen-ai/gen-ai-token-metrics.md))

**Who emits these today:**
- Claude Code: a handful of `gen_ai.*` keys on its spans.
- Gemini CLI: two metrics, its span attributes and the details event.
- Goose: the content-capture variable.
- Codex: none found.

### 4.2 OpenInference (Arize)

OpenInference is a separate span convention, used by Phoenix ([spec](https://github.com/Arize-ai/openinference/blob/main/spec/semantic_conventions.md)):
- **`openinference.span.kind`:** `LLM`, `CHAIN`, `TOOL`, `AGENT`, `RETRIEVER`, `EMBEDDING`, `RERANKER`, `GUARDRAIL`, `EVALUATOR`, `PROMPT` or `DECISION`.
- **Input and output:** `input.value` and `output.value`.
- **Messages:** flattened as `llm.input_messages.N.message.*`.
- **Tokens:** `llm.token_count.prompt`, `llm.token_count.completion`, `llm.token_count.total` and `…prompt_details.cache_read`.
- **Cost:** `llm.cost.*`.
- **Tools:** `tool.name` and `tool.parameters`.
- **Session and user:** `session.id` and `user.id`.

No coding agent here emits it natively. It matters only if SolidTrace wants to ingest from frameworks instrumented with OpenInference. Langfuse maps `input.value` and `output.value` ([Langfuse OTel](https://langfuse.com/docs/opentelemetry/get-started)).

## 5. Prior art for storage

### Langfuse (ClickHouse)

**Data model:** traces made of nested observations, optional sessions grouping traces, and scores ([data model](https://langfuse.com/docs/observability/data-model)).
- **Observation types:** `span`, `generation`, `event`, `agent`, `tool`, `chain`, `retriever`, `evaluator`, `embedding` and `guardrail`. Only generations carry model, usage and cost ([OTel](https://langfuse.com/docs/opentelemetry/get-started)).

**v3 storage** ([blog](https://langfuse.com/blog/2026-03-10-simplify-langfuse-for-scale)):
- Separate `traces`, `observations` and `scores` tables, as `ReplacingMergeTree` with full-row updates.
- Read-time dedupe (`FINAL` or `LIMIT BY`) and joins across billions of rows. Nodes grew 15x while data grew 19x, and queries still ran out of memory.

**v4 storage:**
- **`events_full`:** one row per observation, with `user_id`, `session_id`, `tags`, `release` and `trace_name` on every row ([DDL](https://github.com/langfuse/langfuse/blob/main/packages/shared/clickhouse/migrations/canonical/0039_create_events_full.up.sql)). Columns worth noting:
  - `usage_details Map(LowCardinality(String), UInt64)` and `cost_details Map(LowCardinality(String), Decimal(18,12))`, with materialised input, output and total cost;
  - `tool_calls Array(String)`, `tool_call_names Array(String)`, `tool_definitions Map(String,String)`;
  - `input` and `output` as `String CODEC(ZSTD(3))`, with materialised lengths;
  - metadata as parallel name and value arrays;
  - `service_name` and `scope_name` from OTel.
- **Table settings:** `ReplacingMergeTree(event_ts, is_deleted)`, `PARTITION BY toYYYYMM(start_time)`, `ORDER BY (project_id, toStartOfMinute(start_time), xxHash32(trace_id), span_id, start_time)`, `index_granularity_bytes = 64Mi` and a full-text index.
- **`events_core`:** a materialised-view projection "truncating large metadata, input, and output fields". It serves tables and charts, and `events_full` is read only for single records ([upgrade guide](https://langfuse.com/self-hosting/upgrade/upgrade-guides/upgrade-v3-to-v4)).
- **Requirements:** ClickHouse 25.12 or later.
- **Scores:** `scores` has `name`, `value Float64`, `string_value`, `data_type`, `source`, `comment`, `trace_id`, `observation_id` and `session_id`, ordered by `(project_id, toDate(timestamp), name, id)` ([scores DDL](https://github.com/langfuse/langfuse/blob/main/packages/shared/clickhouse/migrations/canonical/0003_scores.up.sql)).

**OTel ingestion** ([OTel](https://langfuse.com/docs/opentelemetry/get-started)):
- Endpoint `/api/public/otel/v1/traces`, OTLP over HTTP protobuf or JSON, with no gRPC, and Basic auth.
- Traces only.
- `session.id` and `user.id` map to session and user. Unmapped attributes land in metadata.
- Trace-level attributes must be propagated to every span, for example by baggage.

### Helicone

- **Capture:** a proxy or gateway in front of the provider records every request and response.
- **Storage:** ClickHouse `request_response_rmt`, with `session_id`, `session_name` and `session_path` columns added from the `Helicone-Session-*` headers ([PR #3595](https://github.com/Helicone/helicone/pull/3595), [HQL](https://docs.helicone.ai/features/hql.md)).
- **Sessions:** a session path like `/parent/child` encodes the hierarchy ([sessions](https://docs.helicone.ai/features/sessions.md)).
- **Lesson:** a proxy sees exact usage and full bodies, but not tool execution except as it reappears in the next request.

### Phoenix (Arize)

- **Shape:** a UI, a trace collector and SQLite or PostgreSQL. It isn't ClickHouse-based ([architecture](https://arize.com/docs/phoenix/self-hosting/architecture)).
- **Ingest:** OTLP traces over HTTP at `/v1/traces` on the UI port 6006, and gRPC on 4317 ([configuration](https://www.arize.com/docs/phoenix/self-hosting/configuration.md)).
- **Model:** OpenInference spans grouped into projects, traces and sessions by `session.id`.

### OpenLIT

- **Ingest:** a built-in OTLP receiver on 4317 and 4318.
- **Storage:** ClickHouse `otel_*` tables for traces, metrics and logs ([install](https://docs.openlit.io/latest/openlit/installation)).
- **Lesson:** the shape of a generic OTel store with GenAI views on top.

### The OTel Collector's ClickHouse exporter

- **Stability:** beta for traces and logs.
- **Tables:** `otel_logs` and `otel_traces`, as `MergeTree`.
- **Logs ORDER BY:** `(toStartOfFiveMinutes(Timestamp), ServiceName, Timestamp)`.
- **JSON:** an optional `json: true` replaces Map columns with JSON.

([README](https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/main/exporter/clickhouseexporter/README.md))

### Worth copying

1. **Denormalise session-level fields onto every row,** as Langfuse v4 does: session id, agent, model, host, task id. Then no query needs a join. SolidTrace's Logs already work this way.
2. **Keep usage and cost as Maps** keyed by `input`, `output`, `cache_read`, `cache_write` and `reasoning`. Agents disagree on which buckets exist (Claude: cache creation 5m and 1h; Codex: reasoning and cache write).
3. **Store full content once, compressed,** with a truncated projection for lists. Read the full row only for one record.
4. **Have a scores table** for outcomes: tests passed, PR merged, human rating. Its shape can match `gen_ai.evaluation.result`.
5. **Use `*_ref` URIs,** as the GenAI conventions do, so large content can live in a bucket.

## 6. Capture design compared

| Path | Completeness | Reliability | Effort per VM | Format stability |
|---|---|---|---|---|
| **OTel push** (Claude, Codex, Gemini, Goose, OpenHands) | Usage, cost, tools, decisions, errors and timings in full. Prompts, responses and tool input only when gated on, and truncated: 60 KB per attribute, 512 chars per tool-input value. Tool output only in beta traces. | In-memory batches, 5 s for logs. Lost if the process or VM dies first. Retry behaviour unconfirmed. | Env vars only. Can be locked in managed settings. | Documented. Claude's events are versioned in its docs. GenAI conventions are Development. |
| **Tail the JSONL** (Claude transcript, Codex rollout) | Everything: messages, tool inputs and outputs, usage per response, subagents in separate files | Offsets checkpoint and survive restarts. Must handle per-block lines, files that are rewritten or compressed (Codex `.zst`), and lines over 500 KB. | A shipper on every VM | Internal, "changes between versions" ([sessions](https://code.claude.com/docs/en/sessions#where-transcripts-are-stored)) |
| **Upload at session end** (hook or orchestrator) | Everything, once | No live view. A crash or SIGKILL skips `SessionEnd`, so a sweeper is needed. | A hook or wrapper script plus a sweeper | Same as tailing, but parsing moves to the server, where versions can be handled in one place |
| **Hooks streaming** (`http` hooks) | Prompts, full `tool_input` and `tool_response`, final messages, compaction summaries. No token usage. | Synchronous by default, adding latency to every tool call. A failing endpoint is non-blocking. | Settings only | Documented |
| **stream-json or `exec --json` wrapper** | Full messages and the final `result` with cost, usage, turns and outcome. Subagent text needs `--forward-subagent-text`. | Only for runs the orchestrator launches. Lost if the wrapper dies, unless it tees to disk. | Lives in the orchestrator | Documented SDK types |
| **API proxy** (gateway via `ANTHROPIC_BASE_URL`) | Exact request and response bodies | One more service in the critical path | Network config | Provider API |

**Shippers** (only needed for tailing):
- **OTel Collector `filelog` receiver:** beta. It persists offsets with a storage extension and resumes after restarts ([filelog](https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/main/receiver/filelogreceiver/README.md)).
- **Vector `file` source:** checkpoints after each read, but discards lines over `max_line_bytes`, which defaults to 100 KiB. That must be raised for transcripts ([Vector file](https://vector.dev/docs/reference/configuration/sources/file/)).
- **Fluent Bit:** has a tail input. Its limits were not checked (unconfirmed).
- **Durability for OTel push:** a local collector with a persistent sending queue could buffer it, but that is a second process per VM.

**Redaction and secrets.** Transcripts hold whatever the agent read or ran: `.env` files, `env` output, tokens in command output, and keys in pasted prompts. This repo itself has a modified `.env` in its working tree.
- **At the agent, OTel path:** keep content gates off by default and turn on only the ones analysis needs. Never set `OTEL_LOG_RAW_API_BODIES`: it holds the full history of every request, so its volume grows quadratically. The rest of the agent side is already constrained: repository settings can't enable capture, OTel credentials aren't exposed to tools, and `user.email` is sent under OAuth ([monitoring](https://code.claude.com/docs/en/monitoring-usage)).
- **At the agent, transcript path:** `--no-session-persistence` and `--ephemeral` stop transcripts being written at all, where none are wanted.
- **In transit:** the Collector's `redaction` processor keeps an allow-list of attributes and masks or HMACs values that match regexes. It is beta for traces and alpha for logs ([redaction](https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/main/processor/redactionprocessor/README.md)).
- **At SolidTrace:** a Project's Log transform already runs VRL on every Log before it is stored ([glossary](../../../GLOSSARY.md), [ADR 0012](../../../docs/adr/0012-log-transform-runs-vrl-through-cgo.md)). That makes it one central place to mask known key formats. The caveat is that raw Logs sit in Pebble until Log processing runs the transform.
- **Transcript uploads:** they bypass the Log transform, so redact on the server while parsing, with the same rules. Keep the raw blob only if Retention and access allow it.
- **Proportion:** one redaction point (the server parser plus the Log transform) and content gates at the agent should be enough. A per-VM scanner is a later option.

## Recommendation for SolidTrace

### Order

1. **First: OTLP/HTTP logs into Logs.**
   - **Why:**
     - It needs no code on the VMs, only env vars or managed settings, and the same receiver serves Claude Code, Codex, Gemini CLI and Goose.
     - It delivers session, usage, cost, tool-call, error and timing data at once, into the existing Query, Dashboards and Monitors. For example, cost by model per day, tool failure rate, or a Monitor on `api_error` bursts.
     - Logs' flat dotted Attributes already anticipate OTLP resource, scope and record attributes sharing one namespace ([ADR 0002](../../../docs/adr/0002-logs-scoped-by-project-flat-attributes.md)).
   - **Agent settings:**
     - `OTEL_RESOURCE_ATTRIBUTES` carries `vm`, `task.id` and `factory.run_id`, so they land on every Log.
     - Turn on `OTEL_LOG_USER_PROMPTS` and `OTEL_LOG_TOOL_DETAILS`, and leave raw bodies off.
2. **Second: transcript upload at session end, into new tables.**
   - **Who uploads:**
     - The orchestrator POSTs the finished file after the agent exits: Claude's `<session>.jsonl` plus its `subagents/` files, or a Codex rollout. For headless runs it can upload the stream-json it captured instead, which is the documented format.
     - A `SessionEnd` hook does the same for interactive sessions. Its timeout needs raising, or it hands off to a background process.
     - A sweeper uploads leftovers after a crash.
   - **What EventStore keeps:** the raw lines always, parsed on the server per agent and version, so format drift is fixed in one place and can be re-parsed later.
3. **Third: OTLP traces into `spans`.**
   - This gives the per-turn waterfall (interaction, then LLM requests and tools).
   - Pass `TRACEPARENT` from the orchestrator in `-p` runs, so an agent run joins the factory's own Trace across Projects. The Trace concept and `trace_index` already do this cross-Project join ([traces.md](../../../docs/traces.md)).
4. **Skip OTLP metrics at first.** Claude's `api_request` events and Codex's `sse_event` carry the same numbers per request, and SolidTrace has no metrics store.

### Minimal data model

**Entities:**

| Entity | Key | Source fields | Where |
|---|---|---|---|
| **Session** | `session_id` (Claude `session.id`; Codex conversation or `thread_id`) | agent, agent version, VM or host, cwd, repo, branch, models, started and ended, end reason, outcome (`result.subtype`, `is_error`, exit code), turns, token and cost totals, task id, trace id, transcript ref | New `agent_sessions` table (`ReplacingMergeTree` on `updated_at`), built from OTel Logs and the upload. At first it can be a Query over Logs. |
| **Turn** | `prompt.id` (Claude), turn (Codex) | prompt (gated), duration, tokens, cost | A Span (`claude_code.interaction`) once traces land. Until then, Logs grouped by `prompt.id`. |
| **LLM request (usage)** | `request_id` | model, input, output, cache read and cache write tokens, cost, duration, ttft, attempt, error | Logs (`claude_code.api_request`, `codex.sse_event`), then a Span (`claude_code.llm_request`) |
| **Tool call** | `tool_use_id` / `gen_ai.tool.call.id` | name, input (truncated), success, duration, decision, error, output ref | Logs (`tool_result`, `tool_decision`), then a Span (`claude_code.tool`) |
| **Message** (transcript line) | (`session_id`, `seq`) | type, uuid, parent uuid, agent id or sidechain, role, content JSON, tool use id, message id, usage | New `agent_messages` table |
| **Score** | (`session_id`, name) | value, label, source, comment | New `agent_scores` table, shaped like `gen_ai.evaluation.result` and Langfuse `scores` |

**Mapping onto what exists:**
- **OTel log record → Log:**
  - time and severity → time and Level;
  - `body` (or `event.name` if the body is empty, unconfirmed which Claude uses) → `message`;
  - `service.name` → `service`;
  - `trace_id` and `span_id` → built-ins;
  - resource, scope and record attributes → Attributes, flattened as ADR 0002 says.
- **Log limits:** each value fits the 64 KiB string limit, because Claude truncates content at 60 KB.
- **Project:** one Project, or one per team, with `service` = `claude-code` or `codex`.
- **OTel span → `spans` row:**
  - trace, span and parent ids carry over;
  - `transaction` = the root span name, or the session;
  - `op` = the span name (`claude_code.tool`);
  - `description` = the tool name or model;
  - `attributes Map(String,String)` holds the rest.
  ([spans DDL](../../../event_store/storage/spans_clickhouse.go))
- **Span gaps:** the table is Sentry-shaped. `group_hash`, `self_time_us` and `sample_rate` need defaults.
- **Transcript lines don't fit Logs:** observed lines reach 554 KB against the 64 KiB string cut and the 1 MiB Log drop ([normalise.go](../../../event_store/ingest/normalise/normalise.go)), and `search_text` would duplicate them. Use a new table instead.

**Sketch:**

```sql
CREATE TABLE agent_messages (
  project_id UInt32, session_id String, seq UInt32,
  agent LowCardinality(String), agent_version LowCardinality(String),
  time DateTime64(3, 'UTC'), type LowCardinality(String),
  uuid String, parent_uuid String, agent_id String,   -- subagent / sidechain
  message_id String, tool_use_id String, tool_name LowCardinality(String),
  usage Map(LowCardinality(String), UInt64),           -- deduped by message_id when summed
  content String CODEC(ZSTD(3)),                       -- the raw line, redacted
  content_bytes UInt32 MATERIALIZED length(content)
) ENGINE = MergeTree
PARTITION BY (project_id, toDate(time))
ORDER BY (project_id, session_id, seq);
```

The partitioning matches `logs` and `spans`, so Retention, Project deletion and the disk limit drop it the same way ([ADR 0016](../../../docs/adr/0016-logs-and-events-in-clickhouse.md)).

### What EventStore needs

1. **An OTLP/HTTP receiver.** gRPC isn't needed:
   - Claude Code defaults to `http/protobuf` for http(s) endpoints;
   - Codex, Gemini and Goose all speak HTTP;
   - Langfuse ships without gRPC.
   - **Routes:** `POST /v1/logs` first, then `/v1/traces`, accepting protobuf and JSON with gzip.
   - **Auth:** the Project key in a header, sent through `OTEL_EXPORTER_OTLP_HEADERS`, as `/api/logs` already finds the Project by key alone ([routes.go](../../../event_store/handler/routes.go)).
   - **Pipeline:** each request becomes a Log batch in Pebble before the `200`. Logs then flow through the normal Log processing and the Log transform. Traces become a range for Span processing.
   - **Normaliser:** a new one for OTLP records, beside the JSON and Sentry ones in `ingest/normalise/`.
2. **A transcript upload route.**
   - **Request:** `POST /api/:project_id/agent-transcripts` with `agent`, `agent_version`, `session_id` and a zstd or gzip NDJSON body. The body limit must exceed today's 10 MiB, or the upload must be chunked.
   - **Storage:** stored in Pebble, then inserted under a dedup token per (session, chunk), as Log processing does.
   - **Parsing:** server-side parsers per agent fill `agent_messages` and update `agent_sessions`, redacting as they go.
3. **Grants.** Add `agent_sessions`, `agent_messages` and `agent_scores` to the SQL user's grants and row policies, as `spans` was added ([ADR 0016](../../../docs/adr/0016-logs-and-events-in-clickhouse.md)). Add MCP tools for agents to read sessions and transcripts.
4. **Rejected: writing straight to ClickHouse.** Running the Collector's ClickHouse exporter into SolidTrace's ClickHouse would skip Pebble's durability, Project scoping, Retention and row policies. EventStore owns its writes ([ADR 0001](../../../docs/adr/0001-eventstore-writes-console-tables.md)).
