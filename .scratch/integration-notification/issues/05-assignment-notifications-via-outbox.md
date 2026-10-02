# 05: Assignment notifications via the outbox

**What to build:** When an Issue's assignee changes and an Integration has the assignment rule on, the change is recorded as an *assignment changed* outbox row and delivered under the same per-Integration rate limit as everything else.

- **Trigger:** the Issue model's assignment callback calls the module's *Issue assignment changed* entry point directly, since it only writes outbox rows. The dedicated assignment job and its dead retry declaration are deleted.
- **Payload and names:** the row stores the previous and new assignee ids. Names are resolved at delivery time, including archived (discarded) Organization members, so an archived previous assignee is named rather than shown as "Unassigned". A nil id renders as "Unassigned", so unassigning an Issue produces "To: Unassigned".
- **Delivery:** *assignment changed* rows are delivered one message per row in the same tick. Message formatting is otherwise unchanged.

See spec: "Assignment payload", "Triggers become thin" (user stories 7, 18–20, 34).

**Blocked by:** 02: Integration notification module owns the new-Issue path

**Status:** resolved

- [x] Changing the assignee with the assignment rule on writes one *assignment changed* row per active Integration and schedules the tick
- [x] With the assignment rule off, no row is written
- [x] An archived previous assignee is named correctly in the delivered message
- [x] Unassigning produces a message reading "To: Unassigned"
- [x] *Assignment changed* rows are delivered one message per row and count towards the shared rate limit
- [x] The assignment job and its tests are deleted; the callback calls the module
- [x] Tests drive *Issue assignment changed* and the delivery tick, observing outbox rows and WebMock requests
