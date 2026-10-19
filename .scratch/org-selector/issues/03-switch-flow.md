# 03: Playwright flow for switching Organizations

**Status:** ready-for-agent
**Spec:** [spec.md](../spec.md)
**Blocked by:** 01, 02

**What to build:** the one Playwright flow in the spec's Testing Decisions, in `console/e2e/flows/` (prior art: `palette.spec.ts`, and `make-organization.spec.ts`, which makes a second user and Organization).

- [ ] A user in two Organizations switches from the sidebar header on a Logs page and lands on the other Organization's Logs.
- [ ] `/` then keeps them in that Organization.
- [ ] The palette's Organizations group jumps back.
- [ ] A user in one Organization sees no dropdown.
- [ ] No screenshots. Fix any bug the flow finds in 01's or 02's code.
