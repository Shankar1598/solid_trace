# Evidence files

An agent that runs `open-pr` or `update-pr` uploads the screenshots and videos of its checks with `solidtrace evidence add`, and the PR body links to each one. A member of the Organization clicks a link, signs in when it is necessary, and sees the image or plays the video in Console. A person who is not a member gets 404. EventStore keeps the bytes, in the `object_store` bucket under `evidence/`, or under its `evidence_path` folder when there is no bucket. After the Agent Project's Evidence Retention (90 days by default), EventStore deletes the bytes, and the link shows when the file was deleted.

## Sub-features

- `evidence-upload`: `POST <ingest>/api/agent/v1/agent-sessions/<session id>/evidence?caption=<text>` with a `sessions:write` token. The body is the file. 201 with `{id, url, content_type, byte_size}` for a new file, 200 and the same `id` for the same file in the same Agent session, 415 for a type that is not PNG, JPEG, WebP, GIF, MP4 or WebM (found from the bytes), 413 over 32 MiB, 403 without `sessions:write`.
- `evidence-cli`: `solidtrace evidence add <file>... [--caption <text>] [--session <id>]` prints one line for each file: the link, the type and the size. The Agent session is `--session`, else `CLAUDE_CODE_SESSION_ID`. A failed file prints `<file>: <message>` on stderr, and the command exits 1 at the end. An old server gives "this SolidTrace server is too old for Evidence files: ask the operator to upgrade it".
- `evidence-page`: `/<org>/evidence/<id>` shows `[data-evidence="image"]` or `[data-evidence="video"]`, and `[data-section="evidence-file-facts"]` (caption, uploader, date, the Agent session link and `[data-review-record-link]`). The video has the speed buttons 0.25x, 0.5x, 1x and 2x, and the page keeps the last speed for the viewer.
- `evidence-bytes`: `/<org>/evidence/<id>/file` streams the bytes with `Range` (206), with the stored `Content-Type`, `X-Content-Type-Options: nosniff`, `Content-Security-Policy: sandbox`, `Content-Disposition: inline`, `Cache-Control: private` and an `ETag` (304 on `If-None-Match`). A deleted file gives 410.
- `evidence-lists`: `/<org>/evidence` (newest first, `[data-evidence-file]` rows with `[data-cell="caption"]`, `type` and `size`, `[data-mark="video"]`, `[data-badge="deleted"]`), and `[data-section="evidence-files"]` on the Agent session page and the Review record page.
- `evidence-access`: signed out goes to sign-in and back to the same URL. A member of another Organization gets 404 for the page and the file.
- `evidence-retention`: Settings → Agent sessions has `[data-section="evidence-retention"]` (Organization admins, 1 to 3650 days). The daily pass deletes the bytes of older files and sets their `deleted_at`. The page then has `[data-notice="evidence-deleted"]`.

## How to get to it (user POV)

- A link in a PR body that `open-pr` or `update-pr` wrote: `Screenshot: <caption>` or `Video: <caption>`.
- Sidebar group **Coding agents**, link **Evidence files**, which opens `/<org>/evidence`.
- The Evidence files list on an Agent session's page and on a Review record's page.
- Settings → Agent sessions, Evidence Retention (Organization admins).
- The CLI: `solidtrace evidence add`, signed in with `solidtrace login`.

## Driving it with verify

Preconditions:

- The baseline stack is up, and `"$V" doctor` is all `ok`.
- Agent sessions are on for `solid-trace-org`, and you have a `sessions:write` token. In the stack, `"$V" run -- bin/rails runner <file>` with a file that calls `AgentSessionsSettings.turn_on(organization:, user:, entries: ["github.com/example/shop"], confirmed: true)` and `ApiToken.issue!(user:, name:, scopes: ["sessions:write"], organization_id:)`, and writes the secret to a file. Never print the token.
- For the CLI, build it from the checkout (`cd cli && go build -o <dir>/solidtrace ./cmd/solidtrace`) and run it with `SOLIDTRACE_CONFIG_DIR` set to a new folder, so that your own CLI sign-in does not change. Sign in to the stack with `solidtrace login --device`.

- **Upload with curl.** `curl -s -w '\n%{http_code}\n' -X POST "http://localhost:4510/api/agent/v1/agent-sessions/$(cat /proc/sys/kernel/random/uuid)/evidence?caption=Shot" -H "Authorization: Bearer $(cat <token file>)" --data-binary @shot.png`. Pass: 201 and a JSON line whose `url` is `http://localhost:4511/solid-trace-org/evidence/<id>`. The same command with the same session and file gives 200 and the same `id`. An SVG gives 415, and a 33 MiB file gives 413.
- **Upload with the CLI.** `SOLIDTRACE_CONFIG_DIR=<dir> <dir>/solidtrace evidence add shot.png clip.webm --caption "Review records list" --session <uuid>`. Pass: two lines, each a link, a type and a size, and exit 0. Without `--session`, the command takes `CLAUDE_CODE_SESSION_ID`, which Claude Code sets in its Bash tool.
- **Where the bytes go.** With no bucket (the stack), the file is `<run>/stack/evidence/evidence/org=<org id>/<id>.<ext>`, and `<run>/stack/evidence/tmp/` is empty after the upload. The row: `"$V" run -- bin/rails runner 'p EvidenceFile.order(:created_at).last.attributes.slice("id", "agent_session_id", "content_type", "byte_size", "storage_key", "deleted_at")'`.
- **A range read.** `curl -s -H 'Range: bytes=100-199' http://127.0.0.1:4512/evidence/<id>/content -o part.bin`, then `cmp part.bin <(dd if=shot.png bs=1 skip=100 count=100 status=none)`. Pass: 206, and `cmp` is silent.
- **The page, the video and the lists.** `"$V" flow --video -- e2e/flows/evidence-files.spec.ts`. Pass: the signed-out member lands on the PNG's page after sign-in, and the image has `naturalWidth` > 0. The video reaches `currentTime` ≥ 5 with a 206. At 0.5x it moves 0.4 to 0.6 s of media in each second of wall time, and 0.5x stays after a reload. The headers are in `flow/headers.txt`. Both lists show both files, newest first, with the video mark and the deleted badge. Evidence files is the one lit entry in the sidebar. A member of another Organization gets 404 for both pages and both files.
- **One page, signed in.** `"$V" snap --path /solid-trace-org/evidence/<id> --name evidence-page --as member --wait-text <caption>`. Pass: status 200 and no page errors.
- **Evidence Retention.** `"$V" flow -- e2e/flows/evidence-retention.spec.ts`. Pass: the admin sets 1 day on Settings → Agent sessions (0 is refused), the flow moves a file's `created_at` 2 days back, then `POST http://127.0.0.1:4512/retention` runs the daily pass now. The page then shows `[data-notice="evidence-deleted"]` with the date, the file URL gives 410, and the bytes are gone from `<run>/stack/evidence/evidence/`.
- **Proof.** The traces, the videos and the screenshots in `<run>/evidence/flow-<time>/`, and the rows and files above.

## Gotchas

- A new stack has Agent sessions off. Each upload then gets 403 with the reason. Turn them on first, as the precondition says.
- EventStore takes the type from the bytes, not from the name. A PNG name on HTML bytes gives 415.
- `POST /retention` is on the query port (loopback only). It runs the whole daily pass, Log Retention too. Use it only on your own stack.
- A `--video` flow writes Chromium's shared memory to `/tmp` on the root disk, up to about 1.8 GB. Check `df -h /` before the flow.
- Do not upload a file that has a secret on it. The agent decides what is safe to upload.
