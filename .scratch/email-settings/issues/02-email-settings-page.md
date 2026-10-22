# 02: Email settings page

**Status:** ready-for-agent
**Spec:** [spec.md](../spec.md)
**Blocked by:** 01

**What to build:** an Email page in Settings where an instance admin sets SMTP and From, and sends a test email.

- [ ] Route `/:org_slug/settings/email` (`show`, `update`), plus a `POST` for the test email, in a controller that 404s, or redirects the way other settings pages refuse, for anyone who isn't an instance admin. The Settings nav lists "Email" only for instance admins, and the page says it applies to the whole install, every Organization. Follow `pages/Settings/Organization.tsx` and its controller for layout, forms and flash.
- [ ] The form has host, port, username, password and From. The password field is never filled from the server. The props carry only whether a password is saved, and whether it's readable. With one saved, the field reads "Saved — leave blank to keep" or similar, with a way to clear it. If it's unreadable, the page says it must be entered again, because Console's secret changed. Blank keeps the saved password. Clearing the username clears the password too.
- [ ] A status line: "Email is set up", or what's missing ("No SMTP host: Console sends no email").
- [ ] Send test email: one field for the recipient, defaulting to the current user's email. It sends synchronously (`deliver_now`) and shows success, or the SMTP error's message (rescue `Net::SMTP*`, `Timeout`, socket and `OpenSSL` errors and show their message), never a 500. When email isn't set up, the button is disabled and says why. In development, letter_opener catches it.
- [ ] Request tests:
  - instance admin only: an Organization admin who isn't an instance admin is refused for show, update and the test email;
  - the response never contains the password;
  - blank password keeps the saved one, and clear removes it;
  - validation errors come back on the form;
  - the test email's success, and its SMTP failure (stub delivery to raise).
- [ ] A Playwright flow in `console/e2e`:
  - the seeded instance admin opens Settings → Email;
  - they save host, port, username, password and From, reload, and the password shows as saved and the field is empty;
  - they send a test email and see success (development uses letter_opener);
  - a member who isn't an instance admin sees no Email link.

  Take one desktop-dark screenshot to check the page renders. Touch edited `.tsx` files before running flows, because Vite can serve stale files.
