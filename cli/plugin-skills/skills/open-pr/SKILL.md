---
name: open-pr
description: Run the final review, push this branch and open its PR, with a record of every review finding and every decision on it.
disable-model-invocation: true
---

# Open PR

The person typed this command, so push the branch and open the PR without asking again, except where a step says to ask. The PR body is for the human reviewer. The record is data for the team that improves its reviews.

**Local mode.** When the arguments include `local` (`$ARGUMENTS`), do every step but do not push and do not open a PR. Step 7 writes the PR to a file instead. Use it to test this skill, or to read the PR before it goes out. To test `update-pr local` after it, write feedback in `<branch>.comments.md` next to the PR file, in the form that `update-pr` gives.

## 1. Check the branch

- The branch is not the base branch. The base branch is the one the person names, else the remote's default branch (`git remote show origin`). Run `git fetch origin <base>`, and use `origin/<base>` as `<base>` in every later step, so that the diff holds only this branch's work. In local mode, use the local base branch: the person may not have pushed it.
- The branch has no open PR. On github.com, run `gh pr list --head <branch> --state open`. On bitbucket.org, send `GET https://api.bitbucket.org/2.0/repositories/<workspace>/<repo>/pullrequests?q=source.branch.name="<branch>" AND state="OPEN"`. In local mode, look for the file of step 7. When a PR exists, stop and tell the person to run `/solidtrace-skills:update-pr`, which works on the PR's feedback.
- The working tree is clean. If it is not, list the changed files and ask the person whether to commit them. Continue when the tree is clean.

Make a folder for this PR's files: `dir=$(mktemp -d)`.

## 2. Find the sessions of this feature

A feature often spans several sessions. Run `solidtrace review sessions --base <base> --session ${CLAUDE_SESSION_ID} > "$dir/sessions.json"`.

- **Other sessions are listed.** Show the person a numbered list: the title, the dates, and the links (for example "3 of the PR's commits, 7 of its files"). The current session and each `strong` session are selected. Ask the person in one message which sessions to add or remove. Continue with the sessions that the person confirms.
- **Only the current session is listed.** Run the command again with `--all --days 3`. Spawn one sub-agent on the `sessions` role of `${CLAUDE_SKILL_DIR}/../code-review/models.md`, with the PR's commit subjects and the listed titles and dates, to name the sessions that are about the same work. When it names some, ask the person as above. When it names none, continue with the current session and ask nothing.

## 3. Final review

Each fix can add a new fault, so the reviews here run on the final diff, and security runs last. The diff is `git diff <base>...HEAD`.

You read two other skills' files here, and their `${CLAUDE_SKILL_DIR}` is not replaced when you read them. In `deep-review/SKILL.md`, it means `${CLAUDE_SKILL_DIR}/../deep-review`. In `code-review/SKILL.md`, it means `${CLAUDE_SKILL_DIR}/../code-review`. Write the full path in each prompt that you give a reviewer.

1. **Deep review.** Skip it when the diff changes only docs, tests or test data, when the person said to skip it, or when a `deep-review` block in the confirmed sessions has `head` equal to `git rev-parse HEAD`. To find such a block, run `solidtrace review digest` with one `--session ID` for each confirmed session, and read its `findings` events. When you skip it, end a reply with a `deep-review` block that has `"findings": []` and `skipped` (see `${CLAUDE_SKILL_DIR}/../code-review/findings.md`). Else read `${CLAUDE_SKILL_DIR}/../deep-review/SKILL.md` and follow it on the diff.
2. **High availability and infrastructure.** Run the `ha-and-infrastructure` reviewer as `code-review` runs a reviewer. Follow `${CLAUDE_SKILL_DIR}/../code-review/SKILL.md` from "Find the spec and the rules" to "Report, then fix", with the `ha-and-infrastructure` role of `${CLAUDE_SKILL_DIR}/../code-review/models.md`, the `REVIEW.md` sections High availability and Infrastructure, and the diff. Its block has `"trigger": "open-pr"`, `"stage": "final"` and `"step": "ha-and-infrastructure"`. Report and decide, and do not fix yet.
3. **Ask the person.** In one message, list each `high` finding and each `ask` finding of 1 and 2, with your decision, and ask what to do with each. Wait for the answer. When the answer changes a decision, put the new decision in an update block (`${CLAUDE_SKILL_DIR}/../code-review/findings.md`, The update block).
4. **Fix.** Fix the findings with decision `fix` and the ones the person said to fix. Run the tests that cover them. Commit the fixes with explicit paths and the `Review-Findings` trailers, then end the reply with an update block, as in `code-review/SKILL.md` › Report, then fix.
5. **Security, last.** Run the `security` reviewer in the same way, with the `security` role, on the diff after step 4, with the `REVIEW.md` section Security. Its block has `"trigger": "open-pr"`, `"stage": "final"` and `"step": "security"`. Ask the person about each `high` and `ask` finding, then fix and commit as in step 4. When a fix changed code, run the security reviewer once more on the fix diff only (`git diff <the head before the fix>`). Stop after this second round, and list what is still open in the PR.

## 4. Start the record and the push at the same time

- **The record.** Run the digest in a later message than your last block, because Claude Code writes a message to the transcript only when its tool calls end. Run `solidtrace review digest` with one `--session ID` for each confirmed session, `> "$dir/digest.json"`. Then spawn one sub-agent on the `record` role of `${CLAUDE_SKILL_DIR}/../code-review/models.md`. Its prompt is the text of [record.md](record.md), then the two paths: `$dir/digest.json` to read, `$dir/record.json` to write.
- **The push.** Run `git push -u origin HEAD`, and wait for its result. When it fails, stop before step 7: tell the person why, and do not open a PR on an older remote head. In local mode, skip it.

## 5. Check the record

Read `$dir/record.json`. It is complete when:

- It is valid JSON with the fields of record.md.
- Each finding of each `findings` event in the digest has one entry in `findings`, with every event of its block and an `outcome`.
- Each `evidence` value is a `uuid` of an event in the digest.
- `story` has `started_from`, `changes`, `reviews` and `sessions`, and `sessions` are the digest's sessions.

When a check fails, send the sub-agent the failed checks and wait for a new record. After two failed rounds, continue with the record as it is, and say so in the final reply.

## 6. Write the PR

The title is one line that says what the change does. The body has these sections, in this order, and each is short:

- **Summary**: what changes for the user, and why, from the record's `story`.
- **Evidence**: the tests and checks that ran, and their result.
- **Review**: one row per finding found by a reviewer: id, stage and step, reviewer, severity, title, the decisions in order (for example "trivial, verifier pushed back, fixed"), outcome, and what the person said. Then one line per finding found outside the reviews, as "Missed by the review, found by the person" (or "by an agent", "by a test run"). Then the confirmations that were not given.

End the body with the line `<!-- solidtrace-record round 1 -->`, which `update-pr` reads. Write the body to `$dir/body.md`.

## 7. Open the PR

Choose by where the PR goes:

- **Local mode**: write the file `$(git rev-parse --git-common-dir)/solidtrace-pr/<branch>.md`, with each `/` of the branch name changed to `-`. It holds a `# <title>` line, the base and the head (`git rev-parse HEAD`), then the body. Git does not track this folder, and the next local run of the same branch writes over the file.
- **Inside a Run** (`$SOLIDTRACE_AGENT_SOCKET` is set): `solidtrace pr --title "<title>" --description "$(cat "$dir/body.md")"`.
- **github.com**: `gh pr create --base <base> --title "<title>" --body-file "$dir/body.md"`.
- **bitbucket.org**: with `$BITBUCKET_TOKEN` set, send `POST https://api.bitbucket.org/2.0/repositories/<workspace>/<repo>/pullrequests` with `Authorization: Bearer $BITBUCKET_TOKEN` and the JSON `{"title": ..., "description": <body>, "source": {"branch": {"name": <branch>}}, "destination": {"branch": {"name": <base>}}}`. Build the JSON with a tool that escapes text, not by hand. The PR's URL is `links.html.href` in the answer. With no token, give the person `https://bitbucket.org/<workspace>/<repo>/pull-requests/new?source=<branch>&dest=<base>` and the path of `body.md`.

## 8. Final reply

Give the PR's URL, or in local mode the file's path. Then set `pr` in the record to that URL, to `local:<the file's path>` in local mode, or to `null` when no PR opened, and end the reply with the record in one `solidtrace-pr-record` block, as one line of JSON:

````
```solidtrace-pr-record
{"version": 2, ...}
```
````

This block is how SolidTrace keeps the record: the reply uploads with the session.
