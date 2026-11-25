---
name: deep-review
description: Try to break the whole branch with several models, keep only the findings that a run proves, and record each decision in a solidtrace-findings block.
disable-model-invocation: true
---

# Deep review

Several adversaries try to break the change. A finding counts only when a run proves it. `open-pr` runs this once for each PR, in its final stage. The person can also type it at any time.

## 1. Frame

The scope is what the person names. Else it is the branch diff from its merge base with the base branch, through the working tree. Write the intent in two or three sentences, from the person's messages, the commits and the PR description. Ask the person when the intent is not clear. Note the base and `git rev-parse HEAD`.

## 2. Break it

Spawn one adversary for each entry of the `deep-review` role in `${CLAUDE_SKILL_DIR}/../code-review/models.md`, all in one message. Give each `isolation: "worktree"`, so that its repros do not touch your checkout. A new worktree can start from another commit, so each prompt starts with `git checkout --detach <HEAD from step 1>`. Copy the scope's uncommitted changes into each worktree. Give each the scope, the intent, and this brief:

> Break this change. Prove each finding with a run against the real code: a failing test, a repro script, or command output that shows the wrong behavior. Keep repros in your own worktree, and do not commit or push. Report each finding with a title, the file and line, the exact command, the output it printed, and the output that the intent needs. Report only what you ran. That leaves out style, naming, structure, and risks that need a future change to matter. No findings is a valid result. Then give the count of things you tried and dropped.

Run each finding's command again yourself, on your own build of HEAD. Keep a finding when its output shows a failure that breaks the intent. Merge findings that have the same root cause: the first adversary that found it is `reviewer`, and the others go in `also`. Name the adversaries `adversary-1`, `adversary-2` and on, in the order of the role's list.

Done when every adversary has returned, and every kept finding failed again in your own run.

## 3. Decide each finding

Number and decide the findings as `code-review` does (`${CLAUDE_SKILL_DIR}/../code-review/SKILL.md`, the step "Decide each finding"). A run proved each finding, so the decisions are `fix`, `defer` or `ask`. A finding that you think is not a problem is `ask`, with your `why`: only the person can overrule a run.

## 4. Report

Write the findings for the person, most severe first: the id, the title, the command and its output, how it breaks the intent, and your decision. End the reply with one block in the format of `${CLAUDE_SKILL_DIR}/../code-review/findings.md`, with `"skill": "deep-review"`, `"stage": "final"`, `"step": null` and `"verifier": null`. `trigger` is `person` when the person typed this skill, and `open-pr` when `open-pr` runs it. The `scenario` of each finding is the command, the output it printed, and the output that the intent needs. `kind` is `hard`.

When the person typed this skill, stop after the block: the person decides the `ask` findings before any fix. When `open-pr` runs it, go on with the next step of `open-pr`.
