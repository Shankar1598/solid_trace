# 06: Passkeys count as Two-factor

Spec: [../spec.md](../spec.md) (Two-factor, Passkeys), [ADR 0018](../../../docs/adr/0018-passkeys-count-as-two-factor-on-the-console-urls-domain.md)

**What to build:** A Passkey is a second factor. A user who has one, even without an Authenticator app, no longer signs in with a password alone: the second step offers "Use a passkey". A first Passkey, as a user's first factor, shows Recovery codes and ends their other Sessions. A Passkey meets Require two-factor, and removing the last factor is refused there. Passkeys made for another host still count, are listed as unusable here, and can only be removed. The operator reset removes Passkeys too.

**Blocked by:** 03, 04, 05

**Status:** ready-for-agent

- [ ] "Has Two-factor" counts any Passkey, whatever host it was made for
- [ ] The second step offers "Use a passkey" for the pending user's Passkeys on this host, through 02's one place
- [ ] A Passkey as the user's first factor shows Recovery codes and ends their other Sessions; a Passkey added after another factor ends nothing
- [ ] Removing a Passkey accepts a code, the Passkey or a Recovery code as proof; removing the last factor deletes Recovery codes and is refused under Require two-factor
- [ ] A Passkey meets Require two-factor, on pages and over MCP
- [ ] Settings → User lists Passkeys for another host as "Made for <host>; can't be used here", removable only
- [ ] `bin/rails users:reset_two_factor` also removes Passkeys
- [ ] `sign_in_as` can finish the second step with `WebAuthn::FakeClient`
- [ ] Rails integration tests: password + Passkey, Passkey-only user can't sign in with a password alone, first-factor Sessions ended and Recovery codes shown, Require two-factor met, another host's Passkey counted, the reset
- [ ] The Playwright `passkeys` flow also signs in with password + Passkey
