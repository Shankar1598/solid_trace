# 06: Passkeys count as Two-factor

Spec: [../spec.md](../spec.md) (Two-factor, Passkeys), [ADR 0018](../../../docs/adr/0018-passkeys-count-as-two-factor-on-the-console-urls-domain.md)

**What to build:** A Passkey is a second factor. A user who has one, even without an Authenticator app, no longer signs in with a password alone: the second step offers "Use a passkey". A first Passkey, as a user's first factor, shows Recovery codes and ends their other Sessions. A Passkey meets Require two-factor, and removing the last factor is refused there. Passkeys made for another host still count, are listed as unusable here, and can only be removed. The operator reset removes Passkeys too.

**Blocked by:** 03, 04, 05

**Status:** resolved

- [x] "Has Two-factor" counts any Passkey, whatever host it was made for
- [x] The second step offers "Use a passkey" for the pending user's Passkeys on this host, through 02's one place
- [x] A Passkey as the user's first factor shows Recovery codes and ends their other Sessions; a Passkey added after another factor ends nothing
- [x] Removing a Passkey accepts a code, the Passkey or a Recovery code as proof; removing the last factor deletes Recovery codes and is refused under Require two-factor
- [x] A Passkey meets Require two-factor, on pages and over MCP
- [x] Settings → User lists Passkeys for another host as "Made for <host>; can't be used here", removable only
- [x] `bin/rails users:reset_two_factor` also removes Passkeys
- [x] `sign_in_as` can finish the second step with `WebAuthn::FakeClient`
- [x] Rails integration tests: password + Passkey, Passkey-only user can't sign in with a password alone, first-factor Sessions ended and Recovery codes shown, Require two-factor met, another host's Passkey counted, the reset
- [x] The Playwright `passkeys` flow also signs in with password + Passkey

## Comments

- Proof is one shared answer everywhere: a code, a Recovery code, or an answer from **any** of the user's Passkeys for this host (not only the one being removed). 05's per-Passkey `POST /passkeys/:id/removal_options` is replaced by `POST /passkeys/proof_options`, used by removing a Passkey or the app and by making new Recovery codes; the second step has `POST /login/second_step/passkey_options` (the pending user's Passkeys here). A page opened away from the Console URL can't answer with a Passkey, but can remove one with a code or Recovery code.
- Removing a Passkey is now an inline form under it (`aria-label` "Remove <name>"), with the second-step fields and "Use a passkey".
- The Authenticator app's set-up page (where Require two-factor sends members) links to Settings → User to add a Passkey instead, when Passkeys work there; it doesn't embed the add form.
- Removing the app while Passkeys remain says "Authenticator app removed." (no "Two-factor is off").
