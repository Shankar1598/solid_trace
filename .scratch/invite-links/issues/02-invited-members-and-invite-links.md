# 02: Invited members and the invite link page

**Status:** ready-for-agent
**Spec:** [spec.md](../spec.md)
**Blocked by:** 01

**What to build:** the Rails half of the spec, and the invite link page.

- [ ] Migration: `users` gains an Invited flag (boolean, default false, not null). ADR 0001 doesn't apply, since EventStore doesn't write `users`.
- [ ] `User`: `generates_token_for :invite, expires_in: 7.days { password_salt.last(10) }`, and a way to build a user's invite URL for an Organization.
- [ ] `MemberInvite`: marks the users it makes as Invited. It emails only when mail is set up (one check: in production, `SMTP_ADDRESS` present; always in development and test), and `emailed` says whether it did. "Already invited. Copy their link from the table." for a kept membership whose user is Invited. No password is passed to the mailer.
- [ ] `OrganizationMailer#invitation_email` and both templates carry the invite link instead of the credentials.
- [ ] The invite page: route, controller (`allow_unauthenticated_access`, `auth` layout, rate-limited like passwords), and Inertia page `Invites/Edit`. It shows the email read-only, then name (prefilled), password and confirmation, and says who is signed in when someone else is. On success: clear Invited, `terminate_session` for any other user, `start_new_session_for`, redirect to the Organization's Issues. A token that's bad, expired or used, or a user who isn't Invited or has no kept membership of `org`, gets the "expired or already used" message with a sign-in link. A blank password is refused.
- [ ] `PasswordsController#update` clears Invited.
- [ ] `OrganizationUsersController#create`'s notices follow the spec's Messages. `#index` gives each member `invited`, and gives admins `invite_url` on Invited rows (the serializer or the controller, whichever reads better).
- [ ] MCP `invite_member` returns `invite_link` for someone new or still Invited, and its description drops "an email with their password".
- [ ] Rails tests per the spec's Testing Decisions.

Ticket 03 reads `members[].invited: boolean` and `members[].invite_url?: string`.

## Comments
