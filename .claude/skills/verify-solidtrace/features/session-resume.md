# Session resume

`solidtrace resume <id>` continues an Agent session. On the machine where it started (its transcript is in a Claude Code project folder and its start folder is there), it starts `claude --resume <id>` in that folder, with the same id and no server call. On another machine, it downloads the Agent session into a temporary folder (`$TMPDIR/solidtrace-resume-*`), never into a Claude Code project folder, and starts `claude --resume <temporary transcript> --fork-session --session-id <new id>`: a new Agent session from the history. This is the same for the person's own Agent session, another person's and a Run's. The Agent session page shows the command under **Resume on another machine**, with **Copy**.

## Sub-features

- `resume-page`: `/<org>/agent_sessions/<id>` has `[data-section="resume"]` with the heading `Resume on another machine`, `[data-command="resume"]` with `solidtrace resume <id>`, and the button `Copy the resume command`. Each viewer gets the same text: on the machine where the Agent session started, the command continues it; on another machine, it starts a new Agent session from its history.
- `resume-in-place`: the owner resumes on the first machine. The command is `claude --resume <id>` in the start folder, with the same id.
- `resume-fork`: the owner on a second machine, and another person, each resume. Each gets a fork with a new id, which uploads as their own Agent session. No project folder gets the download, and the first Agent session does not change.
- `resume-refusals`: a checkout of another repository, and a sign-in without `sessions:download`, each exit 1 with what to do.

## How to get to it (user POV)

- The Agent session page, section **Resume on another machine**, button **Copy**.
- `solidtrace resume SESSION_ID [--print] [-- CLAUDE_ARGS...]` in a terminal, in a checkout of the repository of the Agent session.

## Driving it with verify

Preconditions:

- The baseline stack is up, and `"$V" doctor` is all `ok`. Read the Console and ingest ports from `"$V" doctor`. The commands below use slot 1 (`4511` and `4510`).
- Agent sessions are on, with a captured entry such as `flows.example/flowteam/*`. The page path: Settings → Agent sessions, as the admin.
- For the CLI steps: a `solidtrace` CLI built from this checkout, and a Claude Code that is signed in. Use Haiku to keep the cost low.

- **Page and Copy.** `"$V" flow -- e2e/flows/agent-sessions.spec.ts -g "resumes it on another machine"`. Pass: the command is `solidtrace resume <id>`, the clipboard has the same text after **Copy**, the member sees the same text as the admin, and the page has no sideways scroll at 390 px. The test folder has `resume-desktop-light.png`, `resume-desktop-dark.png`, `resume-phone-light.png` and `resume-phone-dark.png`.
- **Build the CLI and make two machines.** Each machine is a CLI config folder and a Claude Code config folder. Copy the Claude Code sign-in into each Claude Code config folder:

  ```bash
  W=~/.cache/solid-trace/resume-check; mkdir -p "$W"
  (cd cli && CGO_ENABLED=0 go build -o "$W/solidtrace" ./cmd/solidtrace)
  for m in a1 a2 b; do mkdir -p "$W/$m/st" "$W/$m/claude"; cp ~/.claude/.credentials.json "$W/$m/claude/"; done
  m() { SOLIDTRACE_CONFIG_DIR="$W/$1/st" CLAUDE_CONFIG_DIR="$W/$1/claude" "${@:2}"; }
  ```

- **Sign in.** For each machine: `m a1 "$W/solidtrace" login --device --ingest-url http://localhost:4510 http://localhost:4511`. Open the printed device URL, type the code, and approve, as the admin for `a1` and `a2`, and as the member for `b`. Pass: exit 0 and `Signed in to`.
- **Make the Agent session (person A, path P1).** Make a checkout with a remote that matches the captured entry, for example `git init "$W/p1" && git -C "$W/p1" remote add origin git@flows.example:flowteam/resume.git`. Then, in `$W/p1`: `m a1 claude -p --model haiku --output-format json "Remember the word PELICAN. Reply OK."`. Note `session_id`. Upload it as the SessionEnd hook does: `echo '{"session_id":"<id>","transcript_path":"<the <id>.jsonl under $W/a1/claude/projects>","cwd":"'"$W/p1"'","hook_event_name":"SessionEnd"}' | m a1 "$W/solidtrace" capture`. Pass: the Agent session page opens.
- **In place (person A, the first machine).** In `$W`, not in P1: `m a1 "$W/solidtrace" resume <id> --print`. Pass: the line is `cd <P1> && claude --resume <id>`, with no `--fork-session`, and stderr is empty. Then `cd "$W/p1" && m a1 claude -p --model haiku --output-format json --resume <id> "Which word did I ask you to remember?"`. Pass: PELICAN, and the same `session_id`.
- **Move P1 away.** `mv "$W/p1" "$W/p1.moved-away"`. A second machine does not have P1, and the first cwd of the history is P1. While P1 is on this host, capture uses it as the start folder of the fork, so move P1 away before the next steps.
- **Fork (person A, a second machine, path P2).** Make a second checkout of the same remote at a longer path, for example `$W/a-much-longer-folder-name/p2`. In it: `m a2 "$W/solidtrace" resume <id> -- -p --model haiku --output-format json "Which word did I ask you to remember?"`. Pass: stderr has `the new Agent session <new id> starts from the history of the Agent session <id>`, the answer has PELICAN, and its `session_id` is the new id. `$W/a2/claude/projects/<name of P2>/<new id>.jsonl` exists, and no project folder under `$W/a2/claude/projects` has `<id>.jsonl`. `$W/a2/st/capture/<new id>.status.json` has `start_dir` P2. Capture the new id as above, with `a2`, its transcript and P2. Pass: the new Agent session page shows the admin as its person, and the Tokens of the first Agent session do not change.
- **Fork (person B).** A third checkout, `$W/p3`, of the same remote. In it, the same command with `m b`. Pass: as for P2, with another new id. Capture the new id as above, with `b` and P3, but with `"hook_event_name":"SessionStart"`, so that the sweep runs too. Pass: the new Agent session page shows the member as its person, and `$W/b/st/capture.log` has no line for `<id>`, because `b` has no copy of it.
- **Refusals.** In a checkout with another remote: `m b "$W/solidtrace" resume <id>`. Pass: exit 1 and "Clone <repository>". With an old sign-in: remove `sessions:download` from the scope in `$W/b/st/credentials.json`, and run the command again. Pass: exit 1 and "Run solidtrace login again".
- **Proof.** The flow's trace and screenshots, and each CLI command with its stdout, stderr and exit code.

## Gotchas

- The name of the project folder comes from the absolute path of the Agent session's folder. It changes each character that is not `[A-Za-z0-9]` to `-`. A path of more than 200 characters gets a hash at the end. Use a long P2 to prove that capture finds the start folder of the fork from its record.
- `claude -p` in a new `CLAUDE_CONFIG_DIR` has no sign-in. Copy `.credentials.json`, or set `ANTHROPIC_API_KEY`.
- The page section shows on each Agent session page, also when the flags are `partial` or `cut`. The CLI refuses those, and the page's notice above the header says that a resume can fail.
- The fork does not have the `<id>/` folder (saved tool outputs, subagents). The transcript names saved outputs by their paths on the first machine, so Claude Code cannot read them on the second machine. It still has the start of each output in the transcript.
- Remove `$W` after the check. It has copies of the Claude Code sign-in.
