# 03: Members page shows Invited rows with a copy-link button

**Status:** resolved
**Spec:** [spec.md](../spec.md)
**Blocked by:** 02

**What to build:** the Members page half of the spec, and the end-to-end flow.

- [x] An "Invited" badge on Invited rows, for everyone.
- [x] For admins, a copy button on Invited rows (icon button with `aria-label="Copy invite link for <name>"`) that copies `invite_url`. Try `navigator.clipboard.writeText`, then a hidden textarea with `document.execCommand('copy')`, then a popover with the link selected. Toast "Invite link copied" on success.
- [x] The trash button's label on Invited rows reads "Revoke invite" (with a matching confirm text).
- [x] The form's button reads "Invite" ("Inviting…" while it posts).
- [x] Vitest for the copy fallback, if it's its own module.
- [x] Playwright flow `e2e/flows/invite-link.spec.ts`, per the spec's Testing Decisions. No screenshots. Touch the edited `.tsx` files before running, since the dev stack's Vite can serve stale files.

## Comments

Resolved on `feature/invite-links` (merge `9382f3d`, review fixes `eea1ca0`). The copy fallback is `lib/clipboard.ts` (`copyToClipboard`, vitest in `clipboard.test.ts`); its last resort is the existing Dialog with the link selected, since `components/ui` has no popover. The revoke label carries the name ("Revoke invite for <name>"). Revoking still flashes "Member archived successfully". `components/copyText.ts`, the older helper without a fallback, is left as is. `e2e/flows/invite-link.spec.ts` has two tests (join through a copied link; revoke kills the link), cleaning up with `rails runner`; both pass on the dev stack.
