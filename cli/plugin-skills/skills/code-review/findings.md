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
- `skill`: `code-review` or `deep-review`. `trigger`: `person` when the person asked for this review, `agent` when you started it, `open-pr` in the final stage.
- `stage`: `step` or `final`. `step`: the step's name. In the final stage, it is `null` for `deep-review`, and the reviewer's name (`ha-and-infrastructure` or `security`) for the others.
- `reviewers` and `verifier`: the `model` and `effort` that each one ran on. `model` is the alias: `opus`, `sonnet`, `haiku` or `fable`. For one with no `model`, write the alias of this session's model. `effort` is the `effort` that the spawn passed, or `default` when it passed none. `verifier` is `null` when no finding went to a verifier.
- `skipped`: only on a `deep-review` block that did not run, with `"findings": []`. It says why: `docs only`, `person`, or `covered by <head>`.
- `kind`, `severity` and `category`: as the reviewer gave them (see `reviewers.md`). `line` is `null` when no one line applies.
- `events`, in the order they happened:
  - `found`: the reviewer reported it in this run. `refound`: it has the id of a finding from an earlier block.
  - `triage`: your decision, one of those in `SKILL.md` › Decide each finding, with `why` for every decision but `fix`. A second `triage` follows a verifier's push back.
  - `verify`: the verifier's `verdict` (`CONFIRMED`, `PLAUSIBLE` or `REFUTED`), `pushed_back` and `why`.
- `dropped`: for each reviewer, the findings it dropped itself because it had no scenario, and in `deep-review` the findings that no run proved.
- With no findings, `findings` is `[]`.
