# 02: "Forgot your password?" leads nowhere

**Status:** needs-triage
**Area:** Console
**Found:** 2026-10-17, while checking the bugs the MCP build raised

## What happens

The sign-in page links "Forgot your password?" to `/passwords/new` (`console/app/frontend/pages/Sessions/New.tsx:50`). `PasswordsController` is Rails' generated scaffold (`6688f7b`, 2026-01-30), and was never finished:

- `new` and `edit` render nothing: there is no `app/views/passwords` and no Inertia page.
- `create` looks the user up by `email_address`, but the column is `email`. `User.find_by(email_address: …)` raises `SQLite3::SQLException: no such column: users.email_address`, so the request fails with a 500. (Checked with `bin/rails runner`.)
- It calls `PasswordsMailer.reset`, which doesn't exist.

So a user who forgets their password can't reset it. Only an admin can help, by inviting them again or through the console.

## To decide

- **Build it:**
  - Inertia pages for "send me a link" and "set a new password";
  - a `PasswordsMailer`, sent the way `OrganizationMailer` sends invites;
  - `email` in the lookup;
  - controller tests and a Playwright flow.

  An install without mail settings then needs its own message.
- **Or remove the link and the routes** until resets are wanted.

## Comments
