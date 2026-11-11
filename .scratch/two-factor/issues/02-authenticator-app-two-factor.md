# 02: Two-factor with an Authenticator app

Spec: [../spec.md](../spec.md) (Two-factor)

**What to build:** A user sets up an Authenticator app on Settings → User by scanning a QR code (or typing the secret) and confirming with a code. From then on they have Two-factor: a right password no longer signs them in alone, but leads to a second-step page that takes a code. Getting Two-factor ends their other Sessions. Removing the app needs a code. Recovery codes (03) and Passkeys (06) join this later, so "has Two-factor" and "what the second step accepts" are each built as one place they can extend.

**Blocked by:** None (can start immediately)

**Status:** ready-for-agent

- [ ] Settings → User → Two-factor: set up shows a QR code (issuer "SolidTrace", the user's email) and the secret as text; it counts as Two-factor only once a code from the app confirms it
- [ ] The secret is encrypted at rest as the SMTP password is (ADR 0015)
- [ ] A code is accepted for its 30-second step and one step either side, and no step is accepted twice
- [ ] "Has Two-factor" and "the second step accepts" each live in one place, used by sign-in and by removal
- [ ] Sign-in: a right password for a user with Two-factor makes no Session and goes to the second-step page; the pending step holds the user and Remember me for 5 minutes and is cleared by success or a new sign-in; the second step is rate-limited as sign-in is
- [ ] Getting Two-factor ends the user's other Sessions and keeps the current one
- [ ] Removing the Authenticator app needs a code in the same request
- [ ] The set-up page is reachable outside any Organization's pages, and can be told which Organization to return to (04 sends members there)
- [ ] Password reset leaves Two-factor as it is (and still ends Sessions and revokes API tokens)
- [ ] No MCP tool reads or changes Two-factor
- [ ] `sign_in_as` in the integration test helper finishes the second step with a code computed from the user's secret
- [ ] Rails integration tests: confirm on set-up, wrong code, replayed step, drift, pending step expiry, Sessions ended, removal needs a code
- [ ] Playwright `two-factor` flow: set up with a code computed from the secret the page shows, sign out, sign in with password + code
