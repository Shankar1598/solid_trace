# 07: How can a transcript be captured, on a laptop and in a Nomad task?

Type: research
Status: resolved
Map: [SolidTrace Software Factory](../map.md)

## Question

[research/agent-transcripts.md](../research/agent-transcripts.md) lists Claude Code's and Codex's capture surfaces (OTel, hooks, the on-disk JSONL, `stream-json`) and recommends OTLP logs plus a raw upload at session end. It doesn't settle how the full transcript gets from the machine to SolidTrace in each of two settings:

- **A laptop session:** an engineer's interactive Claude Code or Codex, configured only through managed settings or a config file. Session-end upload through a hook, tailing the JSONL, or something else? What happens when a session is abandoned, resumed, compacted or forked, and to subagent transcripts?
- **A Nomad task:** a dispatched `claude -p` or `codex exec`. Is `stream-json` on stdout enough, or the Agent SDK's session store, or the JSONL uploaded at exit? What survives the task being killed by a timeout?

For each option: what it captures (subagents, tool input and output in full, cost and usage), whether its format is documented or internal, what it needs installed on the machine, and how it relates the transcript to the OTLP events of the same session (a shared session id?). Cover Claude Code and Codex.

The answer feeds [Where are transcripts stored?](06-transcript-storage.md) and [What are a Run and a Session?](08-run-and-session.md).

## Answer

Resolved 2026-11-13 (research). Details and sources: [research/transcript-capture.md](../research/transcript-capture.md).

- **The on-disk transcript is the only complete record.**
  - **Claude:** `<session>.jsonl`, plus `<session>/subagents/` (including `workflows/wf_<id>/`) and `<session>/tool-results/`. Large tool outputs are complete only in `tool-results/`; the JSONL keeps a preview.
  - **Codex:** one rollout file per thread, subagents included. Command and MCP output is cut at 64 KiB.
  - **The documented streams lose things:** `stream-json` drops subagent text unless `--forward-subagent-text` is set, and `codex exec --json` shows the primary thread only.
- **Both agents only append** across resume and compaction, so uploads can send just the bytes past an offset. Three exceptions need handling: Claude's opt-in `CLAUDE_CODE_TRANSCRIPT_LOCAL_GC` trims a file, Codex compresses rollouts once they are 7 days cold, and a fork copies history into a new id.
- **The join needs no new id:**
  - **Claude:** OTLP `session.id` = transcript `sessionId` = the file name. The dispatcher can fix it in advance with `--session-id`.
  - **Codex:** OTLP `conversation.id` = the thread id = the rollout file name.
- **Laptop:** OTLP logs through managed settings. Hook-driven uploads of new bytes on `Stop`, `SubagentStop` and `SessionEnd`, plus a sweep at `SessionStart` for crashed or abandoned sessions. No daemon. For Claude, a plugin force-enabled in managed settings can ship the uploader. Codex leaves script distribution to the company's own tooling.
- **Nomad:** an entrypoint wrapper that works for both agents:
  - It puts the agent's state directory in `alloc/` and records the documented stream to a file.
  - It uploads the transcript directory's new bytes every few seconds and at exit.
  - On the kill signal, it stops the agent gracefully: SIGINT for Codex, which has no SIGTERM handler; SIGINT then SIGTERM for Claude. Then it makes the final upload within a raised `kill_timeout`.
  - The Agent SDK's `SessionStore` (Claude only, SDK only) waits, but SolidTrace's upload API should take its `append` shape.
