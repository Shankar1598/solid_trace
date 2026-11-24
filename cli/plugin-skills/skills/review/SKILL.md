---
name: review
description: Review this branch's change, and record each finding as data that the open-pr skill tracks.
disable-model-invocation: true
---

# Review

Reviewers try to break the change. A finding counts only when a run proves it. Report the findings, and do not fix them: the person decides what happens to each.

## 1. Frame

The scope is what the person names. Otherwise it is the branch's diff from its merge base with the base branch, through the working tree. Write the intent in two or three sentences, from the person's messages, the commits and the PR description. Ask the person when the intent is not clear.

## 2. Review

Find pstack's interrogate skill:

```bash
ls -d ~/.claude/plugins/cache/*/pstack/*/skills/interrogate 2>/dev/null | sort -V | tail -1
```

- **pstack is installed.** Read `SKILL.md` in that folder and follow it, with the scope and the intent from step 1. Its verified findings are the findings of this review.
- **pstack is not installed.** Spawn two reviewer sub-agents at the same time: one with `model: "opus"`, and one with no `model`. Give each the scope, the intent, and this brief:

  > Break this change. Prove each finding with a run against the real code: a failing test, a repro script, or command output that shows the wrong behavior. Report each finding with a title, the file and line, the exact command, the output it printed, and the output that the intent needs. Report only what you ran. No findings is a valid result.

  Run each finding's command again yourself. Keep a finding when its output shows a failure that breaks the intent. Merge findings that have the same root cause.

## 3. Number the findings

Give each finding an id: `F1`, `F2`, and so on. When an earlier review in this session found the same root cause, use its id again. A new finding gets the next free number. Set its severity:

- `high`: it breaks the intent for a user, or loses or exposes data.
- `medium`: it breaks the intent in a case that a user can reach, but not often.
- `low`: it is real, and the harm is small.

## 4. Report

Write the findings for the person, most severe first: the id, the title, the proof, and how it breaks the intent. Then end the reply with one `solidtrace-findings` block. The block is the record that the open-pr skill reads, so its JSON must be valid and must hold every verified finding of this run:

````
```solidtrace-findings
{"version": 1, "run": 1, "reviewer": "pstack:interrogate", "intent": "...",
 "findings": [
  {"id": "F1", "severity": "high", "category": "correctness", "file": "app/x.rb", "line": 42,
   "title": "...", "proof": "the command, and the output that it printed", "needs": "the output that the intent needs"}
 ],
 "dropped": 3}
```
````

- `run` is 1 for the first review in this session, then 2, 3 and so on.
- `reviewer` is `pstack:interrogate` or `review`.
- `category` is one of `correctness`, `security`, `performance`, `data-loss`, `test-gap`, `design` or `other`.
- `line` is `null` when no one line applies.
- `dropped` is the count of findings that no run proved.
- With no verified findings, `findings` is `[]`.

Stop after the block. Ask nothing about the fixes in this reply.
