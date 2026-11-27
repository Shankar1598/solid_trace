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
- Agent sessions are on, with a captured entry such as `flows.example/flowteam/*`. The page path: Settings → Agent sessions, as the admin. As a fixture: `"$V" run -- bin/rails runner 'AgentSessionsSettings.turn_on(organization: Organization.find_by!(slug: "solid-trace-org"), user: User.find_by!(email: "admin@solidtrace.local"), entries: ["flows.example/flowteam/*"], confirmed: true)'`.
- For the CLI steps: a `solidtrace` CLI built from this checkout, and a Claude Code that is signed in. Use Haiku to keep the cost low.

- **Page and Copy.** `"$V" flow -- e2e/flows/agent-sessions.spec.ts -g "resumes it on another machine"`. Pass: the command is `solidtrace resume <id>`, the clipboard has the same text after **Copy**, the member sees the same text as the admin, and the page has no sideways scroll at 390 px. The test folder has `resume-desktop-light.png`, `resume-desktop-dark.png`, `resume-phone-light.png` and `resume-phone-dark.png`.
- **Build the CLI and make three machines.** Each machine is a CLI config folder and a Claude Code config folder. Copy the Claude Code sign-in into each Claude Code config folder. Give each Claude Code config folder the hooks of the plugin (`cli/plugin/hooks/hooks.json`), with the path of this CLI, so that each `claude` call uploads as on a real machine:

  ```bash
  W=~/.cache/solid-trace/resume-check; mkdir -p "$W"
  (cd cli && CGO_ENABLED=0 go build -o "$W/solidtrace" ./cmd/solidtrace)
  for m in a1 a2 b; do
    mkdir -p "$W/$m/st" "$W/$m/claude"; cp ~/.claude/.credentials.json "$W/$m/claude/"
    sed "s|\"solidtrace capture|\"$W/solidtrace capture|" cli/plugin/hooks/hooks.json > "$W/$m/claude/settings.json"
  done
  m() { SOLIDTRACE_CONFIG_DIR="$W/$1/st" CLAUDE_CONFIG_DIR="$W/$1/claude" "${@:2}"; }
  # The server's files of an Agent session, with their end_offset.
  files() { curl -s -H "Authorization: Bearer $(jq -r .access_token "$W/$1/st/credentials.json")" \
    "http://localhost:4510/api/agent/v1/agent-sessions/$2/files"; }
  ```

- **Sign in.** For each machine: `m a1 "$W/solidtrace" login --device --ingest-url http://localhost:4510 http://localhost:4511`, as a background command. The code is on the line after `type this code:`. Approve it with Playwright from `console/`, as `signIn` in `e2e/support/factoryFlow.ts` does: sign in at `/login`, open the device URL, fill `Code`, click `Continue`, then `Approve`, and wait for the `status` role. Use the admin for `a1` and `a2`, and the member for `b`. Pass: exit 0, `Signed in to`, and `scope` in `$W/<machine>/st/credentials.json` has `sessions:download`.
- **Make the Agent session (person A, path P1).** Make a checkout with a remote that matches the captured entry, for example `git init "$W/p1" && git -C "$W/p1" remote add origin git@flows.example:flowteam/resume.git`. Then, in `$W/p1`: `m a1 claude -p --model haiku --output-format json "Remember the word PELICAN. Reply OK." </dev/null`. Note `session_id` (`<id>`). The hooks upload it. Pass: `$W/a1/st/capture/<id>.status.json` has `"status":"uploaded"`, and `files a1 <id>` has `<id>.jsonl` with an `end_offset` that is the size of the transcript. Note that `end_offset`.
- **Move P1 away.** `mv "$W/p1" "$W/p1.moved-away"`. A second machine does not have P1, and the first cwd of the history is P1. While P1 is on this host, capture uses it as the start folder of the fork, so move P1 away before the fork steps.
- **Fork (person A, a second machine, path P2).** Make a second checkout of the same remote at a longer path, for example `$W/a-much-longer-folder-name/p2`. In it: `m a2 "$W/solidtrace" resume <id> -- -p --model haiku --output-format json "What word did I ask you to remember? Reply with the word only." </dev/null`. Pass: stderr has `the new Agent session <new id> starts from the history of the Agent session <id>`, `result` is PELICAN, and `session_id` is the new id. `$W/a2/claude/projects/<name of P2>/<new id>.jsonl` exists, and no project folder under `$W/a2/claude/projects` has `<id>.jsonl`. `ls ${TMPDIR:-/tmp}/solidtrace-resume-*/` has `<id>.jsonl`. `$W/a2/st/capture/<new id>.status.json` has `"status":"uploaded"` and `start_dir` P2, and `$W/a2/st/capture/` has no file of `<id>`. `files a2 <new id>` has `person.user_id` of the admin, and `files a2 <id>` has the same `end_offset` as before.
- **Fork (person B).** A third checkout, `$W/p3`, of the same remote. In it, the same command with `m b`. Pass: as for P2, with another new id, and `person.user_id` of the member. The SessionStart hook ran capture's sweep too: `$W/b/st/capture/` has no file of `<id>`.
- **In place (person A, the first machine).** `mv "$W/p1.moved-away" "$W/p1"`. In `$W`, not in P1: `m a1 "$W/solidtrace" resume <id> --print`. Pass: the line is `cd <P1> && claude --resume <id>`, with no `--fork-session`, and stderr is empty. Then, also in `$W`: `m a1 strace -f -qq -e trace=connect,execve -o "$W/in-place.strace" "$W/solidtrace" resume <id> -- -p --model haiku --output-format json "Reply again with the word that I asked you to remember. Reply with the word only." </dev/null`. Pass: `result` is PELICAN, `session_id` is `<id>`, `$W/a1/claude/projects` has only `<id>.jsonl` in the folder of P1, and stderr is empty. In the strace file, the `execve` of `claude` comes before any `connect` of the `resume` process: resume asks the server nothing. (Each `connect` to the ingest port comes from a `solidtrace capture` process of a hook.) After the hooks, `files a1 <id>` has a larger `end_offset`.
- **Refusals.** In a checkout with another remote: `m a2 "$W/solidtrace" resume <id>`. Pass: exit 1 and "Clone <repository>". With an old sign-in: remove `sessions:download` from the scope in `$W/b/st/credentials.json`, and run the command again in P3. Pass: exit 1 and "Run solidtrace login again".
- **Proof.** The flow's trace and screenshots, each CLI command with its stdout, stderr and exit code, the `files` answers with the `end_offset` of each step, and the strace file of the in-place step.

## Gotchas

- The name of the project folder comes from the absolute path of the Agent session's folder. It changes each character that is not `[A-Za-z0-9]` to `-`. A path of more than 200 characters gets a hash at the end. Use a long P2 to prove that capture finds the start folder of the fork from its record.
- `claude -p` in a new `CLAUDE_CONFIG_DIR` has no sign-in. Copy `.credentials.json`, or set `ANTHROPIC_API_KEY`.
- The page section shows on each Agent session page, also when the flags are `partial` or `cut`. The CLI refuses those, and the page's notice above the header says that a resume can fail.
- The fork does not have the `<id>/` folder (saved tool outputs, subagents). The transcript names saved outputs by their paths on the first machine, so Claude Code cannot read them on the second machine. It still has the start of each output in the transcript.
- `capture.log` gets only failures and skips. After clean uploads it does not exist. To prove that a machine sent no copy of an Agent session, look in `capture/` of its config folder and at the `end_offset` on the server.
- Haiku can read a prompt such as "Reply with the word again." literally, and answer "Again". Name the word that it must remember in each prompt.
- `claude -p` with no stdin waits 3 s and warns. Give it `</dev/null`.
- The page of a fork names its person as the author of each prompt of the history, also of the prompts that another person wrote in the first Agent session.
- Remove `$W` after the check. It has copies of the Claude Code sign-in.
