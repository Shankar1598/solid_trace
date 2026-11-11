# Passkeys count as Two-factor, made for the Console URL's domain

A user has **Two-factor** when they have an Authenticator app, a Passkey, or both. Once they do, a password alone no longer signs them in: a Passkey signs them in by itself, and a password must be followed by a code, a Recovery code or a Passkey. An Organization's **Require two-factor** is met by either. Every user keeps a password.

A Passkey's relying-party ID is the host of the Console URL, as the Console URL setting or `CONSOLE_URL` gives it, never the address a request arrived on. Passkeys are offered only when that URL is `https://` on a domain (or `http://localhost`, for development and tests), and only to a page opened on that host. There is no switch: an install either can have them or can't. On a bare-IP install (ADR 0010) Two-factor means the Authenticator app alone.

We chose this because a Passkey is the stronger factor: it is bound to the real domain, so a fake sign-in page can't relay it the way it can a six-digit code. Requiring an Authenticator app on top would have pushed Passkey users to keep an app they never open. And had a Passkey counted without ending password-only sign-in, a user with only a Passkey could still be signed in with their password alone. GitHub and Google treat Passkeys the same way.

## Considered options

- **A Passkey doesn't count as Two-factor; Require two-factor needs the Authenticator app.** One meaning of Two-factor, but Passkey users carry an app as well, and a user with a Passkey and no app keeps password-only sign-in.
- **A Passkey replaces only the password, and a code still follows.** A code after a phishing-resistant sign-in adds friction and no protection.
- **A switch on Settings → Instance turns Passkeys on.** It could only ever be on where they work and off where they don't, so it decides nothing.
- **The relying-party ID from each request's host.** An admin who once opened Console by its IP would register Passkeys that stop working at the domain.

## Consequences

- Changing the Console URL's host strands every Passkey made for the old one. Settings → Instance and `update_instance_settings` ask for confirmation, naming how many Passkeys would stop working. A stranded Passkey still counts as Two-factor, so its owner signs in with their password and a code or Recovery code (or, failing that, the operator resets their Two-factor) and makes a new one. Counting it stops a host change from quietly dropping them to password-only sign-in.
- Two-factor protects sign-in, not API tokens: a token made from a signed-in Session works without it. OAuth for the MCP server is the next effort.
- An email password reset never removes Two-factor; only Recovery codes and the operator's `bin/rails users:reset_two_factor` get a user past a lost factor.

Source: [.scratch/two-factor/spec.md](../../.scratch/two-factor/spec.md)
