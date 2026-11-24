---
name: open-pr
description: Push this branch and open its PR, with a record of what each review found and what happened to each finding.
disable-model-invocation: true
---

# Open PR

The person typed this command, so push the branch and open the PR without asking again. The PR body is for the human reviewer. The record is data for the team that improves the review skill.

## 1. Check the branch

- The branch is not the base branch. The base branch is the one the person names, else the remote's default branch (`git remote show origin`).
- The working tree is clean. If it is not, list the changed files and ask the person whether to commit them. Continue when the tree is clean.

Make a folder for this PR's files: `dir=$(mktemp -d)`.

## 2. Find the sessions of this feature

A feature often spans several sessions. Run `solidtrace review sessions --base <base> --session ${CLAUDE_SESSION_ID} > "$dir/sessions.json"`.

- **Other sessions are listed.** Show the person a numbered list: the title, the dates, and the links (for example "3 of the PR's commits, 7 of its files"). The current session and each `strong` session are selected. Ask the person in one message which sessions to add or remove. Continue with the sessions that the person confirms.
- **Only the current session is listed.** Run the command again with `--all --days 3`. Spawn one sub-agent with `model: "haiku"`, with the PR's commit subjects and the listed titles and dates, to name the sessions that are about the same work. When it names some, ask the person as above. When it names none, continue with the current session and ask nothing.

## 3. Start the record and the push at the same time

- **The record.** Run `solidtrace review digest` with one `--session ID` for each confirmed session, `> "$dir/digest.json"`. Then spawn one sub-agent with `model: "sonnet"`. Its prompt is the text of [record.md](record.md), then the two paths: `$dir/digest.json` to read, `$dir/record.json` to write.
- **The push.** Run `git push -u origin HEAD`.

When the digest says `"reviews": 0`, the sub-agent still writes a record with no reviews.

## 4. Check the record

Read `$dir/record.json`. It is complete when:

- It is valid JSON with the fields of record.md.
- Each finding of each `findings` event in the digest has one entry, with an `outcome`.
- Each `evidence` value is a `uuid` of an event in the digest.

When a check fails, send the sub-agent the failed checks and wait for a new record. After two failed rounds, continue with the record as it is, and say so in the final reply.

## 5. Write the PR

The title is one line that says what the change does. The body has these sections, in this order, and each is short:

- **Summary**: what changes for the user, and why, from the record's `story`.
- **Evidence**: the tests and checks that ran, and their result.
- **Review**: one row per finding, from the record: id, severity, title, outcome, and what the person said. Then one line per item of `missed`, as "Missed by the review, found by the person" or "found by an agent".

Write the body to `$dir/body.md`.

## 6. Open the PR

Choose by where the PR goes:

- **Inside a Run** (`$SOLIDTRACE_AGENT_SOCKET` is set): `solidtrace pr --title "<title>" --description "$(cat "$dir/body.md")"`.
- **github.com**: `gh pr create --base <base> --title "<title>" --body-file "$dir/body.md"`.
- **bitbucket.org**: with `$BITBUCKET_TOKEN` set, send `POST https://api.bitbucket.org/2.0/repositories/<workspace>/<repo>/pullrequests` with `Authorization: Bearer $BITBUCKET_TOKEN` and the JSON `{"title": ..., "description": <body>, "source": {"branch": {"name": <branch>}}, "destination": {"branch": {"name": <base>}}}`. Build the JSON with a tool that escapes text, not by hand. The PR's URL is `links.html.href` in the answer. With no token, give the person `https://bitbucket.org/<workspace>/<repo>/pull-requests/new?source=<branch>&dest=<base>` and the path of `body.md`.

## 7. Final reply

Give the PR's URL. Then set `pr` in the record to that URL (or `null` when no PR opened), and end the reply with the record in one `solidtrace-pr-record` block, as one line of JSON:

````
```solidtrace-pr-record
{"version": 1, ...}
```
````

This block is how SolidTrace keeps the record: the reply uploads with the session.
