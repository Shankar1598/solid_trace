# Integration notification

Status: ready-for-agent

Origin: architecture review of 2026-09-18, candidate #3 ("One Integration notification module for all three triggers"), paired with the windowed-count fix from candidate #2 ("One Event query module"). Test seams were confirmed with the maintainer on 2026-09-19.

## Problem Statement

An Organization connects Slack, PagerDuty or email as Integrations and expects SolidTrace to tell them when something happens to an Issue. Today that promise is unreliable in ways nobody can see:

- **Threshold notifications ("N Events in M minutes") almost never fire correctly.**
  - EventStore's Event count ignores the time window, so the Console compares the Issue's *lifetime* Event count against the threshold.
  - The comparison is exact equality, so the notification fires only if the count lands exactly on the threshold. It stays silent whenever a burst of Events moves the count past it.
- **Failed deliveries are recorded as sent.** When Slack returns a 500, PagerDuty refuses the connection or a request times out, the provider notifier logs the error and returns normally. The outbox then marks the row sent. Nobody is told, and nothing retries.
- **The three kinds of notification behave differently for no reason:**
  - "Issue created" notifications are batched and rate-limited.
  - "Threshold reached" and "assignment changed" notifications are sent immediately, one by one, with no rate limit and no record in the outbox.
  - A noisy Issue can therefore flood a Slack channel, while a new Issue waits in a batch.
- **Assignment notifications misreport archived members.** If the previous assignee has been archived, the message says "From: Unassigned".

For the people maintaining SolidTrace, the rules behind all this are spread across seven modules: two jobs, the notifier, the outbox model, the processor job, the assignment job and three provider notifiers. The rule table is written twice, and the rate-limit window is a separate literal from the debounce window. The retry declarations on the jobs can never fire. The tests stub notifier constructors and the Issue's Event collection instead of exercising behaviour, so none of the bugs above is caught.

## Solution

Integrations get one dependable notification path:

- Every notification, of any kind, goes through the outbox. It is batched and rate-limited per Integration in the same way, and ends in a recorded outcome: delivered, failed (and retried a bounded number of times), or skipped with a reason.
- Threshold notifications fire once when an Issue's Event count within the configured window reaches or passes the threshold. They fire again only after the window has passed.
- A failed Slack, PagerDuty or email hand-off is recorded as failed and retried, never recorded as sent.
- Assignment notifications name archived members correctly.

In the codebase, one Console module, **Integration notification**, owns the rules for telling Integrations about Issues:

- rule evaluation
- the outbox
- batching
- the rate limit
- delivery outcome

Jobs and model callbacks become thin triggers that call it. Tests go through its interface.

In EventStore, the Event count honours the same filters as the Event listing, including the time window, so the Console receives the right number.

## User Stories

1. As an Organization admin, I want a Slack Integration to tell me once when an Issue receives at least N Events within M minutes, so that I learn about spikes without counting Events myself.
2. As an Organization admin, I want the threshold to count only Events inside the configured window, so that an old, long-lived Issue doesn't trigger threshold notifications forever.
3. As an Organization admin, I want a threshold notification to fire even when a burst of Events moves the count past the threshold in one step, so that sudden spikes aren't missed.
4. As an Organization admin, I want a threshold notification to fire at most once per Issue per window, so that a noisy Issue doesn't page me for every Event after the threshold.
5. As an Organization admin, I want a threshold notification to fire again if the Issue is still spiking after the window has passed, so that ongoing incidents aren't silenced forever.
6. As an Organization admin, I want new-Issue notifications batched into one message per Integration, so that a deploy that introduces ten Issues produces one Slack message, not ten.
7. As an Organization admin, I want threshold and assignment notifications to follow the same per-Integration rate limit as new-Issue notifications, so that no kind of notification can flood a channel.
8. As an Organization admin, I want each rule (new Issue, threshold, assignment) evaluated independently, so that enabling one rule never suppresses another.
9. As an Organization admin, I want a disabled rule to produce no notification of that kind, so that the settings I choose are respected.
10. As an Organization admin, I want an inactive Integration to receive nothing, and its pending notifications recorded as skipped, so that pausing an Integration takes effect at once.
11. As an Organization admin, I want notifications for a deleted Integration recorded as skipped rather than sent, so that the outbox reflects what happened.
12. As an on-call engineer, I want a notification whose Slack or PagerDuty delivery failed to be retried automatically, so that a brief provider outage doesn't lose alerts.
13. As an on-call engineer, I want retries to stop after a bounded number of attempts, so that a permanently broken webhook doesn't retry forever.
14. As an Organization admin, I want a failed delivery recorded as failed with the provider's error, so that I (or an operator) can see why an Integration went quiet.
15. As an on-call engineer, I want an HTTP error response (non-2xx) from Slack or PagerDuty treated as a failure, so that a revoked webhook is noticed.
16. As an on-call engineer, I want connection failures and timeouts to Slack or PagerDuty treated as failures, so that network problems are retried.
17. As an Organization admin, I want an Integration with no webhook URL, routing key or recipients to record its notifications as skipped with that reason, so that misconfiguration is visible rather than silent.
18. As an Organization admin, I want an assignment notification to show the previous and new assignee by name even if one of them has since been archived, so that the message is accurate.
19. As an Organization admin, I want an assignment notification only when the assignment rule is enabled on the Integration, so that I can opt out of assignment chatter.
20. As an Organization admin, I want unassigning an Issue to produce a notification that says "To: Unassigned", so that I see the change.
21. As an on-call engineer, I want a new-Issue batch to list at most ten Issues plus a count of the rest and a link to all Issues, so that the message stays readable (today's formatting, kept).
22. As an on-call engineer, I want each notification to link to the Issue in the Console, so that I can act immediately (today's formatting, kept).
23. As an Organization admin, I want a new Issue that also crosses the threshold to produce both a new-Issue notification and a threshold notification when both rules are on, so that each rule does what it says.
24. As an operator, I want delivered and skipped outbox rows cleaned up after a day, and permanently failed rows after a week, so that the outbox doesn't grow without bound but failures stay inspectable for a while.
25. As an operator, I want a temporarily unreachable EventStore to delay the threshold check (retried) rather than count as zero Events, so that an outage doesn't silently suppress threshold notifications.
26. As an operator, I want the EventStore Event count to reject a malformed time bound with an error instead of ignoring it, so that a client bug can't silently turn a windowed count into a lifetime count.
27. As a Console developer, I want to count an Issue's Events within a time window and get the right number, so that any feature built on windowed counts works.
28. As a SolidTrace maintainer, I want one place that defines which notification kinds an Issue change produces, so that a rule change is a one-line change.
29. As a SolidTrace maintainer, I want the rate-limit window and the debounce window to be one setting, so that they can't drift apart.
30. As a SolidTrace maintainer, I want the outbox row's kind to decide the delivered message, so that the stored kind isn't decorative.
31. As a SolidTrace maintainer, I want jobs and callbacks to contain no notification rules, so that reading one module explains notifications end to end.
32. As a SolidTrace maintainer, I want to test notification behaviour by triggering it and observing outbox rows and outbound HTTP or mail, so that tests don't depend on how the module is built inside.
33. As a SolidTrace maintainer, I want provider notifiers to report success or failure instead of swallowing errors, so that the outcome can be recorded.
34. As a SolidTrace maintainer, I want the dead retry declarations and the test-only convenience entry point removed, so that the code doesn't suggest behaviour that doesn't exist.
35. As a future Monitor author (Logs v2), I want the notification module to accept a new kind of notification without changes to the batching, rate-limit or outcome logic, so that Monitors can reuse the Integration pipeline.

## Implementation Decisions

### Console: the Integration notification module

- **One module owns the rules for notifying Integrations about Issues.** It replaces the current notifier's public surface. Its interface has three entry points:
  - **Issue received an Event:** takes the Issue and whether it was newly created. It evaluates the new-Issue and threshold rules for every active Integration of the Issue's Organization and writes outbox rows for the kinds that apply.
  - **Issue assignment changed:** takes the Issue and the previous and new assignee. It evaluates the assignment rule for every active Integration and writes outbox rows.
  - **Deliver pending notifications for an Integration:** the processor tick. It applies the rate limit, delivers what is due, records outcomes and schedules the next tick when work remains.
- **Every kind goes through the outbox:**
  - The three kinds are *issue created*, *threshold reached* and *assignment changed*. All three are written as Notification rows. Writing a row schedules the processor tick for that Integration after the initial delay, as the new-Issue path does today.
  - Nothing is sent synchronously from a trigger.
- **The rule table is defined once, and each rule is evaluated independently:**
  - A newly created Issue with the new-Issue rule on produces an *issue created* row.
  - Any Issue receiving Events with the threshold rule on is checked for *threshold reached*.
  - An assignment change with the assignment rule on produces an *assignment changed* row.
  - One trigger may produce several kinds.
- **The threshold rule:**
  - It fires when the Issue's Event count within the last `time_window_minutes` is **greater than or equal to** `event_threshold`.
  - There must also be no *threshold reached* row for the same Issue and Integration created within that same window. The outbox itself is the dedup record.
  - This replaces the exact-equality check.
- **Windowed count failures are errors, not zero:**
  - The threshold check's call to EventStore must tell "EventStore unavailable" apart from a count. An unavailable EventStore raises. The trigger job retries with bounded backoff, then gives up with a logged error.
  - Other callers of the EventStore adapter keep today's swallow-to-default behaviour. Changing that is a separate candidate (Issue events read port).
- **Batching at delivery:**
  - Pending *issue created* rows for an Integration are delivered as one batch message, as today.
  - *Threshold reached* and *assignment changed* rows are delivered as one message per row in the same tick.
  - The whole tick counts once against the rate limit.
- **Rate limit:**
  - At most one delivery tick per Integration per debounce window.
  - The rate-limit window and the debounce window are the same constant.
  - A tick that arrives inside the window reschedules itself for when the window opens, as today.
- **Delivery outcome:**
  - Provider adapters (Slack, PagerDuty, email) report success or failure instead of rescuing errors to nil.
  - For Slack and PagerDuty, a non-2xx response, a connection error or a timeout is a failure.
  - For email, a successful hand-off to the mailer queue is a success; mail delivery retries stay ActionMailer's concern.
  - The module records each row's outcome from the adapter's report.
- **Outcome states on the outbox row:**
  - Existing states: `pending`, `processing`, `sent`, `failed`.
  - New state: **`skipped`**, with a reason. It covers an Integration that is missing or inactive, an Issue that is missing, and a provider that is not configured (no webhook URL, routing key or recipients).
  - Skipped rows are never retried and are no longer recorded as `sent`.
- **Bounded retries:**
  - A new `attempts` count on the Notification row is incremented on each delivery attempt.
  - Failed rows are picked up again by later ticks until they reach an attempt cap of 5. At the cap they stay `failed` and are no longer picked up.
  - A failed batch message marks every row in the batch as failed.
- **Assignment payload:**
  - The outbox row stores the previous and new assignee ids.
  - Names are resolved at delivery time, including archived (discarded) Organization members. A nil id renders as "Unassigned".
- **Kind naming:** the Notification row's `event_type` column is renamed to **`kind`**. "Event" is a glossary term for a Sentry payload, and the column holds a notification kind. The stored kind decides the message the provider adapter formats. The hard-coded batch event name goes away.
- **Provider adapters format; the module decides:**
  - Adapters receive a notification kind plus the Issue or Issues and the assignee names, and format a provider payload.
  - They make no rule decisions.
  - The duplicated HTTP transport and URL helpers in the Slack and PagerDuty adapters are merged into one internal helper, which is private to the adapters.
- **Triggers become thin:**
  - The EventStore message processor keeps enqueuing the per-Issue trigger job, which calls *Issue received an Event*. It stays a job because it makes an HTTP call to EventStore.
  - The Issue model's assignment callback calls *Issue assignment changed* directly, since it only writes outbox rows. The dedicated assignment job is deleted.
  - The processor job calls *Deliver pending*.
  - The retry declarations for network timeouts, which can never fire, are removed from the jobs.
  - The test-only `notify(issue)` convenience entry point is removed.
- **Outbox cleanup:** the maintenance job deletes `sent` and `skipped` rows older than one day, and `failed` rows at the attempt cap older than seven days.
- **Schema changes (Console):**
  - rename `notifications.event_type` to `kind`
  - add `notifications.attempts` (integer, default 0, not null)
  - add the `skipped` status value
  - existing indexes on (integration, kind, status) and (status, created_at) are kept, with the column renamed
- **Not changed:** Integration settings keys, their defaults and their accessors. Today's Slack, PagerDuty and email message formatting stays, apart from the "Unassigned" fix for archived members.

### EventStore: windowed Event count

- The Event count honours the same filters as the Event listing:
  - Project
  - Issue fingerprints
  - `newer_than`
  - `older_than`
  - tags
- **One predicate builder shared by the listing and the count:** it is used by the listing and the count. The Event-with-context query adopts it for the filters it supports, so the filters can't diverge again.
- A malformed `newer_than` or `older_than` on the Event query routes returns HTTP 400 with an error, instead of being silently ignored.
- The HTTP contract of `/api/:project_id/events/count` is otherwise unchanged: same parameters, same `{"count": n}` response.

## Testing Decisions

- **A good test here** triggers behaviour through a module's interface and observes only what the outside world sees:
  - outbox rows and their states
  - outbound HTTP requests to Slack, PagerDuty and EventStore
  - mail handed to ActionMailer
  - jobs scheduled

  Tests must not stub constructors (`SomeNotifier.stub :new`), stub the Issue's Event collection, or assert on private methods.
- **Primary test surface: the Integration notification module's interface.** The three entry points are the only way tests drive notification behaviour. External systems are faked only at existing process edges:
  - WebMock for Slack, PagerDuty and the EventStore count HTTP call
  - ActionMailer test deliveries for email
  - the ActiveJob test adapter plus `travel_to` for scheduling, the initial delay, debounce and rate limit

  No new seam (such as an in-memory provider adapter) is introduced.
- **Behaviours to cover at that surface:**
  - each rule, on and off
  - threshold hit by exact count and by overshoot
  - threshold dedup within the window and re-fire after it
  - EventStore unavailable leading to a retried error rather than no notification
  - new-Issue batching and the ten-Issue cap
  - threshold and assignment rows delivered per row in the same tick
  - rate limit, and rescheduling inside the window
  - Slack 500, connection refused and timeout each recorded as `failed`, then retried up to the cap
  - missing or inactive Integration, missing Issue and unconfigured provider recorded as `skipped`
  - assignment with an archived previous assignee
  - unassignment
  - cleanup retention per state
- **Provider adapter tests** keep their current shape: WebMock payload assertions per provider and kind. They gain success and failure reporting cases (2xx, non-2xx, connection error, timeout).
- **Tests replaced:**
  - The current notifier tests and the processor and assignment job tests are superseded by tests at the module's interface.
  - Job-level tests shrink to "the trigger calls the module", or are dropped where the job is deleted.
- **EventStore:** the count is tested at the HTTP route the Console calls (`/api/:project_id/events/count`), using an in-process Fiber app with a real DuckDB in a temporary directory. The route is tested, not the storage function beneath it. Cases:
  - window filtering on both bounds
  - window combined with fingerprints
  - malformed time bound returns 400
  - a regression case showing the lifetime count is no longer returned for a windowed request
- **Prior art:**
  - Console:
    - the processor job test for `travel_to` and enqueued-job assertions
    - the Slack and PagerDuty notifier tests for WebMock request stubbing
    - the EventStore adapter test for stubbing EventStore HTTP
    - the Notification model test for outbox rows
  - EventStore:
    - the DuckDB storage test for a real DuckDB in a temporary directory
    - the integration test for building the Fiber app with routes

## Out of Scope

- The other architecture-review candidates:
  - the Event pipeline (#1)
  - the rest of the Event query consolidation beyond count and shared predicates, such as moving prev/next and payload fetch out of the handler (#2)
  - the Issue events read port with explicit error modes for all Console reads (#4)
  - the Integration settings definition, including the disagreeing defaults between the model and the New and Edit pages (#5)
  - Issue and membership commands (#6)
  - `IssueRepository` (#7)
  - tier storage (#8)
  - the ingest front door (#9)
- An in-memory provider adapter or a typed provider message port.
- New providers, PagerDuty resolve or acknowledge events, and per-Integration custom message templates.
- Changing which Organization members or Projects an Integration covers.
- Surfacing failed notifications in the Console UI. The outbox records them; a UI is a later feature.
- Authentication on EventStore's read routes (`.scratch/prod-readiness/issues/002`). When it lands, the windowed count call must carry the same credentials as other EventStore reads.
- Monitors (Logs v2).

## Further Notes

- **Monitors (Logs v2):** the Logs map (`.scratch/logs/map.md`) lists "Monitors, reusing the existing Integrations and notification pipeline". This module is that pipeline. Keeping kinds open (story 35) means a *monitor triggered* kind can be added later without touching batching, rate limit or outcome handling. Nothing in this spec decides anything about Monitors.
- **Glossary gap:** `CONTEXT.md` defines **Integration** but not the outbox row (**Notification**) or the module (**Integration notification**). Add both via `domain-modeling` when implementation settles the names. Note that "notification kind" deliberately avoids the word "event".
- **Settings default mismatch:** the model treats a missing threshold setting as *on*, while the New and Edit pages treat it as *off*. This means the fixed threshold rule will start firing for Integrations whose admins believe it is off. That default belongs to candidate #5, but consider fixing it first, or at least checking stored settings before deploying this change.
- **Delivery timing:** threshold and assignment notifications move from immediate to outbox delivery. They gain the initial delay (10 seconds) and share the per-Integration rate limit. This is intended (stories 6–7).
