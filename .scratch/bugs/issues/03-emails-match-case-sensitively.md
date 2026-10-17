# 03: Emails match only in the case they were typed

**Status:** ready-for-agent
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
