# 02: Email settings page

**Status:** ready-for-agent
**Spec:** [spec.md](../spec.md)
**Blocked by:** 01

**What to build:** an Email page in Settings where an instance admin sets SMTP and From, and sends a test email.

- [x] Route `/:org_slug/settings/email` (`show`, `update`), plus a `POST` for the test email, in a controller that 404s, or redirects the way other settings pages refuse, for anyone who isn't an instance admin. The Settings nav lists "Email" only for instance admins, and the page says it applies to the whole install, every Organization. Follow `pages/Settings/Organization.tsx` and its controller for layout, forms and flash.
- [x] The form has host, port, username, password and From. The password field is never filled from the server. The props carry only whether a password is saved, and whether it's readable. With one saved, the field reads "Saved — leave blank to keep" or similar, with a way to clear it. If it's unreadable, the page says it must be entered again, because Console's secret changed. Blank keeps the saved password. Clearing the username clears the password too.
- [x] A status line: "Email is set up", or what's missing ("No SMTP host: Console sends no email").
- [x] Send test email: one field for the recipient, defaulting to the current user's email. It sends synchronously (`deliver_now`) and shows success, or the SMTP error's message (rescue `Net::SMTP*`, `Timeout`, socket and `OpenSSL` errors and show their message), never a 500. When email isn't set up, the button is disabled and says why. In development, letter_opener catches it.
- [x] Request tests:
  - instance admin only: an Organization admin who isn't an instance admin is refused for show, update and the test email;
  - the response never contains the password;
  - blank password keeps the saved one, and clear removes it;
  - validation errors come back on the form;
  - the test email's success, and its SMTP failure (stub delivery to raise).
- [x] A Playwright flow in `console/e2e`:
  - the seeded instance admin opens Settings → Email;
  - they save host, port, username, password and From, reload, and the password shows as saved and the field is empty;
  - they send a test email and see success (development uses letter_opener);
  - a member who isn't an instance admin sees no Email link.

  Take one desktop-dark screenshot to check the page renders. Touch edited `.tsx` files before running flows, because Vite can serve stale files.

## Comments

**2026-10-22, implementer (feature/email-settings).** Built as asked. Calls made where the ticket was silent:
- Routes: `resource :email_settings, path: "email"` in the Organization's settings scope, so `GET`/`PATCH /:org_slug/settings/email` and `POST /:org_slug/settings/email/test` (`send_test`). Anyone who isn't an instance admin gets the Pundit refusal other pages use: a redirect back with "You are not authorized to perform this action."
- The Settings nav has an "Install" group holding "Email", only for instance admins. The shared `current_user` prop gains `instance_admin`.
- Clearing the password is a "Clear the saved password" link that sets `clear_password` and disables the field, with "Keep it" to undo. Saving keeps the field empty.
- `InstanceSetting#update_email` applies only the settings given, keeps the password when it's blank and clears it on `clear_password`; MCP's `update_email_settings` uses it too. `EmailSettingsSerializer` is the shape both show.
- `EmailSettingsMailer.send_test(to)` sends with `deliver_now` and returns the failure's message (Net::SMTP errors, timeouts, socket and system call errors, `IOError`, `OpenSSL::SSL::SSLError`), or nil. It also refuses when email isn't set up, with the reason, and a `to` that isn't an email address. The page shows the result as a flash toast. The button's disabled state reads the row in development too, so it stays disabled there until a host and From are saved.
- The flow clears the email settings before and after it runs, so the dev database stays as it was. Its screenshot: `~/.cache/solid-trace/screenshots/email-settings/email-settings--flows.png`. letter_opener caught the test email, from the saved sender.
