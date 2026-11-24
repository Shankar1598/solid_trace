# The review record

You read the digest of one coding session and write the record of its reviews: what each review found, what happened to each finding, and what the person said. The team uses the records of many sessions to improve its review skill, so each claim must point at the event that proves it.

## The digest

A JSON file with `session`, `reviews` (the count of review runs) and `events`, in the order of the session. Each event has `seq`, `uuid`, `time` and `kind`:

- `task`: the person's first message.
- `review_start`: a review began. `skill`, `trigger` (`person` typed it, or the `agent` started it), `args`.
- `agent_text`: a text that the agent wrote.
- `findings`: a `solidtrace-findings` block with each finding's `id`, `severity`, `category`, `file`, `line`, `title`, `proof` and `needs`.
- `delegate`: the agent gave work to a subagent. The text is the subagent's description and prompt.
- `subagent_report`: a subagent's report. A review often runs in subagents, so their reports can hold findings.
- `person`: a message from the person.
- `edit`: a file that the agent changed (`tool`, `file`).
- `commit`: a `git commit` command.

## Steps

1. **Find each review run.** Each `review_start` begins one run. A run's findings are in its `findings` event. When a run has no `findings` event, take its findings from the `subagent_report` and `agent_text` events that report that review: give them the ids `T<run>-1`, `T<run>-2` and so on, and set `"source": "text"`. A subagent can also run a review that has no `review_start`, such as a review of a later part of the work. Each such review is one more run, with `"trigger": "subagent"` and `"start"` set to the uuid of its first `delegate`. One run is one review of one part of the work: the reviewers that review the same part at the same time (for example a standards reviewer and a spec reviewer) are one run, not one run each. A subagent that only verifies or tests the work is not a review.
2. **Decide the outcome of each finding.** Read the events after the run's report, up to the PR. Use the first outcome in this list that the events prove:
   - `fixed`: an `edit` changes the finding's file after the report, or a `delegate` gives the fix to a subagent and its `subagent_report` or a `commit` says that it is done, and a later run does not report it again. Evidence: the edit, or the delegate and its report, and the person message that asked for it, if one did.
   - `ignored_by_person`: the person said to skip, ignore or leave it. Evidence: that message.
   - `disputed`: the person or the agent said that it is not a real problem. Evidence: that message or report.
   - `deferred`: the person or the agent said to do it later, or in another change or ticket. Evidence: that message.
   - `ignored_trivial`: the agent said that it is too small to fix. Evidence: the event where the agent said so.
   - `open`: no event shows what happened to it. Evidence: the report.
3. **Quote the person.** `person_said` is the person's own words about this finding, at most 200 characters, cut with `…`. Use `null` when the person said nothing about it.
4. **Find what the review missed.** Go through every `person` event after the first review, one at a time, and every `subagent_report` of a verifier or a test run. For each one, ask: does it name a problem, or ask a question whose answer shows a problem (for example "Have you tested this?", answered "Partly")? A problem that no finding of the review before it names is a `missed` item. `found_by` is `person` when a person message names it, or asks a question that makes the agent find it (also a person who reads deeper and finds more). It is `agent` for a verifier, a test run or a later subagent. Evidence: the person message or the agent's event, and the event where the agent agrees that the problem is real.
5. **Mark doubt.** When the events let two outcomes fit, choose the one with the stronger evidence and set `"confidence": "low"`. Else `"high"`.

Every `evidence` value is a `uuid` from the digest. Write no claim that no event shows.

## The record

Write this JSON to the output path:

```json
{
  "version": 1,
  "session": "<from the digest>",
  "pr": null,
  "reviews": [
    {
      "run": 1, "skill": "pstack:interrogate", "trigger": "person", "start": "<uuid of the review_start or the delegate>",
      "findings": [
        {"id": "F1", "source": "block", "severity": "high", "category": "correctness", "file": "app/x.rb",
         "title": "...", "outcome": "fixed", "person_said": "...", "evidence": ["<uuid>"], "confidence": "high"}
      ]
    }
  ],
  "missed": [
    {"after_run": 1, "found_by": "person", "summary": "...", "file": "app/y.rb", "evidence": ["<uuid>"]}
  ]
}
```

For a finding from text, `severity`, `category` and `file` are `null` when the report does not give them. Then reply with one line: the path you wrote, the count of runs, the count of findings by outcome, and the count of `missed` items by `found_by`.
