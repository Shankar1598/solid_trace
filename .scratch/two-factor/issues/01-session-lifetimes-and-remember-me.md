# 01: Session lifetimes and Remember me

Spec: [../spec.md](../spec.md) (Sessions)

**What to build:** A Session lasts 24 hours from signing in, or 7 days if the user ticks Remember me on the sign-in page. Console itself refuses an expired Session, not only the cookie, and sends the user to sign in and back to the page they wanted. Using Console never extends a Session. Sessions signed in before the upgrade keep working for 7 days from it.

**Blocked by:** None (can start immediately)

**Status:** resolved

- [x] Sign-in page has a Remember me box, unticked by default; unticked gives a 24-hour Session, ticked 7 days, counted from sign-in
- [x] The Session's record carries its expiry and the cookie's expiry matches; the cookie is no longer permanent
- [x] An expired Session is refused and deleted; the user signs in and returns to the page they were opening
- [x] Using Console doesn't extend a Session
- [x] The Invite link's and Sign-up's sign-ins make 24-hour Sessions
- [x] Existing Sessions get an expiry of 7 days from the migration
- [x] Rails integration tests: both lifetimes, expiry with time travel, no extension on use, the backfill
- [x] Playwright `remember-me` flow: sign in with and without Remember me and check the cookie's expiry

## Comments

- The cookie is signed with its expiry, so Rails itself refuses a copied cookie after it; Console also refuses and deletes a Session record past its `expires_at`, which is what ends pre-upgrade Sessions (permanent cookies) 7 days after the migration. A new Session's cookie expires with it, so the browser (or Rails, for a copied cookie) drops it first and the record is never looked up again: it stays in `sessions`, unusable, until the user's Sessions are ended (password reset). No clean-up job was added.
- The backfill is `change_column_null`'s default in the one migration, not a separate data migration.
