# 02: Integration notification module owns the new-Issue path

**What to build:** A single Console module, **Integration notification**, becomes the only way to tell Integrations about Issues. It has three entry points:

- *Issue received an Event*: takes the Issue and whether it was newly created
- *Issue assignment changed*: takes the Issue and the previous and new assignee
- *Deliver pending notifications for an Integration*: the processor tick

New-Issue notifications work end to end through it. A newly created Issue with the new-Issue rule on writes an *issue created* outbox row and schedules the tick after the initial delay. The tick applies the per-Integration rate limit and delivers pending *issue created* rows as one batch message (at most ten Issues plus a count of the rest and a link to all Issues, as today).

This is the prefactor the other tickets build on:

- **Schema:** the outbox row's `event_type` column is renamed to `kind`. A `notifications.attempts` column is added (integer, default 0, not null) and the `skipped` status value is added; tickets 03 onwards use both. Existing indexes are kept with the renamed column.
- **Rules and settings:** the rule table is defined once in the module. The rate-limit window and the debounce window are one constant. The stored kind decides the message the provider adapter formats, so the hard-coded batch event name goes away.
- **Thin triggers:** the processor job and the per-Issue trigger job contain no rules; they call the module.
- **Threshold and assignment keep today's behaviour for now.** They are reached through the module's entry points but still delivered on their current paths, so nothing regresses before tickets 04 and 05.
- **Removed:** the test-only `notify(issue)` entry point.
- **Glossary:** **Notification** (the outbox row) and **Integration notification** (the module) are added to `CONTEXT.md` via `domain-modeling`. "Notification kind" deliberately avoids the word "event".

See spec: "Console: the Integration notification module" and "Testing Decisions" (user stories 6, 8, 9, 21, 22, 28–32, 35).

**Blocked by:** None (can start immediately)

**Status:** ready-for-agent

- [ ] The migration renames `event_type` to `kind`, adds `attempts` and the `skipped` status, and keeps both indexes
- [ ] A new Issue with the new-Issue rule on produces one *issue created* row per active Integration and schedules the tick after the initial delay
- [ ] With the new-Issue rule off, no *issue created* row is written
- [ ] Pending *issue created* rows for an Integration are delivered as one batch message, capped at ten Issues plus a remainder count and a link
- [ ] A tick inside the rate-limit window reschedules itself for when the window opens
- [ ] The rate-limit and debounce windows are the same constant
- [ ] The stored `kind` selects the formatted message
- [ ] The processor job and the per-Issue trigger job only call the module
- [ ] `notify(issue)` is removed
- [ ] Assignment and threshold notifications still go out as they do today
- [ ] Tests drive the module's three entry points and observe outbox rows, WebMock requests, ActionMailer deliveries and enqueued jobs (`travel_to` for timing). No constructor stubs, no stubbing the Issue's Event collection.
- [ ] The current notifier and processor job tests are superseded by these tests
- [ ] `CONTEXT.md` defines Notification and Integration notification
