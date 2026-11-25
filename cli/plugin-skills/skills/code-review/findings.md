# The solidtrace-findings block

Each review ends its reply with one block. The block records what each reviewer found and what you decided, at the time you decided it. `open-pr` adds what happened later. The JSON must be valid, on one line or several.

````
```solidtrace-findings
{"version": 2, "run": 3, "skill": "code-review", "trigger": "agent", "stage": "step", "step": "otlp-03",
 "base": "<commit>", "head": "<git rev-parse HEAD>",
 "reviewers": [{"name": "bugs-and-spec", "model": "opus", "effort": "xhigh"},
               {"name": "code-quality", "model": "sonnet", "effort": "high"}],
 "verifier": {"model": "opus", "effort": "xhigh"},
 "findings": [
  {"id": "F7", "reviewer": "bugs-and-spec", "kind": "hard", "severity": "high", "category": "security",
   "file": "app/routes.go", "line": 52, "title": "...", "scenario": "...",
   "events": [{"type": "found"}, {"type": "triage", "decision": "fix"}]},
  {"id": "F8", "reviewer": "code-quality", "kind": "judgement", "severity": "low", "category": "design",
   "file": "app/otlp.go", "line": 10, "title": "possible Data Clumps", "scenario": "...",
   "events": [{"type": "found"},
              {"type": "triage", "decision": "trivial", "why": "..."},
              {"type": "verify", "verdict": "CONFIRMED", "pushed_back": true, "why": "..."},
              {"type": "triage", "decision": "fix", "why": "the verifier showed that ..."}]},
  {"id": "F2", "reviewer": "tests", "kind": "hard", "severity": "medium", "category": "test-gap",
   "file": "app/otlp_test.go", "line": null, "title": "...", "scenario": "...",
   "events": [{"type": "refound"}, {"type": "triage", "decision": "fix"}]}],
 "confirmations": [{"id": "C1", "reviewer": "tests", "ask": "run the Go tests of ./handler"}],
 "dropped": {"bugs-and-spec": 0, "code-quality": 1}}
```
````

- `run`: 1 for the first block in this transcript, then 2, 3 and so on. `run` and the ids count only in this transcript. Another session or a subagent starts again at 1, and the record makes them unique.
- `skill`: `code-review`, `deep-review` or `update-pr`. `trigger`: `person` when the person asked for this review, `agent` when you started it, `open-pr` in the final stage, `update-pr` for the security review of PR feedback fixes.
- `stage`: `step`, `final`, or `pr` for the feedback on an open PR. `step`: the step's name. In the final stage, it is `null` for `deep-review`, and the reviewer's name (`ha-and-infrastructure` or `security`) for the others. In the `pr` stage, it is `round <N>` for the feedback, and `security` for its security review.
- `reviewers` is `[]` on an `update-pr` feedback block, where each finding has `"reviewer": "pr-feedback"` and its own `found_by`.
- `reviewers` and `verifier`: the `model` and `effort` that each one ran on, as [models.md](models.md) says. `verifier` is `null` when no finding went to a verifier.
- `skipped`: only on a `deep-review` block that did not run, with `"findings": []`. It says why: `docs only`, `person`, or `covered by <head>`.
- `kind`, `severity` and `category`: as the reviewer gave them (see `reviewers.md`). `line` is `null` when no one line applies.
- `also`: the other reviewers that found the same root cause, when you merged their findings into this one. Leave it out when no other reviewer found it.
- `found_by`: only on an `update-pr` block, where a finding comes from the PR's feedback: `{"kind": "person", "name": "<login>", "access": "write", "ref": "<comment URL or id>"}` (`access` is the author's access to the repository; add `"self": true` when the author is the person who runs the session), `{"kind": "reviewer", "name": "<bot or tool>", "ref": "..."}` for a review bot, or `{"kind": "test", "name": "<check>", "ref": "..."}` for a failed check.
- `events`, in the order they happened:
  - `found`: the reviewer reported it in this run. `refound`: it has the id of a finding from an earlier block.
  - `triage`: your decision, one of those in `SKILL.md` › Decide each finding, with `why` for every decision but `fix`. `update-pr` also gives `tradeoffs`, the trade-offs it showed the person, and `"approved_by": "author"` when the person's own comment was the go-ahead. A second `triage` follows a verifier's push back.
  - `verify`: the verifier's `verdict` (`CONFIRMED`, `PLAUSIBLE` or `REFUTED`), `pushed_back` and `why`.
- `dropped`: for each reviewer, the findings it dropped itself because it had no scenario, and in `deep-review` the findings that no run proved.
- With no findings, `findings` is `[]`.

## The update block

A decision or a fix after the report goes in a new block, not only in your text. End the reply with one update block when, after a block:

- you fixed and committed findings,
- you changed a decision, for example a `trivial` finding that turned out to be real, or
- the person decided an `ask` finding.

````
```solidtrace-findings
{"version": 2, "update": 1,
 "findings": [
  {"id": "F12", "events": [{"type": "triage", "decision": "fix", "why": "the digest of this session showed the input"},
                           {"type": "fix", "commit": "<git rev-parse HEAD>", "files": ["cli/review/digest.go"]}]},
  {"id": "F3", "events": [{"type": "fix", "commit": "<git rev-parse HEAD>", "files": ["app/routes.go"]}]}]}
```
````

- `update`: the `run` of the block that these findings came from, in this transcript. One update block has the findings of one run.
- `events`: only the new events, in the order they happened. `triage` as above. `fix`: `commit` is the full hash of the commit that holds the fix, and `files` are the files of the fix.
- A subagent also puts its update block in its final report.
