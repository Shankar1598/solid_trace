# 03: Members page shows Invited rows with a copy-link button

**Status:** claimed
**Spec:** [spec.md](../spec.md)
**Blocked by:** 02

**What to build:** the Members page half of the spec, and the end-to-end flow.

- [ ] An "Invited" badge on Invited rows, for everyone.
- [ ] For admins, a copy button on Invited rows (icon button with `aria-label="Copy invite link for <name>"`) that copies `invite_url`. Try `navigator.clipboard.writeText`, then a hidden textarea with `document.execCommand('copy')`, then a popover with the link selected. Toast "Invite link copied" on success.
- [ ] The trash button's label on Invited rows reads "Revoke invite" (with a matching confirm text).
- [ ] The form's button reads "Invite" ("Inviting…" while it posts).
- [ ] Vitest for the copy fallback, if it's its own module.
- [ ] Playwright flow `e2e/flows/invite-link.spec.ts`, per the spec's Testing Decisions. No screenshots. Touch the edited `.tsx` files before running, since the dev stack's Vite can serve stale files.

## Comments
