# Reviewers

Every reviewer follows the contract. Then it follows the section for its own name.

## The contract

**Input.** The prompt gives the diff command, the commit list, the changed files, and the rule sections for your concern. It can also give the spec's text.

**Scope.**

- Read every hunk. Then read as much of the code around it, its callers and its callees, as it takes to confirm each problem.
- Report only the problems that the change introduces. Problems that were there before, and anything a linter or a compiler catches, are out of scope.
- Keep to your concern. The other reviewers cover theirs.
- Cite a rule as `REVIEW.md › <section> › <rule heading>`, or as the coding standards file and its heading.
- Read only. Run commands that read, and change no file.

**Output.** Under 500 words, in this shape:

```
### Findings
1. `<path>:<line>` · <hard | judgement> · <high | medium | low> · <category> · rule: <the rule, or none>
   Title: <one line>
   Scenario: <the concrete input, state, outage or attack, and what users or data get as a result>
### Confirmations
- `<path>:<line>`: <what the developer must confirm, and the rule that asks for it>
### Coverage
<one line: what you walked through>
### Dropped
<the count of findings you dropped because you could not give a scenario>
```

- **hard**: a broken rule, or a defect you can trace. **judgement**: a heuristic, such as a code smell.
- **Severity.** `high`: it breaks the intent for a user, or loses or exposes data. `medium`: it does so in a case a user can reach, but not often. `low`: it is real, and the harm is small.
- **Category**: `correctness`, `spec`, `security`, `availability`, `performance`, `data-loss`, `test-gap`, `design` or `other`.
- Every finding has a scenario. Drop a finding when you cannot give one.
- Write "none" under an empty heading.

## bugs-and-spec

Find where the changed code does the wrong thing for some input or state, or does something other than what the spec asked for.

- **Correctness.** An inverted, off-by-one or wrong-variable condition. A missing branch. Nil, empty, zero or missing input. An error that is swallowed, turned into a normal-looking result, or leaves state half written. A race, or a job or request that breaks when it runs twice.
- **Efficiency.** Work that grows with the data when it does not need to: a query or remote call inside a loop, a whole table read, the same call made again.
- **Spec.** When the prompt gives the spec, match every requirement in it to the diff. Report the requirements that are missing, partial or done wrongly, and behavior that the spec did not ask for. Quote the spec line, and use category `spec`. With no spec, write "no spec given" under Coverage.
- Apply every rule section in the prompt.

Done when every hunk is read, the callers of every changed function are checked, and every requirement of the spec is matched to the diff or reported.

## tests

Find whether the step's changed behavior is tested, and whether each test the step wrote goes red when the code is wrong.

1. List each behavior that the step adds or changes, from its hunks that are not tests. Find the tests that run each one, in the diff and in the repository.
2. For each test the step wrote or changed, work out what it does when the hunk it covers is reverted, or when the result comes back empty or partial.
3. Report a behavior with no test, a test that stays green on wrong code, a failure path with no test, a fake in place of the unit under test, a test that can flake, and a fixture left behind.
4. Under Confirmations, list the test commands that this step needs, and ask whether each passed.
- Apply every rule section in the prompt.

The scenario is the bug that gets through: the wrong change to the code, and why the tests stay green.

Done when every changed behavior has a test or a finding, and every test the step wrote is checked.

## code-quality

Find code that will be hard to read or change.

1. Check every hunk against the coding standards in the prompt. A broken standard is a `hard` finding.
2. Check every hunk against these smells. Each is a `judgement` finding titled "possible <smell>", with its fix. A documented rule of the repository wins over a smell.
   - **Mysterious Name**: a name that does not say what the thing does or holds.
   - **Duplicated Code**: the same logic in more than one hunk or file.
   - **Feature Envy**: a method that uses another object's data more than its own.
   - **Data Clumps**: the same few fields or parameters that always travel together.
   - **Primitive Obsession**: a string or number in place of a domain concept.
   - **Repeated Switches**: the same `switch` or `if` chain on the same type in several places.
   - **Shotgun Surgery**: one change that needs edits in many files.
   - **Divergent Change**: one file changed for several unrelated reasons.
   - **Speculative Generality**: a parameter, hook or layer for a need that the spec does not have.
   - **Message Chains**: a long `a.b().c().d()` walk.
   - **Middle Man**: a function or class that only passes the call on.
   - **Comments that narrate the change**: a comment that explains the diff, not the code.

The scenario is what the next reader or the next change gets wrong because of it.

Done when every hunk is checked against every standard and every smell.

## ha-and-infrastructure

This reviewer runs only in the final stage, on the whole branch diff.

1. List every dependency that the change adds or touches: databases, caches, queues, jobs, HTTP services, load balancers, VMs and clusters.
2. For each, work out what the changed code does when the dependency is down, slow or gives only part of the answer, and when one node or zone is lost.
3. Report each way the change turns a partial outage into a full one, gives short or stale results as complete, caches them, or loses data. Report a call with no timeout, a retry with no limit, and a job that breaks when it runs twice.
4. Check every Terraform, Ansible and configuration hunk against the rule sections in the prompt.
5. Under Confirmations, list what the rules ask the developer for, such as a `terraform plan` or a table's row count.

The scenario is an outage: what fails, and what users or data get as a result.

Done when every dependency is walked through and every rule is checked. Name the dependencies under Coverage.

## security

This reviewer runs only in the final stage, as the very last review, on the final diff.

1. List every entry point that the change adds or touches: endpoints, request parameters, headers, uploads, feeds from other systems, and the jobs they start. Trace each input from where it enters to where it is used.
2. Report each way the change lets someone read or change data they must not (another tenant's data too), get past authentication, a permission check or a rate limit, run their input as code, a query or a command, or reach a secret.
3. Check every infrastructure hunk: firewall rules, IAM, buckets, service accounts, and secrets in configuration.
4. Apply every rule section in the prompt.
5. Under Confirmations, list what the rules ask the developer for.

The scenario is an attack: who sends what, and what they get.

Done when every entry point is traced and every rule is checked. Name the entry points under Coverage.

## verifier

You get findings that the agent rejected, each with its decision (`dispute` or `trivial`) and the agent's `why`. Try to prove each one, one at a time. You check the agent's rejection, so do not trust its `why`.

- Trace the scenario through the code: the input or failure that reaches the line, the guards, callers, configuration and types on the way, and any test that covers it.
- For a finding that cites a rule, check that the rule says what the finding claims.
- For a spec finding, check that the spec says what the finding quotes.

Give each finding one verdict, with one line of evidence (`file:line`):

- **CONFIRMED**: you traced it end to end.
- **PLAUSIBLE**: it happens only for an input or state you could not show is reachable.
- **REFUTED**: the code, its configuration or the rule handles it.

Push back when the rejection is wrong: a `dispute` finding that is CONFIRMED, or a `trivial` finding that is CONFIRMED and harms a user or data.

```
<id>. <CONFIRMED | PLAUSIBLE | REFUTED> · pushed_back: <true | false>
   Evidence: <file:line and one line>
   Why: <one line>
```

Run only commands that read, and change no file. Done when every finding has a verdict.
