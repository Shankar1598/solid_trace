---
name: open-pr
description: Run the final review, push this branch and open its PR, with a record of every review finding and every decision on it. Use when the person, or the plan that the agent follows, asks to open the PR of a finished branch, also in local mode.
---

# Open PR

The person typed this command, so push the branch and open the PR without asking again, except where a step says to ask. The PR body is for the human reviewer. The record is data for the team that improves its reviews.

**Local mode.** When the arguments include `local` (`$ARGUMENTS`), do every step but do not push and do not open a PR. Step 7 writes the PR to a file instead. Use it to test this skill, or to read the PR before it goes out. To test `update-pr local` after it, write feedback in the feedback file ([platforms.md](platforms.md), The local PR file).

## 1. Check the branch

- The branch is not the base branch. The base branch is the one the person names, else the remote's default branch (`git remote show origin`). Run `git fetch origin <base>`, and use `origin/<base>` as `<base>` in every later step, so that the diff holds only this branch's work. In local mode, use the local base branch: the person may not have pushed it.
- The branch has no open PR ([platforms.md](platforms.md), Find the open PR of the branch). When a PR exists, stop and tell the person to run `/solidtrace-skills:update-pr`, which works on the PR's feedback.
- The working tree is clean. If it is not, list the changed files and ask the person whether to commit them. Continue when the tree is clean.

Make a folder for this PR's files: `dir=$(mktemp -d)`.

## 2. Find the sessions of this feature

A feature often spans several sessions. Run `solidtrace review sessions --base <base> --session ${CLAUDE_SESSION_ID} > "$dir/sessions.json"`.

- **Other sessions are listed.** Show the person a numbered list: the title, the dates, and the links (for example "3 of the PR's commits, 7 of its files"). The current session and each `strong` session are selected. Ask the person in one message which sessions to add or remove. Continue with the sessions that the person confirms.
- **Only the current session is listed.** Run the command again with `--all --days 3`. Spawn one sub-agent on the `sessions` role of `${CLAUDE_SKILL_DIR}/../code-review/models.md`, with the PR's commit subjects and the listed titles and dates, to name the sessions that are about the same work. When it names some, ask the person as above. When it names none, continue with the current session and ask nothing.

## 3. Final review

Each fix can add a new fault, so the reviews here run on the final diff, and security runs last. The diff is `git diff <base>...HEAD`.

You read two other skills' files here, and their `${CLAUDE_SKILL_DIR}` is not replaced when you read them. In `deep-review/SKILL.md`, it means `${CLAUDE_SKILL_DIR}/../deep-review`. In `code-review/SKILL.md`, it means `${CLAUDE_SKILL_DIR}/../code-review`. Write the full path in each prompt that you give a reviewer.

1. **Deep review.** Skip it when the diff changes only docs, tests or test data, when the person said to skip it, or when a `deep-review` block in the confirmed sessions has `head` equal to `git rev-parse HEAD`. To find such a block, run `solidtrace review digest` with one `--session ID` for each confirmed session, and read its `findings` events. When you skip it, record a `deep-review` block with `solidtrace review block` that has `"findings": []` and `skipped` (see `${CLAUDE_SKILL_DIR}/../code-review/findings.md`). Else read `${CLAUDE_SKILL_DIR}/../deep-review/SKILL.md` and follow it on the diff.
2. **High availability and infrastructure.** Run the `ha-and-infrastructure` reviewer as `code-review` runs a reviewer. Follow `${CLAUDE_SKILL_DIR}/../code-review/SKILL.md` from "Find the spec and the rules" to "Report, then fix", with the `ha-and-infrastructure` role of `${CLAUDE_SKILL_DIR}/../code-review/models.md`, the `REVIEW.md` sections High availability and Infrastructure, and the diff. Its block has `"trigger": "open-pr"`, `"stage": "final"` and `"step": "ha-and-infrastructure"`. Report and decide, and do not fix yet.
3. **Ask the person.** In one message, list each `ask` finding of 1 and 2, and each `high` finding with the decision `dispute`, `trivial` or `defer`, with your decision, and ask what to do with each. Wait for the answer. List the `high` findings that you fix in the same message, for the person to know, but do not wait for them. With nothing to ask, go on. When the answer changes a decision, put the new decision in an update block (`${CLAUDE_SKILL_DIR}/../code-review/findings.md`, The update block).
4. **Fix.** Fix the findings with decision `fix` and the ones the person said to fix. Run the tests that cover them. Commit the fixes with explicit paths and the `Review-Findings` trailers, then run `solidtrace review fixed`, as in `code-review/SKILL.md` › Report, then fix.
5. **Security, last.** Run the `security` reviewer in the same way, with the `security` role, on the diff after step 4, with the `REVIEW.md` section Security. Its block has `"trigger": "open-pr"`, `"stage": "final"` and `"step": "security"`. Ask the person as in step 3, then fix and commit as in step 4. When a fix changed code, run the security reviewer once more on the fix diff only (`git diff <the head before the fix>`). Stop after this second round. Decide its findings, fix the ones you decide to fix, and commit as in step 4. In the PR, list them as fixed after the last review, and list what is still open.

## 4. Start the record and the push at the same time

- **The record.** Run the digest in a later message than your last block, because Claude Code writes a message to the transcript only when its tool calls end. Then:
  1. Run `solidtrace review digest` with one `--session ID` for each confirmed session, `> "$dir/digest.json"`.
  2. Run `solidtrace review record --digest "$dir/digest.json" > "$dir/record0.json"`. This is the mechanical part of the record.
  3. Spawn one sub-agent with `subagent_type: "solidtrace-skills:record-writer"`, which can only read files, on the `record` role of `${CLAUDE_SKILL_DIR}/../code-review/models.md`. Its prompt is the text of [record.md](record.md), then the two paths to read: `$dir/digest.json` and `$dir/record0.json`. It replies with the notes. Write them to `$dir/notes.json`.
  4. Run `solidtrace review record --digest "$dir/digest.json" --notes "$dir/notes.json" --round 1 > "$dir/record.json"`. When it refuses a note (exit 1), send the sub-agent the error, and wait for new notes. After two refusals, use `$dir/record0.json` with `--pr` and `--round` as the record, and say in the final reply which notes were refused.
- **The push.** Run `git push -u origin HEAD`, and wait for its result. When it fails, stop before step 7: tell the person why, and do not open a PR on an older remote head. In local mode, skip it.

## 5. Check the record

Run `solidtrace review check --digest "$dir/digest.json" --record "$dir/record.json"`. It prints the problems it finds, and exits 1 when there are any.

When it finds problems, send the sub-agent the problems, wait for new notes, and merge them again. After two failed rounds, continue with the record as it is, and say so in the final reply.

## 6. Write the PR

Write the title and the body with the `technical-writing` skill of the pstack plugin, when it is installed. Find its file with `ls -d ~/.claude/plugins/cache/*/pstack/*/skills/technical-writing/SKILL.md 2>/dev/null | sort -V | tail -1`. Read the file and follow it, because the Skill tool does not run that skill. When the command prints nothing, write short sentences with one thought each, in the words of the repository's glossary.

The title is one line that says what the change does. The body is a briefing that a reviewer reads in about one minute, in at most 60 lines. It has these sections, in this order:

- **What changes**: one `##` section for each change that a user or an operator sees, from the record's `story`, with no parent heading. The heading says the point, for example "Review records moves into Agent sessions". Say what changes and why. Name the routes, the limits and the fallbacks as the code names them.
- **Where to look**: the two to four files or functions that a reviewer reads first, and why each one.
- **Evidence**: the tests and checks that ran, what each one covers, and their result. Name the suites that did not run. Then the links of the Evidence files (below), one on each line.
- **Review**, in short sentences:
  - the count of reviews and of their findings, and how many of those findings are fixed, disputed, deferred or open;
  - for each review, what it found that changed the code, or that it found nothing;
  - for each finding found outside the reviews, what it was, who found it (the person, an agent or a test run), and its outcome;
  - the confirmations that were not given.

  Do not list each finding of the reviews. End the section with one line that says the Review record has each finding and its decisions.

In every section, do not quote the person's messages from the sessions: they are private, and can hold host names or secrets. Say what they asked for or decided in your own short words.

**The Evidence files.** Upload the screenshots and videos that prove this PR to SolidTrace, for example the files of a verify-solidtrace run. They are in the run directory that its `up` command printed as its last line: each screenshot at `<run>/evidence/<name>.png`, and each video of a `flow --video` at `<run>/evidence/flow-<HHMMSS>/playwright/<test>/*.webm`. A reviewer who is a member of the Organization opens each link in Console after sign-in. Upload only the files that prove this PR. Look at each file before you upload it: each member of the Organization can see it, so upload only a file that shows no secret, token, password or private data. You decide which files are safe, and no person confirms each file. With no such file, upload nothing.

Upload each file in its own call, with a caption that says what the file proves, in at most 200 characters:

```bash
solidtrace evidence add ~/.cache/solid-trace/verify/runs/20261122T085753Z-s1/evidence/review-records-list.png --caption "The Review records list, newest PR first"
```

For each file, the command prints one line: the link, the type and the size, for example `https://console.example.com/acme/evidence/7c1d2e3f-4a5b-4c6d-8e9f-0a1b2c3d4e5f image/png 84 KB`. Write one Markdown link in Evidence for each line: `[Screenshot: <caption>](<link>)` for an `image/` type, and `[Video: <caption>](<link>)` for a `video/` type.

When a call exits with a code that is not 0, write no link for its file, and continue with the PR. In the final reply, give the file and the error that the command printed. For example, an old SolidTrace server says `this SolidTrace server is too old for Evidence files: ask the operator to upgrade it`.

End the body with the line `<!-- solidtrace-record round 1 -->`, which `update-pr` reads. Write the body to `$dir/body.md`.

## 7. Open the PR

Open the PR as [platforms.md](platforms.md) says, in Open a PR.

## 8. Final reply

Run the command of step 4.4 again, with the same `--round`, and with `--pr` set to the PR's URL, or to `local:<the file's path>` in local mode (no `--pr` when no PR opened). Then send the record to `solidtrace review final` in one Bash call, with the record's JSON as one line in a heredoc:

````
solidtrace review final <<'SOLIDTRACE_RECORD_END'
{"version": 2, ...}
SOLIDTRACE_RECORD_END
````

This call is how SolidTrace keeps the record: the call uploads with the session. Put the whole JSON in the heredoc, not a path to its file, and keep the word `'SOLIDTRACE_RECORD_END'` in single quotes, so that the shell does not change the text of the record: SolidTrace keeps no record from a word with no quotes. The record holds the words of PR commenters, so do not use a short word such as `EOF`, which a comment can hold on a line of its own. The command must be only this call: no pipe, `&&`, `;`, redirect or other command, or SolidTrace does not keep the record. It prints the round and the counts on one line. When it refuses the record (exit 1), fix what it names and run it again. When the call fails with "Argument list too long" (Linux refuses a command of 128 KiB or more), SolidTrace keeps the record all the same: do not send it again, and say so in the reply.

Then give the PR's URL, or in local mode the file's path, and a short summary of the findings. Do not write the record's JSON in the reply.
