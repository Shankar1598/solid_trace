# 05: Sign in with a Passkey

Spec: [../spec.md](../spec.md) (Passkeys), [ADR 0018](../../../docs/adr/0018-passkeys-count-as-two-factor-on-the-console-urls-domain.md)

**What to build:** Where the fixed Console URL is HTTPS on a domain (or `http://localhost`), a user adds Passkeys on Settings → User and signs in with one alone, from a button or the email field's autofill, with Remember me. Settings → Instance and `get_instance_settings` say whether Passkeys are offered and at which host, or why not. A page opened on another host says where Passkeys work. In this ticket a Passkey doesn't yet count as Two-factor (06).

**Blocked by:** [public-urls 01](../../public-urls/issues/01-console-and-ingest-url-settings.md) merged into `main`

**Status:** resolved

- [x] "Where are Passkeys offered" is one question, answered from the fixed Console URL (the Console URL setting, else `CONSOLE_URL`, never the request's address): offered at a non-IP `https://` host or `http://localhost`, otherwise not, with the reason
- [x] Settings → Instance shows it as a read-only row; `get_instance_settings` includes it
- [x] Each Passkey records the user, credential id, public key, sign count, name, the host it was made for, and when it was last used; users get a WebAuthn user handle
- [x] Settings → User → Passkeys: add (resident key and user verification required, named by the user with a default from the browser and OS), list with made and last used, rename, remove (needs the Passkey itself); on another host, says where Passkeys work instead
- [x] Sign-in page: "Sign in with a passkey" and autofill on the email field where the browser supports it; success makes the Session at once, with Remember me; sign count checked; rate-limited as sign-in is
- [x] Browser side uses the native JSON helpers with a base64url fallback, no WebAuthn library
- [x] Rails integration tests with `WebAuthn::FakeClient`: availability rules, registration, sign-in alone, sign count, a Passkey for another host refused
- [x] Playwright `passkeys` flow with Chromium's virtual authenticator on `http://localhost`: add a Passkey, sign out, sign in with it alone

## Comments

- "Where are Passkeys offered" is `PasskeyOffer` (`app/services/passkey_offer.rb`), from `PublicUrls.console_url` with no request. A page can use Passkeys only when opened at the Console URL's origin (scheme, host and port), so `http://localhost:4001` works in dev and the flow.
- Settings → User lists only the Passkeys for the current host; listing another host's as unusable, and removing them with a code or Recovery code, is 06's. Removing a Passkey here takes an answer from that Passkey alone; 06 moves it into `User::TwoFactor#remove_factor`.
- Each ceremony's challenge is kept in the Rails session for 5 minutes, one at a time, used once, for the purpose it was made for (add, sign in, remove a given Passkey). A malformed answer from the browser is a refusal, not an error.
- In the flow, Chromium's virtual authenticator answers the email field's autofill as soon as the page asks, so the flow signs in that way first (24 h), then with the button and Remember me (7 days) in a browser whose autofill is turned off.
