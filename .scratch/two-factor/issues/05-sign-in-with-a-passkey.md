# 05: Sign in with a Passkey

Spec: [../spec.md](../spec.md) (Passkeys), [ADR 0018](../../../docs/adr/0018-passkeys-count-as-two-factor-on-the-console-urls-domain.md)

**What to build:** Where the fixed Console URL is HTTPS on a domain (or `http://localhost`), a user adds Passkeys on Settings → User and signs in with one alone, from a button or the email field's autofill, with Remember me. Settings → Instance and `get_instance_settings` say whether Passkeys are offered and at which host, or why not. A page opened on another host says where Passkeys work. In this ticket a Passkey doesn't yet count as Two-factor (06).

**Blocked by:** [public-urls 01](../../public-urls/issues/01-console-and-ingest-url-settings.md) merged into `main`

**Status:** ready-for-agent

- [ ] "Where are Passkeys offered" is one question, answered from the fixed Console URL (the Console URL setting, else `CONSOLE_URL`, never the request's address): offered at a non-IP `https://` host or `http://localhost`, otherwise not, with the reason
- [ ] Settings → Instance shows it as a read-only row; `get_instance_settings` includes it
- [ ] Each Passkey records the user, credential id, public key, sign count, name, the host it was made for, and when it was last used; users get a WebAuthn user handle
- [ ] Settings → User → Passkeys: add (resident key and user verification required, named by the user with a default from the browser and OS), list with made and last used, rename, remove (needs the Passkey itself); on another host, says where Passkeys work instead
- [ ] Sign-in page: "Sign in with a passkey" and autofill on the email field where the browser supports it; success makes the Session at once, with Remember me; sign count checked; rate-limited as sign-in is
- [ ] Browser side uses the native JSON helpers with a base64url fallback, no WebAuthn library
- [ ] Rails integration tests with `WebAuthn::FakeClient`: availability rules, registration, sign-in alone, sign count, a Passkey for another host refused
- [ ] Playwright `passkeys` flow with Chromium's virtual authenticator on `http://localhost`: add a Passkey, sign out, sign in with it alone
