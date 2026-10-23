# Email settings in Console

Planned with the user on 2026-10-22. They want to set SMTP from the dashboard or MCP, not from config files.

## Today

Production reads `SMTP_ADDRESS`, `SMTP_PORT`, `SMTP_USERNAME`, `SMTP_PASSWORD` and `MAIL_FROM` from the environment (`console/config/environments/production.rb`, `ApplicationMailer`). `deploy/setup-gcp` asks for them and writes them to `deploy/.env`, the password in plain text, and the compose file passes them to Console. Changing them means editing `.env` and restarting. `InstanceSetting` holds the install's other settings, set with `bin/rails instance_settings:set`; it has no UI, because Console has no instance-admin role.

## What changes

**The settings live on the `InstanceSetting` row.** New columns: SMTP host, port (default 587), username, password, and the sender (From). There's no host means no mail, as today.

**The password is encrypted.** It uses Active Record Encryption (`encrypts`, non-deterministic). Its keys come from `SECRET_KEY_BASE` through `Rails.application.key_generator`, so there's no new env var or credentials file. The password is write-only: the page and MCP only say whether one is saved, and offer to replace or clear it. If it can't be decrypted (because `SECRET_KEY_BASE` changed), Console says email isn't set up and asks for the password again; it doesn't raise. What this protects: a copy of the SQLite file without `deploy/.env`, plus `inspect` and logs. It doesn't protect against a shell on the VM, where the key and the database sit side by side.

**Console reads them at send time.** `ApplicationMailer` takes From and the SMTP options from `InstanceSetting.current` on every message, when the delivery method is SMTP (production). Development keeps letter_opener and test keeps `:test`. A change applies to the next message in both the web and jobs processes, with no restart. With port 465 Console uses implicit TLS; with any other port, STARTTLS when the server offers it. Authentication is PLAIN when there's a username. Short open and read timeouts mean a wrong host fails within seconds instead of hanging. `ApplicationMailer.configured?` reads the row: production needs a host, a From, and a readable password when there's a username.

**Instance admins.** A new `users.instance_admin` flag. Only instance admins can see or change the email settings, through the page and over MCP. An Organization admin is not enough: the settings apply to every Organization.
- The first user to sign up on an install gets it.
- On an existing install, the migration gives it to the oldest user.
- In development and test, the seeded admin has it.
- An operator grants or removes it with a rake task (`bin/rails instance_admins:grant EMAIL=…` / `revoke`). There's no UI to grant it yet.

**Send a test email.** From the page and over MCP, to an address you type (the current user's by default). It's sent at once, not through a job, so the SMTP error comes back to whoever asked.

**Config files drop out.** `production.rb` stops reading `SMTP_*` and `MAIL_FROM`, and the compose file stops passing them. `deploy/setup-gcp`'s email stage becomes a note that says to set up email on Console's Email settings page once it's running, and its "Send a test email?" step goes. `CONSOLE_URL` stays in the environment: `setup-gcp` works it out from the HTTPS choice, and jobs need it for links. Upgrade: an install with `SMTP_*` in `deploy/.env` sends no email until an instance admin fills in the page. There's no import. `setup-gcp` doesn't delete the old lines, so the operator can still copy from them; the release note says to do that, then delete them.

## Out of scope

- The other instance settings (SQL queries, sign-up, Retention, log-all-reads) on the page or over MCP. They stay on the rake task for now. The page and the instance-admin role leave room for them.
- Encrypting Integration secrets (Slack webhook URLs, PagerDuty routing keys), which are plain JSON today. The same `encrypts` could cover them later.
- Bug 05's remaining items: whether the password reset and Monitor email paths say mail isn't set up.
- A UI to grant instance admin.

## Tickets

- [01](issues/01-email-settings-in-the-database.md): the columns, encryption, instance admins, the mailer reading the row, and config files dropped.
- [02](issues/02-email-settings-page.md): the Email settings page, with a test email.
- [03](issues/03-email-settings-over-mcp.md): MCP tools to read and change them and send a test email.
