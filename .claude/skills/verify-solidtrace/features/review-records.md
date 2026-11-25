# Review records

The `open-pr` and `update-pr` skills end by sending the PR's Review record to `solidtrace review final` in a heredoc, in one Bash call. Before that command, they ended their final reply with the record in a `solidtrace-pr-record` block. When the Agent session uploads, SolidTrace keeps the record from the call, or from the block of an older session. Each member reads the Review records page (one row for each PR, its newest round, with the counts) and a record's page (the story, the review runs, each finding with its decisions in order, the confirmations and the other rounds). The MCP tools `list_review_records` and `get_review_record` give the same rows, and the record with its findings in pages.

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

- **Capture to pages and MCP.** `"$V" flow -- e2e/flows/agent-sessions.spec.ts -g "Review record"`. Pass: the admin signs in the CLI with the device flow, two open-pr Agent sessions (round 1 with the record in its final reply, as an older session gives it, and a round 2 over 64 KiB that sends it to `solidtrace review final`) upload through `solidtrace capture`, the list shows round 2 with `(1 earlier)` and its counts, the record's page shows the decisions of each finding and its last finding, the link to round 1 opens it, and `get_review_record` gives the round 2 record with its first 30 findings in less than 100,000 characters, and its next pages give all 240 findings.
- **Off.** `"$V" snap --path /solid-trace-org/review_records --name review-records-off --as member` before any flow turns on Agent sessions. Pass: status 200, and the ARIA snapshot has `Agent sessions are off`.
- **Side effect.** `curl -s 'http://127.0.0.1:4201/?user=solidtrace' --data-binary 'SELECT pr, round, length(record) FROM solidtrace_verify_s1.review_records FINAL'`. Pass: one row for each uploaded record, with the length of the whole record.
- **Proof.** The traces in `<run>/evidence/flow-<time>/playwright/`, and the screenshots `review-records-list` and `review-record-page`.

## Gotchas

- The list is newest round first for each PR, across all the flow's earlier runs in the same slot. The flow uses a PR with its own run id, so old rows do not change its row.
- A record shows only after the parse, about one second after the upload. Wait with `toPass`, as the flow does.
- The skill's example record comes in as a user line of the transcript. It must not show as a row. A test that sees the example PR on the list has found a fault.
- An MCP client can cut a large tool answer. Check the pages with a direct POST to `/mcp`, as the flow does, not through a client.
