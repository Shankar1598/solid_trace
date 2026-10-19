# Invite links

**Status:** ready-for-agent

Planned with the user on 2026-10-20, after inviting a member from the Members page failed with a 400 on their install, which has no mail set up.

## Problem Statement

An admin can't add anyone to an Organization from the Members page: every invite fails with a 400. Even once that works, an invite only reaches someone through an email carrying a plain-text password. Installs without SMTP send nothing (bugs [05](../bugs/issues/05-production-mail-unconfigured.md)), so the invited person never learns how to sign in. The Members page doesn't show who has been invited but hasn't signed in yet.

The 400 comes from the page posting `{ email, name }` while `OrganizationUsersController#create` reads `params.require(:member)`. Rails wraps JSON params under `organization_user`, so `ParameterMissing` is raised for every email. The controller tests pass because they send `member: {...}` themselves.

## Solution

Inviting someone new makes them an **Invited** member (see `CONTEXT.md`): a user who has never set a password. Their row on the Members page carries an "Invited" badge, and admins get a copy button that copies an **Invite link** to the clipboard. The admin shares it however they like. Opening it asks for a password (and their name), then signs them in to that Organization. When SMTP is set up, the invite also emails the link. No password is ever emailed.

Someone who already has a password is added at once, as today, and gets no link.

## User Stories

1. As an admin, I want inviting by email from the Members page to work, so that I can add people at all.
2. As an admin on an install without mail, I want to copy an invite link from the invited person's row, so that I can send it to them over chat.
3. As an admin, I want invited people listed in the members table with an "Invited" badge, so that I can see who hasn't signed in yet.
4. As a member who isn't an admin, I want to see the "Invited" badge too, but no copy button, since only admins invite.
5. As an admin, I want copying to work over plain HTTP, since my VM may not have HTTPS turned on.
6. As an admin, I want a toast that tells me what happened: invited (and emailed, if mail is set up), added, or reactivated.
7. As an admin on an install with mail, I want the invite emailed as a link, so that the person can start without me.
8. As an invited person, I want the link to ask me for a password and my name, then sign me in to the Organization that invited me.
9. As an invited person with an expired or used link, I want to be told to ask an admin for a new one, with a link to sign in.
10. As an admin, I want a link to stop working after seven days, once a password is set, or when I revoke the invite, so that old links don't linger.
11. As an admin, I want the trash button on an Invited row to read "Revoke invite".
12. As an admin, I want inviting someone who already has a password to add them at once, as today, without offering a link that would reset their password.
13. As an admin, I want inviting someone already Invited here to say "Already invited. Copy their link from the table."
14. As someone invited to two Organizations, I want the first link I use to set my password, after which I'm an ordinary member of both.
15. As an admin testing a link while signed in, I want the page to say I'll be signed in as the invited person instead.
16. As an admin using the MCP server, I want `invite_member` to return the invite link for someone new, so that an agent can hand it over.

## Implementation Decisions

- **Invited lives on the user.** `users` gains a column marking a user as Invited (a boolean, default false; existing users stay false). `MemberInvite` sets it on the users it makes. It's cleared whenever a password is set: from the invite link, or a password reset (`PasswordsController#update`). Invited users still get a random password, which nobody knows, so sign-in can't succeed and the reset token, which hangs off `password_salt`, keeps working. Sign-up with an Invited user's email is refused as taken, with today's message.
- **Invite token.** `User` gets `generates_token_for :invite, expires_in: 7.days { password_salt.last(10) }`. Setting a password changes the salt, so the link works only once. Each copy makes a fresh token, and older ones keep working until one is used or they expire. Nothing is stored.
- **The link carries its Organization.** For example `GET /invites/:token?org=<slug>` (exact route left to the build). The page is shown, and the password accepted, only while the user is Invited and holds a current (kept) membership of that Organization. Revoking the invite therefore kills the link. Changing the slug can only land them in another Organization that also invited them, which is fine.
- **The invite page** (Inertia, `auth` layout, `allow_unauthenticated_access`): email shown read-only, then name (prefilled), password and confirmation. A blank password is refused, as in `PasswordsController#update`. On success it clears Invited, ends any other session, signs the user in (`start_new_session_for`) and redirects to the Organization's Issues. When someone else is signed in, the page says "You're signed in as X. Setting this password signs you in as Y instead." A bad, expired or used token, or a lost membership, gets "That invite link has expired or was already used. Ask an admin for a new one." with a sign-in link.
- **The Members page props.** For admins, each Invited member's row carries `invite_url`, made when the page renders, so a click copies immediately with no server round trip in between, which some browsers would block. Every row carries `invited: true/false`. Non-admins never receive the URL.
- **Copying.** Use `navigator.clipboard.writeText`. If that's unavailable (HTTP outside localhost) or fails, fall back to a hidden textarea and `document.execCommand('copy')`. If that fails too, open a popover with the link selected. On success, toast "Invite link copied".
- **The 400.** The page sends `{ member: { email, name } }`, matching the controller and its tests.
- **Messages.** Someone new: "Ravi invited. Copy their invite link from the table." without mail, or "Ravi invited and emailed their invite link." with it. An existing user: "Grace added" / "Grace reactivated". Already Invited here: "Already invited. Copy their link from the table." Already a member (with a password): today's message. The form's button reads "Invite".
- **Mail only when it can be sent.** One check says whether mail is set up: in production, when `SMTP_ADDRESS` is set; always in development and test. `MemberInvite` sends the invitation only then, and `Result#emailed` says whether it did. `OrganizationMailer#invitation_email` carries the invite link, not a password, and both templates drop the credentials block.
- **MCP `invite_member`** returns `invite_link` for someone new or still Invited, and `emailed` as now. Its description stops promising an email with a password.
- **Glossary.** `CONTEXT.md` defines **Invite**, **Invited** and **Invite link**, and avoids "magic link".

## Testing Decisions

- Tests check what a request, a user or an agent sees, not how it's built.
- **Rails:**
  - `MemberInvite`: a new user is Invited, is emailed only when mail is set up, and is never sent a password. An existing user with a password is added and not Invited. Re-inviting an Invited member gives the "Already invited" error.
  - Controller: posting `member:` as the page now does works. The Members page gives admins `invite_url` for Invited rows and gives members none.
  - The invite page: the right token and membership shows the form. Setting a password signs them in, clears Invited, lands on the Organization's Issues, and makes the same link stop working. Expired, garbled, revoked (membership archived) and already-used links are refused. A blank password is refused. A password reset clears Invited.
  - MCP: `invite_member` returns `invite_link`.

  Prior art: `test/services/member_invite_test.rb`, `test/controllers/organization_users_controller_test.rb`, the passwords controller tests, `test/mcp/admin_tools_test.rb`.
- **Vitest** for the copy fallback, if it lives in its own module.
- **One Playwright flow**, `e2e/flows/invite-link.spec.ts`: an admin invites someone from the Members page and sees the Invited row. They copy the link (read it from the clipboard, or from the row's data in the test), open it in a fresh browser context, set a name and password, and land on the Organization's Issues. Back on the admin's page, the badge has gone, and the old link now says it's expired. No screenshots. Prior art: `password-reset.spec.ts`, `make-organization.spec.ts`.
- Run only the relevant tests. The user runs the full suites.

## Out of Scope

- A separate Invitation record, or asking existing users to accept before they're added.
- Revoking one link without revoking the invite.
- Changing the sign-up message for Invited emails, which would tell strangers who's been invited.
- Showing the other paths bugs 05 names (password reset and Monitor email) as unavailable without mail. Only the invite path changes here.
- Roles chosen at invite time (everyone joins as a member, as today).

## Further Notes

- No ADR: an Invited flag and a stateless token are easy to replace with an Invitation table later.
- `MemberInvite` is shared by the Members page and the MCP tool, so both change together.
