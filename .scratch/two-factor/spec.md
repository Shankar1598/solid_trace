# Two-factor, Passkeys and Session lifetimes

Status: ready-for-agent

Planned with the user on 2026-11-11 (grilling). Terms are in [GLOSSARY.md](../../GLOSSARY.md): **Session**, **Two-factor**, **Authenticator app**, **Passkey**, **Recovery code**, **Require two-factor**. The decision that a Passkey counts as Two-factor, on the Console URL's domain, is [ADR 0018](../../docs/adr/0018-passkeys-count-as-two-factor-on-the-console-urls-domain.md). Tickets: [issues/](issues/).

## Problem Statement

Anyone who has a user's password can sign in to Console as them: there is no second factor, and no way to sign in without typing a password. Once signed in, a Session lasts until the user signs out, so a borrowed or stolen laptop stays signed in indefinitely. An Organization's admins, who are responsible for who sees its Projects, Logs and Issues, can't require more of their members than a password.

## Solution

- A Session lasts 24 hours from signing in, or 7 days if the user ticks **Remember me**.
- A user can turn on **Two-factor** by setting up an **Authenticator app**, adding a **Passkey**, or both. Once they have either, a password alone no longer signs them in: after the password they give a code, a **Recovery code** or a Passkey.
- A Passkey also signs a user in on its own, with no password and no code. Passkeys are offered wherever Console's fixed Console URL is HTTPS on a domain, as Tagalys' `https://solid-trace.bluebuck-viper.ts.net` is; on a bare-IP install, Two-factor means the Authenticator app.
- A user who loses their factors signs in with a Recovery code; failing that, the operator resets their Two-factor from a shell.
- An Organization's admins can turn on **Require two-factor**: members without Two-factor are then taken to set it up before they can open the Organization, and their API tokens stop working on it.

## User Stories

### Sessions

1. As a user, I want my Session to end 24 hours after I sign in, so that a browser I forget about doesn't stay signed in.
2. As a user, I want a Remember me box on the sign-in page, so that on my own computer I stay signed in for 7 days.
3. As a user, I want Remember me left unticked by default, so that a shared computer gets the shorter Session unless I choose otherwise.
4. As a user, I want my Session's length counted from when I signed in, not from when I last used Console, so that using Console daily doesn't keep me signed in forever.
5. As a user whose Session has ended, I want to be sent to the sign-in page and then back to the page I was opening, so that an expired Session costs me only a sign-in.
6. As a user who signs in with a Passkey, I want the same Remember me box to apply, so that I choose my Session's length whichever way I sign in.
7. As an Invited member who sets a password from an Invite link, I want to be signed in for 24 hours, so that joining works as it does today.
8. As a user signed in before this release, I want my Session to keep working for 7 days after the upgrade, so that the upgrade doesn't sign everyone out at once.
9. As an operator, I want an expired Session refused by Console itself, not only by the cookie's expiry, so that a copied cookie stops working on time.

### Setting up Two-factor

10. As a user, I want to set up an Authenticator app from Settings → User by scanning a QR code, so that I can turn on Two-factor in a minute.
11. As a user whose phone can't scan the QR code, I want the secret shown as text, so that I can type it into my app.
12. As a user setting up an Authenticator app, I want it to count only once I've entered a code from it, so that a scan that didn't work can't lock me out.
13. As a user, I want to see Recovery codes once, when I first get Two-factor, so that I have a way in if I lose my phone or Passkey.
14. As a user, I want to copy or download my Recovery codes as a text file, so that I can keep them in my password manager or on paper.
15. As a user, I want Settings → User to say how many Recovery codes I have left, so that I know when to make new ones.
16. As a user, I want to make a new set of Recovery codes, after proving it's me, so that I can replace a set I've used up or exposed.
17. As a user, I want my other Sessions ended when I first get Two-factor, so that anyone signed in as me elsewhere is signed out.
18. As a user, I want adding a second factor not to sign me out anywhere, so that adding a Passkey after the app costs nothing.
19. As a user, I want to remove my Authenticator app only after giving a code, a Passkey or a Recovery code, so that someone at my unlocked browser can't turn off my Two-factor.
20. As a user, I want removing my last factor to turn Two-factor off and throw away my Recovery codes, so that unused codes don't linger.
21. As a user with a new phone, I want to remove my Authenticator app and set it up again, so that I can move to the new phone.

### Signing in with Two-factor

22. As a user with Two-factor, I want to be asked for a code after my password, so that a stolen password isn't enough to sign in as me.
23. As a user with Two-factor, I want a code from a moment ago still accepted, so that a slow typist or a slightly wrong clock doesn't fail me.
24. As a user with Two-factor, I want each code to work only once, so that a code seen over my shoulder can't be reused.
25. As a user who can't reach their app, I want to give a Recovery code instead, so that I can still sign in.
26. As a user who signed in with a Recovery code, I want to be told how many are left, so that I know to make new ones.
27. As a user with a Passkey, I want to use it at the second step after my password, so that I can finish signing in on a browser where I typed my password.
28. As a user who stops halfway, I want the second step to expire after 5 minutes, so that a half-finished sign-in on a shared computer can't be completed later.
29. As a user, I want repeated wrong codes slowed down, so that a code can't be guessed.
30. As a user who resets their password by email, I want my Two-factor kept, so that someone who can read my email still can't sign in as me.
31. As a user who has lost every factor and every Recovery code, I want the operator to be able to reset my Two-factor, so that I'm not locked out for good.
32. As an operator, I want a shell command that removes a user's Two-factor and ends their Sessions, so that I can let a locked-out user back in without touching the database.
33. As an operator, I want no Console page and no MCP tool able to remove someone's Two-factor, so that a stolen admin Session or API token can't turn it off.

### Passkeys

34. As a user, I want to add a Passkey from Settings → User, so that I can sign in with my fingerprint, face or PIN.
35. As a user, I want to name each Passkey, so that I can tell my laptop's from my phone's.
36. As a user, I want to see when each Passkey was made and last used, so that I can spot one I no longer use.
37. As a user, I want to rename and remove Passkeys, removing only after proving it's me, so that I keep the list tidy and safe.
38. As a user, I want a "Sign in with a passkey" button on the sign-in page, so that I can sign in without my password.
39. As a user, I want my browser to offer my Passkey in the email field's autofill, so that signing in is one tap.
40. As a user, I want a Passkey sign-in to need no code, so that the stronger way to sign in is also the quickest.
41. As a user whose first factor is a Passkey, I want Recovery codes and my other Sessions ended, as with the app, so that both factors behave alike.
42. As a user with a Passkey and no Authenticator app, I want my Passkey to count as Two-factor, so that I don't need an app as well.
43. As a user who keeps a password, I want it to keep working with my second step after it, so that I can still sign in on a device without my Passkey.
44. As a user who opened Console at an address other than its Console URL, I want the Passkey parts to tell me where Passkeys work, so that I'm not left with a browser error.
45. As a user whose Passkeys were made for Console's old domain, I want them listed as unusable here and removable, so that I understand why they don't work and can tidy up.
46. As a user whose only Passkeys were made for an old domain, I want them still to count as Two-factor, so that the domain change doesn't drop me to password-only sign-in.
47. As an Instance admin, I want Settings → Instance to show whether Passkeys are offered and at which host, or why not, so that I know what my install supports.
48. As an Instance admin over MCP, I want `get_instance_settings` to say the same, so that an agent can tell me too.
49. As an Instance admin on a bare-IP install, I want Passkeys simply not offered, so that nobody registers one that can't work.
50. As an Instance admin changing the Console URL to another host, I want to be told how many Passkeys, of how many users, would stop working before I save, so that I don't break sign-in by surprise.
51. As an Instance admin over MCP, I want `update_instance_settings` to refuse such a change unless I confirm it, so that an agent can't break Passkeys by accident.
52. As an operator, I want `bin/rails instance_settings:set` to change the host without asking, so that I can recover from a shell.

### Require two-factor

53. As an Organization admin, I want to turn on Require two-factor on the Organization's settings page, so that every member must have a second factor.
54. As an Organization admin, I want to see how many members don't have Two-factor before I turn it on, so that I know who'll be asked.
55. As an Organization admin without Two-factor, I want to be told to set it up before I can turn the requirement on, so that I can't require what I don't have.
56. As an Organization admin, I want the requirement changeable only on the page, never over MCP, so that a leaked API token can't turn it off.
57. As a member without Two-factor in such an Organization, I want to be taken to the set-up page when I open any of its pages, told which Organization requires it, and returned there afterwards, so that I know why and get back to work quickly.
58. As a member already signed in when the requirement is turned on, I want to be asked at my next page in that Organization, so that the requirement applies at once.
59. As a member of several Organizations, I want my other Organizations to keep working while one requires Two-factor, so that one Organization's rule doesn't block the rest.
60. As a member, I want Settings → User and signing out to keep working while I'm being asked to set up Two-factor, so that I can actually set it up.
61. As a member without Two-factor, I want my API tokens refused on that Organization over MCP with a message saying why, so that my agent tells me what to do.
62. As a member, I want my API tokens to keep working on my other Organizations, so that only the requiring Organization is affected.
63. As an Invited member of such an Organization, I want to set my password from the Invite link and then be taken to set up Two-factor, so that I join with Two-factor from the start.
64. As a member of such an Organization, I want Console to refuse removing my last factor and name the Organizations that require it, so that I can't fall out of compliance by accident.
65. As a member, I want either an Authenticator app or a Passkey to meet the requirement, so that I can choose the factor I prefer.

## Implementation Decisions

### Sessions

- `sessions` gains an expiry. Signing in takes Remember me and sets the expiry to 24 hours or 7 days from now; the cookie's expiry matches (no more permanent cookie). The Invite link and Sign-up sign in with 24 hours.
- Resuming a Session refuses, and deletes, one past its expiry; the existing redirect to sign-in with the return-to URL handles the rest.
- The migration backfills existing Sessions to migration time + 7 days, then makes the expiry required.
- Use never extends a Session.

### Two-factor

- **"Has Two-factor"** is one question on the user, answered from a confirmed Authenticator app or any Passkey (whatever host it was made for). Everything that branches on Two-factor asks it, so 04 adds Passkeys in one place.
- **The second step** is one place that accepts a code, a Recovery code or (from 04) a Passkey assertion, used by sign-in, by removing a factor and by making new Recovery codes.
- **Authenticator app:** TOTP per RFC 6238 (6 digits, 30 s, SHA-1) with `rotp`; the QR code is an inline SVG drawn server-side with `rqrcode` from the `otpauth://` URI (issuer "SolidTrace", the user's email). On the user: the secret, encrypted with Active Record Encryption as the SMTP password is (ADR 0015); when it was confirmed (unconfirmed means set-up in progress, not Two-factor); and the last accepted time step. A code is accepted for its step ±1, and only for a step later than the last accepted one. One Authenticator app per user.
- **Recovery codes:** a `recovery_codes` table (user, digest, used at). 10 per set, made when the user first gets Two-factor, shown once; a new set replaces the old. Removing the last factor deletes them.
- **Getting Two-factor** (the transition from not having it) ends the user's other Sessions, keeping the current one.
- **Removing a factor** needs a second-step answer in the same request. Removing the last one is refused while any of the user's Organizations has Require two-factor, naming them.
- **Sign-in:** after a right password, a user with Two-factor gets no Session. Their id, the time and Remember me go in the Rails session; the browser goes to the second-step page. The pending step lasts 5 minutes and is cleared by success or a new sign-in. The second-step action is rate-limited as sign-in is (10 in 3 minutes). A Recovery code sign-in's notice says how many are left.
- **Password reset** is unchanged: it ends Sessions and revokes API tokens, and leaves Two-factor.
- **Operator reset:** `bin/rails users:reset_two_factor EMAIL` removes the Authenticator app, every Passkey and the Recovery codes, and ends the user's Sessions. Documented in the install docs.
- **No MCP tool** reads or changes a user's Two-factor or Passkeys.

### Passkeys

- **Gem:** `webauthn`. **Browser:** the native `PublicKeyCredential.parseCreationOptionsFromJSON` / `parseRequestOptionsFromJSON` and `toJSON` where present, else a small base64url shim; no WebAuthn JS library.
- **Schema:** a `passkeys` table (user, credential id (unique), public key, sign count, name, the host it was made for, last used at); the user gains a WebAuthn user handle.
- **Relying-party ID:** the host of the fixed Console URL from public-urls' resolver: the Console URL setting, else `CONSOLE_URL`, **never** the request's address. One question, "where are Passkeys offered", answers: offered at <host>, or not, with the reason (no fixed Console URL, not HTTPS, an IP address). `https://` on a non-IP host, or `http://localhost` for development and tests, is offered. A page opened on another host shows where Passkeys work instead of the Passkey controls.
- **Registration:** resident key required, user verification required, any authenticator; excludes the user's existing credentials. Named by the user, defaulting to one from the browser and OS.
- **Sign-in with a Passkey:** a discoverable-credential assertion with user verification required, from a button and from conditional mediation on the email field; success makes the Session with Remember me, with no code. Rate-limited as sign-in is. Sign count is checked and updated.
- **At the second step:** an assertion limited to the pending user's Passkeys for this host.
- **Passkeys for another host** are listed as unusable here, removable only, and still count as Two-factor (ADR 0018).
- **Settings → Instance and `get_instance_settings`:** a read-only Passkeys row from "where are Passkeys offered".

### Require two-factor

- `organizations` gains Require two-factor (default off), changed only from the Organization's settings page by an admin who has Two-factor. The page shows the count of members without Two-factor before turning it on. No MCP tool exposes it.
- **Pages:** the shared Organization lookup, which every Organization page goes through, sends a member without Two-factor of a requiring Organization to the Two-factor set-up page, carrying the Organization so the page can name it and return there. Checked on every request, so it applies to Sessions already signed in. Pages outside an Organization are untouched.
- **MCP:** where an MCP call resolves the Organization it acts on, a requiring Organization refuses a user without Two-factor, with a message to set it up in Console. Calls on other Organizations are unaffected.
- **Invite link:** unchanged; its sign-in lands in the Organization, whose lookup sends the member to set-up.

### Changing the Console URL's host

- Saving a Console URL whose host differs from the current one, while Passkeys exist for the current host, needs confirmation: Settings → Instance shows a confirmation with the count of Passkeys and users; `update_instance_settings` refuses without `confirm_passkeys_stop_working: true` and says the counts. `bin/rails instance_settings:set` doesn't ask.

## Testing Decisions

- **A good test** drives Console as a browser or MCP client does and checks what comes back: redirects, pages' props, cookies, MCP responses, whether a Session exists. It doesn't reach into how a code is verified or how "has Two-factor" is stored, so 04 adding Passkeys doesn't rewrite 02's tests.
- **Main seam: Console's HTTP surface**, through Rails integration tests: sign-in and the second step, Passkey sign-in, Remember me and expiry (with time travel), Settings → User, the Organization settings page, an Organization's pages under Require two-factor, the Invite link, password reset, and `POST /mcp` for the MCP refusal, `get_instance_settings` and `update_instance_settings`. The integration test helper's `sign_in_as` learns to finish the second step: a code computed with ROTP from the user's secret, or an assertion from `WebAuthn::FakeClient`.
- **Prior art:** the sessions, passwords, invites, user settings and organization settings controller tests; the MCP integration test (JSON-RPC to `POST /mcp` with an API token) and the instance settings tools test.
- **The operator reset** is tested as the existing rake tasks are.
- **UI proof: Playwright flows** in Console's e2e flows: `two-factor` (set up with a code computed from the secret the page shows, sign out, sign in with password + code, then with a Recovery code), `passkeys` (Chromium's virtual authenticator over CDP on `http://localhost`: add, sign out, sign in with it alone, then password + Passkey), `require-two-factor` (admin turns it on, a member is taken to set-up and returns), and `remember-me` (cookie expiry with and without). Prior art: the password-reset and invite-link flows, which make their own users with `rails runner`. The global setup signs the seeded admin in fresh each run, so 24-hour Sessions don't affect other flows; the seeded admin never gets Two-factor.
- **No unit tests below these seams.**
- Targeted tests per ticket; the full suites are for the user.

## Out of Scope

- **OAuth for the MCP server** (browser sign-in for MCP clients, short-lived tokens, refresh): the next effort, for the release after this one. API tokens don't change here: no expiry, and making one needs no Two-factor beyond the Session it's made in.
- A list of Sessions on Settings → User with "sign out" per Session.
- An instance-wide requirement, a grace period, or removing members who don't comply.
- Emails about Two-factor: turned on, Require two-factor turned on, a new sign-in.
- SMS or email codes; more than one Authenticator app per user.
- "Remember this browser" (skipping the code on a trusted browser).
- "Skip password when possible" or any other per-user choice about Passkey sign-in.
- Passkey-only accounts: every user keeps a password.

## Further Notes

- **Order:** [01](issues/01-session-lifetimes-and-remember-me.md) (Sessions) and [02](issues/02-authenticator-app-two-factor.md) (Authenticator app) can start at once, and [05](issues/05-sign-in-with-a-passkey.md) (sign in with a Passkey) once [public-urls 01](../public-urls/issues/01-console-and-ingest-url-settings.md) is merged into `main`. [03](issues/03-recovery-codes-and-operator-reset.md) (Recovery codes, operator reset) and [04](issues/04-require-two-factor.md) (Require two-factor) need 02. [06](issues/06-passkeys-count-as-two-factor.md) (Passkeys count as Two-factor) needs 03, 04 and 05. [07](issues/07-console-url-host-change-warns-about-passkeys.md) (the host-change warning) needs 05.
- Tagalys' install: Console at `https://solid-trace.bluebuck-viper.ts.net` (Tailscale) once public-urls ships, so Passkeys are offered there; ingest stays on the IP, which Passkeys don't touch.
- A changed `SECRET_KEY_BASE` makes Authenticator app secrets unreadable, as it does the SMTP password: those users sign in with a Recovery code or a Passkey, or the operator resets them.
- Decisions made while writing the spec rather than in the grilling: Passkeys for another host still count as Two-factor; `update_instance_settings` needs a confirmation argument for a host change that strands Passkeys; one Authenticator app per user; 10 Recovery codes, a new set needing proof; a 5-minute second step; 24-hour Sessions from the Invite link and Sign-up; the operator reset ends Sessions.
