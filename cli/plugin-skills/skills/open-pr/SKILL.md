---
name: open-pr
description: Run the final review, push this branch and open its PR, with a record of every review finding and every decision on it.
disable-model-invocation: true
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

The title is one line that says what the change does. The body has these sections, in this order, and each is short:

- **Summary**: what changes for the user, and why, from the record's `story`.
- **Evidence**: the tests and checks that ran, and their result.
- **Review**: one row per finding found by a reviewer: id, stage and step, reviewer, severity, title, the decisions in order (for example "trivial, verifier pushed back, fixed"), outcome, and what the person decided, in your own short words. Then one line per finding found outside the reviews, as "Missed by the review, found by the person" (or "by an agent", "by a test run"). Then the confirmations that were not given.

In every section, do not quote the person's messages from the sessions: they are private, and can hold host names or secrets. Say what they asked for or decided in your own short words.

End the body with the line `<!-- solidtrace-record round 1 -->`, which `update-pr` reads. Write the body to `$dir/body.md`.

## 7. Open the PR

Open the PR as [platforms.md](platforms.md) says, in Open a PR.

## 8. Final reply

Give the PR's URL, or in local mode the file's path. Then run the command of step 4.4 again, with the same `--round`, and with `--pr` set to that URL, or to `local:<the file's path>` in local mode (no `--pr` when no PR opened), and end the reply with the record in one `solidtrace-pr-record` block, as one line of JSON:

````
```solidtrace-pr-record
{"version": 2, ...}
```
````

This block is how SolidTrace keeps the record: the reply uploads with the session.
