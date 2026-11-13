# Capturing a whole transcript, on a laptop and in a Nomad task

Research for [ticket 07](../issues/07-transcript-capture.md), 2026-11-13. It builds on [agent-transcripts.md](agent-transcripts.md), which lists the capture surfaces of Claude Code and Codex. This note covers only what that note left open: how the whole transcript gets to SolidTrace in each setting, what happens to it under resume, fork, compaction and subagents, what survives a killed task, and how it joins the OTLP events of the same session.

Sources:
- **Claude Code:** the vendor docs at code.claude.com, linked inline.
- **Codex:** the upstream source, [openai/codex](https://github.com/openai/codex) at commit `e8f5d2bb` (2026-11-13), and the vendor docs at learn.chatgpt.com.
- **Observed:** this machine's own Claude Code 2.1.322 transcripts under `~/.claude/projects/*solid-trace*`. Only their structure was counted; no content was copied.
- **Unverified:** marked where a claim has no primary source.

## Summary

- **The on-disk transcript is the only complete record, for both agents.** The documented streams (`stream-json` and `codex exec --json`) leave things out:
  - `stream-json` drops subagent text unless a flag asks for it.
  - `codex exec --json` drops subagent threads entirely.
  - Neither stream ends with a result line when the task is killed.
- **Upload a whole directory, not one file.** A Claude session is spread across several files:
  - `<session>.jsonl` holds the main conversation.
  - `<session>/subagents/` holds one file per subagent, and workflow agents sit one level deeper in `subagents/workflows/wf_<id>/`.
  - `<session>/tool-results/` holds large tool outputs in full. The JSONL itself keeps only a preview of them (observed).

  Codex gives each subagent a rollout file of its own, linked to its parent.
- **Both agents only append to their transcript files.** That holds for resume and compaction too, so uploading only the new bytes past an offset works. Three exceptions break that assumption:
  - Claude's opt-in `CLAUDE_CODE_TRANSCRIPT_LOCAL_GC` trims a file.
  - Codex compresses rollouts once they are 7 days cold, and decompresses them again on resume.
  - Claude can set a transcript aside as `.superseded-` or `.orphaned-` instead of overwriting it.
- **Joining to the OTLP events needs no new identifier:**
  - **Claude:** `session.id` in the OTLP events equals `sessionId` in the transcript and the file name. A dispatcher can fix it in advance with `--session-id`.
  - **Codex:** `conversation.id` equals the thread id, which is the id in the rollout file name and the `thread_id` that `codex exec --json` prints first.
- **Recommendation for a laptop:**
  - **OTLP logs**, configured through managed settings.
  - **An upload of the transcript's new bytes, driven by hooks:** after each turn (`Stop`), after each subagent (`SubagentStop`) and at `SessionEnd`.
  - **A sweep at `SessionStart`** for sessions that crashed or were abandoned before their last upload.
  - **No daemon.**
- **Recommendation for a Nomad task:**
  - **The task's entrypoint is a small wrapper** that works the same for both agents.
  - **It puts the agent's state directory inside the allocation.**
  - **It records the documented stream to a file.**
  - **It uploads the transcript directory's new bytes** every few seconds and once more at exit.
  - **On Nomad's kill signal,** it stops the agent gracefully, then makes the final upload.
- **The Agent SDK's `SessionStore` comes second.** It works only for Claude, and only when the agent is driven through the TypeScript or Python SDK. Its `append` contract is still a good shape for SolidTrace's upload API to copy.

## 1. What each option captures

### 1.1 Claude Code

| Option | What it captures | Format | Needs on the machine | Joins OTLP by |
|---|---|---|---|---|
| **The transcript directory** | All of the following:<br>• Every main-session line, including history from before a compaction.<br>• Request records (system prompt, tool definitions).<br>• Usage per response.<br>• A file per subagent.<br>• Large tool outputs in full, in `tool-results/`. | Internal; "changes between versions" ([sessions](https://code.claude.com/docs/en/sessions#where-transcripts-are-stored)) | Something to read and upload it: a hook command or a wrapper | `sessionId` and the file name, plus the finer keys in section 4 |
| **`stream-json` on stdout** | Main messages with usage, and a final `result` line with cost, `num_turns`, `modelUsage` and the outcome. Subagents send only tool blocks unless `--forward-subagent-text` is set ([headless](https://code.claude.com/docs/en/headless#follow-subagent-messages)). Compaction appears only as a `compact_boundary` line. | Documented SDK types ([TypeScript SDK](https://code.claude.com/docs/en/agent-sdk/typescript)) | Whatever launched `claude -p` | `session_id` on every message |
| **Agent SDK `SessionStore`** | The same entries as the transcript, mirrored batch by batch as they are written. Subagents arrive under the subpath `subagents/agent-<id>` ([session storage](https://code.claude.com/docs/en/agent-sdk/session-storage)). | The entries are the transcript's internal lines | Node or Python, the Agent SDK, and an adapter. Not available from the bare CLI. | `sessionId` in the key |
| **Hooks** | Pointers, not content: `transcript_path`, and `agent_transcript_path` on `SubagentStop`. Their payloads do carry full `tool_input` and `tool_response` ([hooks](https://code.claude.com/docs/en/hooks)). | Payloads documented; the files they point to are internal | Settings only; a command hook also needs its command present | `session_id` in the payload |

**What the files hold (observed, 2.1.322):**
- **Layout:**
  - 75 main transcripts and 537 subagent transcripts.
  - 299 of the subagent files sit under `subagents/workflows/wf_<id>/`, the directory named by `workflow.run_id` ([monitoring](https://code.claude.com/docs/en/monitoring-usage)).
  - Each subagent has a `.meta.json` with `agentType`, `toolUseId` and `spawnDepth`. Nested subagents sit flat beside the others, with `parentAgentId` in their `.meta.json`.
  - Every subagent line has the parent's `sessionId`, `isSidechain: true` and an `agentId`.
- **Large tool outputs:**
  - 100 outputs were spilled to `tool-results/*.txt`, in 31 sessions, at 30–61 KB each.
  - For each of them, the transcript's `tool_result` block held only about 2 KB, the preview the model saw. Its `toolUseResult` held about 30 KB.
  - So the full output exists only in the sidecar file.
- **Compaction:** 18 main transcripts had a `compact_boundary`, and all 18 still held the messages from before it.
- **Upgrades:** 5 transcripts carried lines from more than one Claude Code `version`.
- **Cost:** a `cost-state` line type carries `totalCostUSD` and `modelUsage`. It is internal and undocumented.

**Unverified:**
- Whether `stream-json` tool results carry the full output or the same 2 KB preview.
- Whether a `SessionStore` mirrors the `tool-results/` sidecars. The docs say `subpath` covers "a subagent transcript or sidecar file".

### 1.2 Codex

| Option | What it captures | Format | Needs on the machine | Joins OTLP by |
|---|---|---|---|---|
| **Rollout files** (`~/.codex/sessions/YYYY/MM/DD/rollout-<ts>-<thread_id>.jsonl`) | All of the following:<br>• Session metadata.<br>• Responses API items.<br>• `compacted` items with the replacement history.<br>• Token usage records.<br>• Inter-agent messages.<br>• One file per subagent thread.<br><br>Command output and MCP results are cut at 64 KiB when persisted ([policy.rs](https://github.com/openai/codex/blob/main/codex-rs/rollout/src/policy.rs)). | Internal | Something to read and upload it | The thread id, in the file name and in `session_meta.id` |
| **`codex exec --json`** | The primary thread only: agent messages, reasoning, commands with `aggregated_output`, file changes, MCP calls, and per-turn usage with no cost. Subagents appear only as `collab_tool_call` items naming the receiving thread ids ([exec_events.rs](https://github.com/openai/codex/blob/main/codex-rs/exec/src/exec_events.rs)). Events from other threads are filtered out (`should_process_notification` in [exec/lib.rs](https://github.com/openai/codex/blob/main/codex-rs/exec/src/lib.rs)). | Documented ([non-interactive mode](https://learn.chatgpt.com/docs/non-interactive-mode)) | Whatever launched it | `thread_id` on `thread.started` |
| **Hooks** | 14 events. Each payload has `session_id` and `transcript_path`, and `SubagentStop` adds `agent_transcript_path` ([hooks](https://learn.chatgpt.com/docs/hooks)). "Subagent hooks use the parent session id." | The docs say the "transcript format isn't a stable interface for hooks" | Settings, plus the script, which Codex doesn't distribute | `session_id`, which is the root thread's id |
| **Session store** | None. `experimental_thread_store` accepts only `local` and is marked "Experimental / do not use" ([config.schema.json](https://github.com/openai/codex/blob/main/codex-rs/core/config.schema.json)). | n/a | n/a | n/a |

**Two corrections to the earlier note:**
- **Codex does emit cost.** A `codex.turn_cost` OTel event carries `usage.estimated_usd`, and a matching counter is recorded in micro-dollars ([session_telemetry.rs](https://github.com/openai/codex/blob/main/codex-rs/otel/src/events/session_telemetry.rs)).
- **Codex does have a trace exporter key.** `[otel] trace_exporter` exists ([otel_init.rs](https://github.com/openai/codex/blob/main/codex-rs/core/src/otel_init.rs)).

**Unverified:** whether `aggregated_output` is ever truncated.

## 2. Session lifecycle: what lands on disk

| Event | Claude Code | Codex |
|---|---|---|
| **Resume** | Same session id, same file, appended. Monitoring: the session "keeps its `session.id`". Resuming in two terminals interleaves both into one transcript. `SessionStart` fires with `source: "resume"`. ([sessions](https://code.claude.com/docs/en/sessions), [monitoring](https://code.claude.com/docs/en/monitoring-usage), [hooks](https://code.claude.com/docs/en/hooks)) | Same thread id, same file, appended, in the date directory where the file was first created. A `.zst` file is decompressed back to `.jsonl` first ([recorder.rs](https://github.com/openai/codex/blob/main/codex-rs/rollout/src/recorder.rs) `open_log_file`). |
| **Fork** (`--fork-session`, `/branch`; `codex fork`) | A new session id and a new file, holding a copy of the history up to the fork point. `SessionStart` fires with `source: "fork"`. The SDK's `forkSession` rewrites `sessionId` and remaps message UUIDs ([session storage](https://code.claude.com/docs/en/agent-sdk/session-storage)). For the CLI's fork it is unverified, and no field naming the parent was observed. | A new thread id. `session_meta.forked_from_id` names the parent. In `Legacy` history mode the parent's history is copied in. In `Paginated` mode the file "starts with its own first turn" and points at the parent's prefix through `history_base` ([protocol.rs](https://github.com/openai/codex/blob/main/codex-rs/protocol/src/protocol.rs), [recorder.rs](https://github.com/openai/codex/blob/main/codex-rs/rollout/src/recorder.rs)). |
| **Compaction** | A `compact_boundary` line and a summary line (`isCompactSummary`) are appended, and earlier lines stay (observed). The SDK's `getSessionMessages` returns only the chain after compaction; `store.load` returns everything ([session storage](https://code.claude.com/docs/en/agent-sdk/session-storage)). An opt-in, `-p`/SDK-only `CLAUDE_CODE_TRANSCRIPT_LOCAL_GC=1` deletes the history before a compaction once the file passes 5 MB ([env vars](https://code.claude.com/docs/en/env-vars)). Idle compaction can also compact a conversation that was left open. | A `compacted` item holding `replacement_history` is appended to a file that is only ever appended to ([history/lib.rs](https://github.com/openai/codex/blob/main/codex-rs/history/src/lib.rs)). `SubAgentSource::Compact` exists; whether compaction writes a rollout file of its own is unverified. |
| **`/clear`** | A new session id and a new file. `SessionEnd` fires with `reason: "clear"` for the old session. OTel's `session.id` changes. | n/a |
| **Subagents** | A file per subagent at `<session>/subagents/agent-<id>.jsonl`. These files are unaffected by compaction of the main conversation and are deleted along with it after `cleanupPeriodDays` ([sub-agents](https://code.claude.com/docs/en/sub-agents)). A resumed subagent "starts a new run of the agent under the same ID", in the same file. Background subagents can keep running after their turn ends. | A rollout file of its own per thread. `session_meta.source` is `SubAgent(ThreadSpawn{parent_thread_id, depth, agent_role})`, and `session_meta.session_id` is the root thread's id. Spawn edges are also kept in Codex's local state database ([spawn.rs](https://github.com/openai/codex/blob/main/codex-rs/core/src/agent/control/spawn.rs)). |
| **Abandoned** (terminal left open, laptop asleep) | No `SessionEnd`. The file holds everything up to the last write. | `SessionEnd` fires after 30 minutes idle with no client connected ([hooks](https://learn.chatgpt.com/docs/hooks)). |
| **Local deletion** | After 30 days (`cleanupPeriodDays`, which managed settings can set). A transcript can be set aside as `.orphaned-…` or `.superseded-…` ([claude-directory](https://code.claude.com/docs/en/claude-directory)). | Rollouts are compressed to `.jsonl.zst` after 7 days cold (`MIN_ROLLOUT_AGE` in [compression.rs](https://github.com/openai/codex/blob/main/codex-rs/rollout/src/compression.rs)). Archived rollouts move to `archived_sessions`. `thread/revert` writes a new file, `…-<thread_id>_<rollout_id>.jsonl`. |

**What this means for an uploader:**
- **Key each upload** by (agent, session or thread id, path relative to the session directory), and send the bytes past a byte offset.
- **If a file shrinks, re-read it whole.** That covers Claude's trimming and Codex's compression and decompression.
- **Dedupe on the server by line `uuid`** (Claude) or ordinal (Codex). Forks copy history, and a retried upload can resend lines.

**Unverified:** whether Codex rollout lines carry an explicit ordinal field.

## 3. What survives a killed task

**Nomad's stop sequence:** it sends `kill_signal`, waits `kill_timeout`, then sends SIGKILL ([task](https://developer.hashicorp.com/nomad/docs/job-specification/task)):
- `kill_signal` defaults to SIGINT, or SIGTERM under `docker`.
- `kill_timeout` defaults to 5 s, capped by the client's `max_kill_timeout`, which defaults to 30 s.

| Signal | Claude `claude -p` | Codex `codex exec` |
|---|---|---|
| **SIGINT** | Ends the turn in progress ([headless](https://code.claude.com/docs/en/headless#stop-a-run-with-sigterm)). Whether the process then exits with a `result` line is unverified. | `exec` listens for Ctrl-C and sends `turn/interrupt` ([exec/lib.rs](https://github.com/openai/codex/blob/main/codex-rs/exec/src/lib.rs)). The turn ends as interrupted. Exiting cleanly afterwards is likely but unverified. |
| **SIGTERM** | Exit code 143, the turn left unfinished and no `result` line. Running Bash commands are killed and recorded as killed. `SessionEnd` hooks run. OTel has `CLAUDE_CODE_OTEL_SHUTDOWN_TIMEOUT_MS` (2 s) to export ([env vars](https://code.claude.com/docs/en/env-vars)); whether the SIGTERM path waits for it is unverified. | No SIGTERM handler was found in `exec` or `core`, so the default action kills the process. Rollout items survive, because the writer writes each batch as it arrives once the file exists ([recorder.rs](https://github.com/openai/codex/blob/main/codex-rs/rollout/src/recorder.rs) `rollout_writer`). OTel exporters "flush on shutdown", so the last batch is lost. |
| **SIGKILL** | No `SessionEnd` and no `result` line. The transcript is "written asynchronously and may lag" ([hooks](https://code.claude.com/docs/en/hooks)), so its last lines may be missing. Up to 5 s of OTel logs is lost. A `SessionStore` keeps the batches it was already sent. | Rollout lines already written survive. Unflushed OTel is lost. |

**What follows for the wrapper:**
- **It should be the task's own process,** so it receives `kill_signal`.
- **It should end the agent gracefully,** before Nomad's SIGKILL:
  - For Codex, it sends SIGINT.
  - For Claude, it sends SIGINT, then SIGTERM if the process hasn't exited.
- **It should raise `kill_timeout` to about 30 s,** leaving time for the final upload.
- **It should enforce the run's own deadline** a little before Nomad's, so a timeout is usually a clean stop rather than a kill.
- **It should write the outcome line itself** (timed out, killed, exit code), because a killed stream has no `result` line.
- **Cost after a kill** comes from per-response usage in the transcript, or from the OTel `api_request` and `codex.turn_cost` events.

## 4. Joining a transcript to its OTLP events

**Claude Code:**

| OTLP attribute | Transcript | Source |
|---|---|---|
| `session.id` | `sessionId` on every line, and the file name `<session-id>.jsonl`. It is also `session_id` in hooks and in `stream-json`. | [monitoring](https://code.claude.com/docs/en/monitoring-usage), [sessions](https://code.claude.com/docs/en/sessions) |
| `prompt.id` | `promptId` on user lines | Observed |
| `message.uuid` on `assistant_response` | The line's `uuid`: "UUID of the message as persisted in the session transcript" | [monitoring](https://code.claude.com/docs/en/monitoring-usage) |
| `request_id` on `api_request` | `requestId` on assistant lines | Observed |
| `tool_use_id` on `tool_result` | The `id` of the `tool_use` block | Observed |
| `agent_id`, `parent_agent_id` | `subagents/agent-<agent_id>.jsonl`, and `parentAgentId` in its `.meta.json`. Also `agent_id` on `stream-json` messages. | [monitoring](https://code.claude.com/docs/en/monitoring-usage), [sub-agents](https://code.claude.com/docs/en/sub-agents), [TypeScript SDK](https://code.claude.com/docs/en/agent-sdk/typescript) |
| `workflow.run_id` (`wf_…`) | `subagents/workflows/wf_<id>/` | Observed |

- **The dispatcher can fix the id before launch.** `--session-id <uuid>` sets the session id ([cli-reference](https://code.claude.com/docs/en/cli-reference)), so the id is known before the first byte arrives.
- **Run tags:** `OTEL_RESOURCE_ATTRIBUTES` adds the run's own tags to every event, but can't override `session.id`.
- **Trace ids:** in `-p` runs with `TRACEPARENT` set, every OTel log record carries `trace_id` and `span_id`, even with no trace exporter configured ([monitoring](https://code.claude.com/docs/en/monitoring-usage)).
- **Caution:** Anthropic warns that joins on transcript fields are "version-specific rather than a stable contract".

**Codex:**
- **The thread id joins everything.**
  - Every OTel log and trace event carries `conversation.id`, which is the thread id ([shared.rs](https://github.com/openai/codex/blob/main/codex-rs/otel/src/events/shared.rs)).
  - The same id is in the rollout file name, in `session_meta.id`, and in `thread.started.thread_id` from `exec --json`.
- **Subagents carry two ids.** Each subagent thread has its own thread id, and `session_meta.session_id` names the root thread. So a whole tree can be grouped by the root id.
- **The thread id can't be fixed in advance.** No flag found to set it, so the wrapper reads it from the first `thread.started` line.
- **`TRACEPARENT` is honoured** for the `exec` span ([trace_context.rs](https://github.com/openai/codex/blob/main/codex-rs/otel/src/trace_context.rs)).
- **Resource attributes:**
  - Codex adds `service.version`, `env` and `host.name` itself ([provider.rs](https://github.com/openai/codex/blob/main/codex-rs/otel/src/provider.rs)).
  - It builds its resource with `Resource::builder()`. In opentelemetry-rust that builder normally includes the environment detector, so `OTEL_RESOURCE_ATTRIBUTES` probably applies; unverified at runtime.

## 5. Recommendation

### 5.1 Laptop

**Configuration only, with no daemon:**

1. **OTLP logs** through managed settings, as the earlier note says.
   - Claude: the `env` block of managed settings.
   - Codex: `[otel]` in managed config.
   - This gives live metadata, cost and errors, even for a session that is never uploaded.
2. **A transcript upload driven by hooks,** in managed settings:
   - **`Stop`:** uploads the main transcript's new bytes, run with `async: true` on Claude so it doesn't add latency.
   - **`SubagentStop`:** uploads `agent_transcript_path`. On Claude it also uploads `.meta.json` and the `tool-results/` files.
   - **`SessionEnd`:** a final upload.
     - On Claude the budget can be raised to 60 s.
     - Codex caps `SessionEnd` at 3 s and runs it synchronously, so its hook must start a detached upload and return at once.
3. **A sweep at `SessionStart`,** run in the background. It finds local transcripts that have bytes SolidTrace hasn't seen and uploads them. This catches crashed, killed and abandoned sessions, Claude sessions that ended without `SessionEnd`, and anything the 30-day sweep or 7-day compression would otherwise take first.

**Distribution:**
- **Claude Code:** a SolidTrace plugin force-enabled through managed `enabledPlugins` can carry both the hooks and the uploader script. Hooks from such plugins still run under `allowManagedHooksOnly` ([hooks](https://code.claude.com/docs/en/hooks)).
- **Codex:** managed hooks go in `requirements.toml`, but "Codex doesn't distribute the scripts" ([hooks](https://learn.chatgpt.com/docs/hooks)). The script needs the company's device management, or a one-line command such as a `curl` of the file.

**Rejected for laptops:**
- **Tailing continuously:** it needs a daemon on every laptop.
- **Uploading only at `SessionEnd`:** it loses abandoned and crashed sessions.
- **`stream-json`:** it doesn't exist for interactive sessions.

### 5.2 Nomad task

**One entrypoint wrapper for both agents:**

1. **Fix where the agent writes, inside the allocation:**
   - Claude: `CLAUDE_CONFIG_DIR=$NOMAD_ALLOC_DIR/claude` and `CLAUDE_CODE_PROJECT_DIR_NAME=run` ([sessions](https://code.claude.com/docs/en/sessions#name-the-project-directory-yourself)).
   - Codex: `CODEX_HOME` set the same way.
   - `alloc/` is shared by all tasks in the group ([filesystem](https://developer.hashicorp.com/nomad/docs/concepts/filesystem)).
2. **Fix the identities:**
   - Claude: `--session-id <uuid>` chosen by the dispatcher.
   - Both agents: `OTEL_RESOURCE_ATTRIBUTES` with the run id, and `TRACEPARENT` from the dispatcher.
3. **Run the agent with its documented stream, and save that stream to a file:**
   - Claude: `claude -p --output-format stream-json --verbose --forward-subagent-text`.
   - Codex: `codex exec --json`.
   - The stream gives the outcome and cost in a documented format, and a fallback if the transcript parser breaks on a new version.
4. **Upload the new bytes of the transcript directory** every few seconds and at exit, using the same API as laptops. Then a SIGKILL loses at most the last few seconds.
5. **On `kill_signal`:** stop the agent as section 3 describes, make the final upload, and write the run's outcome.

**Agent SDK `SessionStore`: not first, but kept open.**
- **What it gives:** a live mirror with retries, and resume on another Nomad client, which helps retries and follow-ups.
- **What it costs:**
  - It covers Claude only.
  - The entrypoint must be an SDK program.
  - It throws with `persistSession: false` or file checkpointing.
  - Its writes are best-effort, signalled by `mirror_error`.
- **What to do now:** make SolidTrace's upload API take the same shape as `append(key, entries)`: a key of project, session and subpath; ordered entries; dedupe by `uuid`. Then a TypeScript adapter pointing at SolidTrace is a small later step. It needs a matching `load` for resume.

**Rejected for Nomad:**
- **`stream-json` alone:** no full tool outputs (unverified), no request records, and no `result` line after a kill.
- **`exec --json` alone:** no subagent threads at all.
- **Uploading only at exit:** a SIGKILL loses everything that wasn't flushed.

## 6. New questions this raises

- **The upload API** belongs to [ticket 06](../issues/06-transcript-storage.md). It needs append-by-offset per (session, relative path), with dedupe by line `uuid` or ordinal, and a way for the client to ask what the server already has.
- **For [ticket 08](../issues/08-run-and-session.md):**
  - Is a fork its own Session, linked to its parent? Codex names the parent (`forked_from_id`); Claude doesn't, as far as observed.
  - Is a `/clear` a new Session?
  - Is a Codex subagent thread a Session, or part of its root's?
- **For [ticket 13](../issues/13-transcript-redaction-and-retention.md):**
  - `tool-results/` files and request records hold the most sensitive content, so they need redaction too.
  - A laptop upload sends whatever the engineer's agent read.
