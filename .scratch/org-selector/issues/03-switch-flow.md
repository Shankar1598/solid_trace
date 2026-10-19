# 03: Playwright flow for switching Organizations

**Status:** resolved
**Spec:** [spec.md](../spec.md)
**Blocked by:** 01, 02

**What to build:** the one Playwright flow in the spec's Testing Decisions, in `console/e2e/flows/` (prior art: `palette.spec.ts`, and `make-organization.spec.ts`, which makes a second user and Organization).

- [x] A user in two Organizations switches from the sidebar header on a Logs page and lands on the other Organization's Logs.
- [x] `/` then keeps them in that Organization.
- [x] The palette's Organizations group jumps back.
- [x] A user in one Organization sees no dropdown.
- [x] No screenshots. Fix any bug the flow finds in 01's or 02's code.

## Comments

- Flow `console/e2e/flows/switch-organization.spec.ts` makes its own user in `solid-trace-org` and a second Organization with a Project (deleted after), switches from a Project's Logs to the other Organization's Logs, checks `/` keeps it, jumps back from the palette's Organizations group (listing only the other one), and checks the seeded member, in one Organization, sees no dropdown. It passed first run alongside `palette.spec.ts` and `make-organization.spec.ts`; it found no bugs in 01's or 02's code.
