# 02: "Forgot your password?" leads nowhere

**Status:** resolved
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

### 2026-10-18 resolved

Built, as the user asked ("fix the bugs"), rather than removed:

- **Pages:** `Passwords/New` asks for the email and `Passwords/Edit` takes the new password twice. Both use the sign-in page's layout and look.
- **Asking for a link:** the lookup uses `email`, which now matches in any case (bugs 03). A user who has the email is sent `PasswordsMailer.reset`, through `deliver_later` as invites are. The answer is the same whether or not a user has it. Asking is rate-limited to 10 times in 3 minutes, as Rails' generator does.
- **The link:** it is `has_secure_password`'s reset token. It works for 15 minutes, and only until the password changes. A wrong, expired or used link goes back to "Forgot your password?" and says why.
- **Saving:** a blank password is refused; `has_secure_password` would otherwise ignore it and report success. A confirmation that doesn't match shows its error under the field. A reset password signs the user out of every session.

Tests:

- `test/controllers/passwords_controller_test.rb`, 10 cases.
- `test/mailers/passwords_mailer_test.rb`: the link in both parts finds the user.
- A mail preview.
- The Playwright flow `e2e/flows/password-reset.spec.ts`: from the sign-in link to signing in with the new password. It is written but not run: MCP ticket 10 runs it.

The Rails suite (571 runs), vitest and typecheck pass.

**The email won't arrive on a production install yet.** Production has no mail settings: the link host is `example.com`, there's no SMTP, and the sender is `from@example.com`. That is [bugs 05](05-production-mail-unconfigured.md), and invitation, Issue and Monitor emails share it.

