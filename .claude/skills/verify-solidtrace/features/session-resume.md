# Session resume

`solidtrace resume <id>` downloads an Agent session from SolidTrace into the Claude Code project folder of a checkout, and starts `claude --resume`. The person's own Agent session continues with the same id. Another person's Agent session, or a Run's, continues as a new Agent session (`--fork-session`). The Agent session page shows the command under **Resume on another machine**, with **Copy**.

## Sub-features

- `resume-page`: `/<org>/agent_sessions/<id>` has `[data-section="resume"]` with the heading `Resume on another machine`, `[data-command="resume"]` with `solidtrace resume <id>`, and the button `Copy the resume command`. The text says "with the same id" for the viewer's own Agent session, and "as a new Agent session of yours" for another person's or a Run's.
- `resume-same-id`: the owner resumes in a second checkout at another path, with a second Claude Code config. The id stays the same, and capture adds to the same Agent session.
- `resume-fork`: another person resumes. `--print` shows `--fork-session`, and the new Agent session uploads as theirs. The first Agent session does not change.
- `resume-refusals`: a checkout of another repository, and a sign-in without `sessions:download`, each exit 1 with what to do.

## How to get to it (user POV)

- The Agent session page, section **Resume on another machine**, button **Copy**.
- `solidtrace resume SESSION_ID [--fork] [--print] [-- CLAUDE_ARGS...]` in a terminal, in a checkout of the repository of the Agent session.

## Driving it with verify

Preconditions:

- The baseline stack is up, and `"$V" doctor` is all `ok`. Read the Console and ingest ports from `"$V" doctor`. The commands below use slot 1 (`4511` and `4510`).
- Agent sessions are on, with a captured entry such as `flows.example/flowteam/*`. The page path: Settings → Agent sessions, as the admin.
- For the CLI steps: a `solidtrace` CLI built from this checkout, and a Claude Code that is signed in. Use Haiku to keep the cost low.

- **Page and Copy.** `"$V" flow -- e2e/flows/agent-sessions.spec.ts -g "resumes it on another machine"`. Pass: the command is `solidtrace resume <id>`, the clipboard has the same text after **Copy**, the member sees "as a new Agent session of yours", and the page has no sideways scroll at 390 px. The test folder has `resume-desktop-light.png`, `resume-desktop-dark.png`, `resume-phone-light.png` and `resume-phone-dark.png`.
- **Build the CLI and make two machines.** Each machine is a CLI config folder and a Claude Code config folder. Copy the Claude Code sign-in into each Claude Code config folder:

  ```bash
  W=~/.cache/solid-trace/resume-check; mkdir -p "$W"
  (cd cli && CGO_ENABLED=0 go build -o "$W/solidtrace" ./cmd/solidtrace)
  for m in a1 a2 b; do mkdir -p "$W/$m/st" "$W/$m/claude"; cp ~/.claude/.credentials.json "$W/$m/claude/"; done
  m() { SOLIDTRACE_CONFIG_DIR="$W/$1/st" CLAUDE_CONFIG_DIR="$W/$1/claude" "${@:2}"; }
  ```

- **Sign in.** For each machine: `m a1 "$W/solidtrace" login --device --ingest-url http://localhost:4510 http://localhost:4511`. Open the printed device URL, type the code, and approve, as the admin for `a1` and `a2`, and as the member for `b`. Pass: exit 0 and `Signed in to`.
- **Make the Agent session (person A, path P1).** Make a checkout with a remote that matches the captured entry, for example `git init "$W/p1" && git -C "$W/p1" remote add origin git@flows.example:flowteam/resume.git`. Then, in `$W/p1`: `m a1 claude -p --model haiku --output-format json "Remember the word PELICAN. Reply OK."`. Note `session_id`. Upload it as the SessionEnd hook does: `echo '{"session_id":"<id>","transcript_path":"<the <id>.jsonl under $W/a1/claude/projects>","cwd":"'"$W/p1"'","hook_event_name":"SessionEnd"}' | m a1 "$W/solidtrace" capture`. Pass: the Agent session page opens.
- **Move P1 away.** `mv "$W/p1" "$W/p1.moved-away"`. The first cwd of the transcript is P1. While P1 is on this host, capture uses it as the start folder, and its fallback for a second machine does not run. A second machine does not have P1, so move P1 away before the resume. Then capture must find P2 from the project folder of the transcript.
- **Same id (person A, path P2).** Make a second checkout of the same remote at a longer path, for example `$W/a-much-longer-folder-name/p2`. In it: `m a2 "$W/solidtrace" resume <id> --print`. Pass: the line is `cd <P2> && claude --resume <id>`, with no `--fork-session`, and `$W/a2/claude/projects/<name of P2>/<id>.jsonl` exists. Then `m a2 claude -p --model haiku --resume <id> "Which word did I ask you to remember?"`. Pass: PELICAN, and the same `session_id`. Capture it as above with `a2` and P2. Pass: on the page, the Last activity and the Tokens go up, and `$W/a2/st/capture.log` has no 409.
- **Fork (person B).** A third checkout, `$W/p3`, of the same remote. `m b "$W/solidtrace" resume <id> --print`. Pass: the command has `--fork-session`. Then `m b claude -p --model haiku --resume <id> --fork-session "Repeat the word."`. Pass: PELICAN and a new `session_id`. Capture the new id with `b`. Pass: the new Agent session shows the member as its person, and the Tokens of the first Agent session do not change.
- **Refusals.** In a checkout with another remote: `m b "$W/solidtrace" resume <id>`. Pass: exit 1 and "Clone <repository>". With an old sign-in: remove `sessions:download` from the scope in `$W/b/st/credentials.json`, and run the command again. Pass: exit 1 and "Run solidtrace login again".
- **Proof.** The flow's trace and screenshots, and each CLI command with its stdout, stderr and exit code.

## Gotchas

- The name of the project folder comes from the absolute path of the Agent session's folder. It changes each character that is not `[A-Za-z0-9]` to `-`. A path of more than 200 characters gets a hash at the end. Use a long P2 to prove the two paths.
- `claude -p` in a new `CLAUDE_CONFIG_DIR` has no sign-in. Copy `.credentials.json`, or set `ANTHROPIC_API_KEY`.
- The page section shows on each Agent session page, also when the flags are `partial` or `cut`. The CLI refuses those, and the page's notice above the header says that a resume can fail.
- Two machines on the same id at the same time: the second gets 409 at capture and keeps its bytes. Close the Agent session on the first machine before the same-id step.
- Remove `$W` after the check. It has copies of the Claude Code sign-in.
