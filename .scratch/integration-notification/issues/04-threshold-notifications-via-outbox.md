# 04: Threshold notifications fire correctly, via the outbox

Status: resolved

**What to build:** An Organization admin with the threshold rule on is told once when an Issue receives at least `event_threshold` Events within the last `time_window_minutes`.

- **The rule:** the *Issue received an Event* entry point asks EventStore for the windowed count. If it is greater than or equal to the threshold, and no *threshold reached* row exists for the same Issue and Integration within that window, it writes a *threshold reached* outbox row. The outbox is the dedup record. The rule fires again once the window has passed if the Issue is still spiking.
- **Delivery:** *threshold reached* rows are delivered one message per row in the same tick as any batch, and the tick counts once against the rate limit.
- **Independent rules:** a new Issue that also crosses the threshold produces both an *issue created* and a *threshold reached* row when both rules are on.
- **EventStore unavailable:** the threshold check raises instead of counting zero. The per-Issue trigger job retries it with bounded backoff, then gives up with a logged error. This replaces the job's timeout retry declaration, which never fires. Other callers of the EventStore adapter keep today's swallow-to-default behaviour.

**Deploy note:** a missing threshold setting is *on* in the model but *off* on the New and Edit pages, so once the rule works it will fire for Integrations whose admins believe it is off. Before deploying, check stored Integration settings, or land the settings-default fix (architecture-review candidate #5) first.

See spec: "The threshold rule", "Windowed count failures are errors, not zero", "Batching at delivery" (user stories 1–5, 7, 23, 25).

**Blocked by:** 01: EventStore Event count honours the time window; 02: Integration notification module owns the new-Issue path

**Status:** resolved

- [x] The threshold fires when the windowed count lands exactly on the threshold
- [x] The threshold fires when a burst overshoots it
- [x] A second crossing within the window writes no new row; after the window passes it fires again
- [x] With the threshold rule off, no *threshold reached* row is written
- [x] The EventStore count request carries the window as `newer_than`
- [x] With EventStore unavailable, the trigger job raises and is retried with bounded backoff, and no zero-count decision is made
- [x] A new Issue crossing the threshold with both rules on yields both rows
- [x] *Threshold reached* rows are delivered one message per row in the same tick as the batch
- [x] The dead timeout retry declaration on the trigger job is gone
- [x] Tests drive *Issue received an Event* with WebMock stubbing the EventStore count call and use `travel_to` for the window
