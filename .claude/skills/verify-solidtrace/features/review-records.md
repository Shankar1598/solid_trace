# Review records

The `open-pr` and `update-pr` skills end with `solidtrace review final "$dir/record.json"`. The command checks the PR's Review record and writes it to `<session id>/solidtrace/review-records/round-<n>-<h>.json` in the Claude project folder of the Agent session. The capture uploads that file with the Agent session, at once at a Stop hook. Before that command, the skills ended their final reply with the record in a `solidtrace-pr-record` block. SolidTrace keeps the record from the file, or from the block of an older session. Each member reads the Review records page (one row for each PR, its newest round, with the counts) and a record's page (the story, the review runs, each finding with its decisions in order, the confirmations and the other rounds). The MCP tools `list_review_records` and `get_review_record` give the same rows, and the record with its findings in pages.

## Sub-features

- `records-list`: `/<org>/review_records` has the table `Review records` with `tr[data-review-record="<key>"]` rows and the cells `[data-cell="round"]`, `findings`, `fixed`, `missed` and `not-given`.
- `records-page`: `/<org>/review_records/record?key=<pr>` (the newest round) or `?id=<record id>` (one round) has `[data-section="review-record-header"]`, the regions `Story` and `Rounds of this PR`, and the tables `Reviews` (`tr[data-run]`), `Findings` (`tr[data-finding]`, with `[data-cell="decisions"]`, `found-by` and `outcome`) and `Confirmations` (`tr[data-confirmation]`).
- `records-text`: all record text shows as text. An HTML tag in a finding's title shows as its characters.
- `records-whole`: a record over 64 KiB shows all its findings. `get_review_record` gives the record with its first 30 findings, and `findings_from` reads the next pages until `next_findings_from` is `null`.
- `records-off`: with Agent sessions off, the list has `[data-notice="agent-sessions-off"]`.

## How to get to it (user POV)

- Sidebar group **Coding agents**, link **Review records**, under Agent sessions, which opens `/<org>/review_records`.
- The palette's page **Review records**.
- MCP: `list_review_records` and `get_review_record`, each answer ending with its Console link.
- The data comes from an Agent session's upload: `solidtrace capture` in a captured repository, or a Run.

## Driving it with verify

Preconditions:

- The baseline stack is up, and `"$V" doctor` is all `ok`.
- Go can build `cli/`. The flow builds it into `console/tmp/`.

- **Capture to pages and MCP.** `"$V" flow -- e2e/flows/agent-sessions.spec.ts -g "record file"`. Pass: the admin signs in the CLI with the device flow, and two open-pr Agent sessions upload through `solidtrace capture`. Round 1 has the record in its final reply, as an older session gives it. Round 2 is past 128 KiB: the flow runs the real `solidtrace review final` on it two times (230 findings, then 240) with a Stop capture after each, and a record of a repository that is not captured stays on the machine. The list shows round 2 with `(1 earlier)` and the counts of the second file, no row of the other repository, the record's page shows the decisions of each finding and its last finding, the link to round 1 opens it, and `get_review_record` gives the round 2 record with its first 30 findings in less than 100,000 characters, and its next pages give all 240 findings.
- **Off.** `"$V" snap --path /solid-trace-org/review_records --name review-records-off --as member` before any flow turns on Agent sessions. Pass: status 200, and the ARIA snapshot has `Agent sessions are off`.
- **Side effect.** `curl -s 'http://127.0.0.1:4201/?user=solidtrace' --data-binary 'SELECT pr, round, path, version, offset, length(record) FROM solidtrace_verify_s1.review_records FINAL'`. Pass: one row for each uploaded record, with the length of the whole record. A file record has the file's path, a version for each write, and offset 0.
- **By hand, with a real CLI.** Sign a CLI in to the stack (`solidtrace login --device --ingest-url http://localhost:<ingest port> http://localhost:<console port>`), in a captured repository. In the Agent session, run `solidtrace review final <record file> --session <id>`, then `solidtrace capture` with a Stop hook input (`{"session_id":…,"transcript_path":…,"cwd":…,"hook_event_name":"Stop"}` on stdin). Pass: the record shows on the Review records page, and the side effect query gives its length.
- **Proof.** The traces in `<run>/evidence/flow-<time>/playwright/`, and the screenshots `review-records-list` and `review-record-page`.

## Gotchas

- The list is newest round first for each PR, across all the flow's earlier runs in the same slot. The flow uses a PR with its own run id, so old rows do not change its row.
- A record shows only after the parse, about one second after the upload. Wait with `toPass`, as the flow does.
- The skill's example record comes in as a user line of the transcript. It must not show as a row. A test that sees the example PR on the list has found a fault.
- `review final` finds the Agent session by its transcript, `<projects>/*/<id>.jsonl` in `CLAUDE_CONFIG_DIR` (else `~/.claude`). Write the transcript before you run it.
- An MCP client can cut a large tool answer. Check the pages with a direct POST to `/mcp`, as the flow does, not through a client.
