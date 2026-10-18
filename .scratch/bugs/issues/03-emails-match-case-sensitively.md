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

**Existing emails.** The migration `20261018090000_normalize_user_emails` lowercases and trims them. Of users whose emails would then be the same, one keeps the email: the one already in lower case, or else the oldest. The others are left as typed, and the migration names each one ("Left as typed, since user 3 has bob@example.com: user 4 (BOB@example.com). Merge them by hand."), because merging two users is a human decision. Until they're merged, the email signs in as the user who kept it. (Fixed after review; see below.)

Tests:

- `test/models/user_test.rb`: an email kept trimmed and in lower case, and an email taken in another case. The second also covers registration, which saves through the model.
- `test/controllers/sessions_controller_test.rb`: signing in in another case.
- `test/services/member_invite_test.rb`: inviting a member in another case finds them, and no second user is made.

All four fail without the change. The whole Rails suite passes (560 runs).

### 2026-10-18 review

The review found one medium here. The first version of the migration left every member of a colliding group as typed. When none of them was already in lower case (`Bob@Example.com` and `BOB@example.com`), every lookup became `bob@example.com`, which no row had. So none of them could sign in or reset, and the next invite of that email made a third user.

Now one member of each group keeps the normalised email, as described above. `test/migrations/normalize_user_emails_test.rb` covers three cases: one email normalised, a group with one member already in lower case, and a group with none. The last two failed against the first version. The migration's warning goes only to the migration's output (the `db_prepare` container's log in `deploy/docker-compose.yml`), so an admin may not see it. Users left as typed can't sign in until they are merged.

