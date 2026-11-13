# 06: Where are transcripts stored: Logs in an agent Project, or new tables?

Type: prototype
Status: open
Blocked by: 07
Map: [SolidTrace Software Factory](../map.md)

## Question

Two candidates:

- **Logs in an agent Project.** Each transcript line is a Log, with oversized values in the bucket by reference. Retention, tiering, grants, the Query language, Dashboards and Monitors come for free. ([research/fable-opinion.md](../research/fable-opinion.md), section 3.)
- **New tables:** `agent_sessions`, `agent_messages` and `agent_scores`, parsed and redacted on the server ([research/agent-transcripts.md](../research/agent-transcripts.md)).

Prototype both with real transcripts: this repo's own, in `~/.claude/projects/*solid-trace*` (608 files, 1.7 GB; 2.6% of lines over 64 KiB, the largest 1,125 KB). Measure what each loses, what the sessions page and a Scorer can query, and the storage per session. Log ingest cuts strings at 64 KiB and drops a Log over 1 MiB (`event_store/ingest/normalise/normalise.go`), so the Logs path needs the by-reference route.

Input from [How can a transcript be captured?](07-transcript-capture.md) (2026-11-13, [research/transcript-capture.md](../research/transcript-capture.md)): the unit uploaded is a session's directory, not one file. Claude's large tool outputs are complete only in `tool-results/` (the `.jsonl` keeps a preview of about 2 KB). The upload API needs append-by-offset per (session, relative path), dedupe by `uuid` or ordinal, and a way for the client to ask what the server already has; the research suggests copying the Agent SDK `SessionStore`'s `append` shape. Whichever storage wins must take that.

Use the capture format chosen by [How can a transcript be captured, on a laptop and in a Nomad task?](07-transcript-capture.md). Run against a worktree flow stack only (`run-flow.sh`), never the main dev ClickHouse.
