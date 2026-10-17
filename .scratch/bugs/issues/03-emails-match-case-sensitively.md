# 03: Emails match only in the case they were typed

**Status:** resolved
**Area:** Console
**Found:** 2026-10-17, by the MCP build's ticket 09 (09-verify-2)

## What happens

User emails aren't normalised. Every lookup is an exact match on SQLite's case-sensitive `=`, and the case-sensitive unique index on `users.email` lets two users differ only in case. The lookups:

- `MemberInvite` (`User.find_by(email:)`), used by the Members page and the MCP's `invite_member`;
- sign-in (`User.authenticate_by(email:)`);
- registration.

Effects:

- **Duplicate user on invite.** Inviting `BOB@example.com` while `bob@example.com` is a member makes a second user, named `BOB`, and emails them a password. The inviter, or the agent, sees "added", not "already a member". Probed by the reviewer.
- **Sign-in fails on case.** A user who signs in with a different capitalisation is told the email or password is wrong.
- **Duplicate accounts at sign-up.** One person can register twice, as `Bob@` and `bob@`.

This predates the MCP. The MCP makes it easier to hit, because agents change an email's case freely.

## Fix

- `normalizes :email, with: ->(e) { e.strip.downcase }` on `User`. Rails applies it to `find_by` and `authenticate_by` too.
- A migration that lowercases existing emails. Before it runs, look for rows that collide once lowercased: they are the same person twice, and merging them is a human decision. Report them rather than guess.
- Tests: an invite in another case finds the member; sign-in in another case works; registering an existing email in another case is refused.

## Comments

### 2026-10-18 resolved

`User` now has `normalizes :email, with: ->(email) { email.strip.downcase }`. Rails applies it when an email is assigned, even through `update_column`, and to finder arguments. So `find_by(email:)` in `MemberInvite`, `authenticate_by` at sign-in and the uniqueness check all match in any case, with no change to those callers.

**Existing emails.** The migration `20261018090000_normalize_user_emails` lowercases and trims them. Emails that would then be the same are left as they are, and the migration names them with their ids ("Not normalized, as 2 users would share …"), because merging two users is a human decision. Until they're merged, signing in with that email finds the lower-case user only. A scratch probe on the test database checked both cases: a mixed-case email was normalised and could sign in, and a colliding pair was named and left as it was.

Tests:

- `test/models/user_test.rb`: an email kept trimmed and in lower case, and an email taken in another case. The second also covers registration, which saves through the model.
- `test/controllers/sessions_controller_test.rb`: signing in in another case.
- `test/services/member_invite_test.rb`: inviting a member in another case finds them, and no second user is made.

All four fail without the change. The whole Rails suite passes (560 runs).

