# Review records come from the final reply of an Agent session

Amends [ADR 0020](0020-agent-transcripts-raw-chunks-in-clickhouse-and-logs-for-reading.md): the parse of the Agent file batches makes a third derived layer, `review_records`, next to the Logs and `agent_sessions`.

Changed later, two times. First the skills sent the record to `solidtrace review final` in a heredoc, not in the final reply. Now `solidtrace review final` writes the record to a file in the folder of the Agent session, and the capture uploads it with the Agent session's other files. See [The record moves to a file of the Agent session](#the-record-moves-to-a-file-of-the-agent-session). The parse still reads the block of an older Agent session.

The `open-pr` and `update-pr` skills ended their final reply with the PR's **Review record**, as JSON in a `solidtrace-pr-record` block. The reply uploads with its **Agent session**, as each transcript line does. SolidTrace reads the record from there:

- **The parse keeps it.** When the parse reads an assistant line of a main transcript, it reads the last `solidtrace-pr-record` block of the line's text. It keeps the record only when it is valid JSON of version 2. A user line, a subagent's file, a sidechain line and a tool result give no record, because the skill's own text has an example record, and a person or a tool can show a record too. The record is stored as the agent wrote it, byte for byte, with its PR (`""` when no PR opened), its round and its counts: findings, fixed, missed (found by a person, an agent or a test run) and confirmations not given.
- **One insert with the Logs.** The parse inserts the records in the same `LogInsert` as the Logs and the totals, with the same dedup token. So a parse that stops and runs again stores each record once, as it stores each Log once.
- **Its own table, not only the Log.** A Log cuts a string at 64 KiB. A real record can be 100 KB or more, so the Log of its line does not keep all of it. `review_records` keeps the whole record, compressed.
- **A table of AgentDays.** `review_records` is partitioned by the Project and the day when the server stored the line, as `agent_sessions` is. It follows the **Agent Project**'s **Retention**, the deletion of the Project and the disk limit with the raw layer.
- **The newest record of a PR.** A PR's key is its `pr`, else the Agent session id. The newest record of a key has the highest round, then the latest store time. A later round replaces the earlier rounds of the same PR on the list, and the record's page links to the earlier rounds.
- **The same readers as the Agent sessions.** Each member who can read the Agent sessions can read the Review records, on the Review records page and with the MCP tools `list_review_records` and `get_review_record`. The record is the agent's text, with the words of PR commenters in it, so the pages show it as text only, never as HTML or Markdown, and link a PR only when it is a web address.

We chose this because the record already uploads with its Agent session. The parse reads only what the upload brings: no new ingest route. (The first form had no new CLI command and no change to the skills. See the next section for the change that came later.)

## The record moves to a call of `solidtrace review final`

Replaced: see the next section. No release had this form, and its parse went.

The block put the whole record on the person's screen, as one line of JSON that can be 100 KB or more. Now the skills send the record to `solidtrace review final` in a heredoc, in one Bash call, and the final reply gives only the PR and a short summary. Claude Code shows a Bash call folded, and the transcript keeps the heredoc byte for byte, so the record uploads with its Agent session as before. This follows `solidtrace review block`, which sends the findings blocks in the same way.

- **The command checks, and sends nothing.** `review final` decodes the record as version 2 and prints one line with its round and counts. It exits 1 when the JSON is not a record, so the agent fixes it and calls again. The capture uploads the call with the transcript.
- **The parse reads the call.** For an assistant line of a main transcript, the parse also reads each Bash tool use whose command is only `solidtrace review final` (or a path of letters, digits, `_`, `.`, `~`, `-` and `/` that ends in `/solidtrace`) with a heredoc whose word is in quotes (`<<'EOF'` or `<<"EOF"`), and nothing more: blank lines and comment lines can come before it and blank lines after it. A heredoc with no quotes, a pipe, `&&`, `;`, a redirect, `$(…)` or a second command gives no record, because then the heredoc is not surely what the command read: the shell changes the body of `<<EOF` (`$…`, `` `…` `` and `\`) before the command reads it. The heredoc goes through the same check as a block. Over the line's blocks and calls, in their order, the last one counts.
- **No check of the tool result.** The parse checks the JSON itself. When the command refused the record, the agent's next call comes in a later line. Each read of `review_records` (the list, the record page, its rounds and the MCP tools) keeps only the last line of an Agent session for the same PR and round, so the refused call and its retry are one round. Two Agent sessions with the same PR and round stay two records.
- **The block stays.** Agent sessions from before this change keep their records, because the parse still reads the block.
- **A record over 128 KiB cannot run on Linux.** Claude Code gives the whole command to `bash -c` as one argument, and Linux refuses one argument of 128 KiB or more ("Argument list too long"). The tool use is in the transcript before the shell starts, and the parse does not read the tool result, so SolidTrace keeps the record. The skill tells the agent not to send the record again then. `review final` does not check such a record, but `solidtrace review record` made it and `solidtrace review check` checked it before.

## The record moves to a file of the Agent session

The heredoc put the whole record in the input of one tool call. The agent must type all of it, and a record of about 100 KB or more failed: the records of a PR of 123 KB and 140 KB did not get into SolidTrace. A permission prompt also showed the whole JSON. Now the agent never types the record:

- **The CLI writes the file.** `solidtrace review record` already writes the record to `$dir/record.json`. `solidtrace review final <file>` checks it as version 2, and writes it with a write and a rename to `<session id>/solidtrace/review-records/round-<n>-<h>.json` in the Claude project folder of the Agent session. `<h>` is the first 12 hex characters of the SHA-256 of the record's `pr`. The session is `--session`, else `CLAUDE_CODE_SESSION_ID`.
- **The capture uploads it as a file of the Agent session.** The capture already uploads each regular file under `<session id>/` whole, with its hash and version. So the record has the same rules as the transcript: a **Captured repository** or a **Run**, the retries, **Retention** and the disk limit. A file under `<session id>/solidtrace/` goes at once, not after it settles, because the rename makes it whole.
- **The parse reads the file.** A whole unit at `<session id>/solidtrace/review-records/<name>.json` of its own Agent session, with the same check as a block, gives one Review record. A subagent's file, a file of another Agent session's folder and a cut file give none. The row has the file's path and version, and offset 0. Its id comes from that place, as a Log's id comes from the place of its line, so a parse again replaces the row.
- **A round written again is a new version of its file.** Of the records of one path, only the highest version counts, so the new file replaces the earlier record on the list.
- **The heredoc parse went.** No release had it, and no EventStore of the first team parsed it. The block parse stays.

We did not take a folder of the CLI with a new scan in the capture: it needs a second way to find the session of each file and a second record of what was sent. We did not take a direct upload from `review final` to a new route: it must copy the rule of the captured repositories, and it has no retry.

## Considered options

- **An ingest route for the record, sent by the skill.** The record would not depend on the parse of a transcript, but each skill and each CLI would need a change, and a record could arrive without its Agent session.
- **A Query on the Logs.** No new table, but a Log cuts the record at 64 KiB, and a Query must find the block in each message.

## Consequences

- No backfill. A record whose line SolidTrace parsed before this change has no Review record, because the parse does not read a line again. Only the Agent sessions that upload after the change give records.
- A record exists only when its Agent session uploads: in a **Captured repository**, or in a **Run**.
- A record that an agent writes wrongly (not valid JSON, or not version 2) gives no row, and nothing tells the person. The skills check the record with `solidtrace review check`, and `solidtrace review final` refuses a record that is not version 2.
- An MCP client can cut a large answer. Claude Code cuts a tool's answer past 25,000 tokens unless `MAX_MCP_OUTPUT_TOKENS` is higher, and a record of 145 KB is over that limit. Most of a record is its findings, so `get_review_record` gives them in pages: 30 findings when the agent gives no `findings_limit`. The first page of a record of 145 KB is about 36,000 characters, so the limit does not cut it. To get the next page, the agent gives `findings_from`. The answer is no longer the record byte for byte: Console parses the record and makes its JSON again with one page of findings. The Review record page still shows all the findings.

Source: [.scratch/review-records/plan.md](../../.scratch/review-records/plan.md)
