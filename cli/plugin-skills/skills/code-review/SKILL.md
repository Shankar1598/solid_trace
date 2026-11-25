---
name: code-review
description: Review one step of a feature with focused reviewers (bugs and spec, tests, code quality), have a verifier check each finding you reject, and record every decision in a solidtrace-findings block. Use after each step of a build, and when the person asks for a code review.
---

# Code review

One step is the change since the last review in this session, or since the point the person names. You coordinate the review in this session: spawn the reviewers, decide each finding, send the findings you reject to a verifier, and write the block. The block is data that the team analyses later, so it records each decision at the time you make it.

Security, high availability and infrastructure are not reviewed here. They run once, on the final diff, when `open-pr` opens the PR.

## 1. Pin the step

- **Commit first.** When the step's files have uncommitted changes, commit them with explicit paths. Then `head` names exactly the code that was reviewed.
- **Base**: the commit that the person names. Else the `head` of the last `solidtrace-findings` block in this session. Else the commit where the step began, when you built the step. Else ask the person, and suggest the merge base with the base branch.
- Run `git diff <base>..HEAD --stat` and `git log <base>..HEAD --oneline`. The diff is `git diff <base>..HEAD`. An empty diff ends the review here.
- **Step**: a short name for the step, such as the ticket or the part (`otlp-03`).

Done when you hold the base, `git rev-parse HEAD`, the diff command, the changed files and the step name.

## 2. Find the spec and the rules

- **Spec**: the ticket or issue that the step builds, from the person's words, the commit messages or the repository's issue tracker. Read its full text. With none, the bugs reviewer reviews without one.
- **Rules**: the `##` sections of `REVIEW.md` at the repository root, when it exists. Also the coding standards file that `CLAUDE.md` or `AGENTS.md` names. Give each section to a reviewer by its heading:
  - Tests goes to `tests`.
  - Coding standards and comments go to `code-quality`.
  - Security, High availability and Infrastructure are for the final stage. Do not use them here.
  - Every other section goes to `bugs-and-spec`.

Done when you hold the spec's text (or know there is none) and each rule section has its reviewer.

## 3. Run the reviewers

Spawn these in one message, so they run at the same time:

| Reviewer | Runs when |
|---|---|
| `bugs-and-spec` | always |
| `tests` | the step changes code or tests |
| `code-quality` | the step changes code |

Take each reviewer's `model` and `effort` from [models.md](models.md).

Each prompt gives the diff command, the commit list, the changed files, the rule sections mapped to that reviewer, and this line: "Read `${CLAUDE_SKILL_DIR}/reviewers.md`, follow its contract and the section for `<reviewer>`." Add the spec's full text for `bugs-and-spec`.

Done when every reviewer has returned.

## 4. Decide each finding

Number the findings `F1`, `F2` and on, after the highest id of the earlier blocks in this session. When a finding has the same root cause as an earlier one, keep the earlier id and add a `refound` event.

Give each finding one decision, with a one-line `why` for every decision but `fix`:

- `fix`: it is real, and you fix it in this step.
- `dispute`: it is not a real problem.
- `trivial`: it is real, and too small to fix.
- `defer`: it is real, and it belongs to a later step or ticket.
- `ask`: only the person can decide.

Done when every finding has one decision. A decision that changes later goes in an update block ([findings.md](findings.md), The update block).

## 5. Verify what you reject

Send every `dispute` and `trivial` finding to a verifier. Verifiers run on the `verifier` role of [models.md](models.md). Spawn one verifier for up to 10 findings, and one more for each 10 above that, all in one message. Each prompt gives the diff command, the findings as the reviewers wrote them, your decision and `why` for each, the spec's text, and this line: "Read `${CLAUDE_SKILL_DIR}/reviewers.md` and follow the section for the verifier."

When the verifier pushes back, decide again: `fix` or `ask`. Do not dispute a finding that the verifier pushed back on.

Done when every `dispute` and `trivial` finding has a verdict.

## 6. Report, then fix

Write the findings for the person, one section per reviewer, most severe first: the id, the file and line, the scenario, your decision and the verifier's verdict. Then the confirmations that the reviewers ask the developer for.

End the reply with one block in the format of [findings.md](findings.md). When you run as a subagent, also put the block in your final report, because only that report reaches the session that started you. Then fix the findings with decision `fix`, unless the person asked for a report only.

Commit the fixes with explicit paths. End the commit message with one trailer line for each run whose findings it fixes, before the other trailers:

```
Review-Findings: run 1: F2, F5, F12
```

`run` and the ids are those of the block. Then end the reply with an update block that gives each fixed finding a `fix` event with the commit's hash.

Done when each finding with decision `fix` has a `fix` event in an update block.
