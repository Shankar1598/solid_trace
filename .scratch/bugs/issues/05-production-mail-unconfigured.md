# 05: Production has no mail settings

**Status:** needs-triage
**Area:** Console
**Found:** 2026-10-18, while building the password reset (bugs 02)

## What happens

`console/config/environments/production.rb` still has the Rails defaults:

- `config.action_mailer.default_url_options = { host: "example.com" }`: every link in an email points at `example.com`.
- The SMTP settings are commented out, so nothing sends on a production install. It goes to Rails' default SMTP on localhost:25, which a container won't have.
- `ApplicationMailer` sends from `from@example.com`.

Nothing reads these from the environment, unlike `PUBLIC_INGEST_URL` or `EVENT_STORE_URL`. Every email Console sends is affected:

- **Invitations.** A new user's invite carries their only password, so a new member can't sign in at all.
- **Issue emails and Monitor emails.**
- **The password reset link** from bugs 02.

`default_url_options[:host]` also builds the Console links in Slack and webhook messages (`notifiers/monitor_message.rb:207`, `notifiers/http_delivery.rb:60`), so those point at `example.com` in production too.

Development uses letter_opener with `localhost:3000`, and the tests use `example.com`, so nothing here shows up before production.

## To decide

What to call the settings, and which are needed. A proposal in the style of the existing ones:

- `CONSOLE_URL`: Console's public URL, for links in emails and Slack. It could fall back to the request's host where there is one, but jobs have none.
- `SMTP_ADDRESS`, `SMTP_PORT`, `SMTP_USERNAME`, `SMTP_PASSWORD`, `SMTP_AUTHENTICATION`: unset means no mail. Then the invite, reset and Monitor email paths should say mail isn't set up, rather than claim it was sent.
- `MAIL_FROM`: the sender.

Then document them next to `PUBLIC_INGEST_URL`, and add a test that production reads them.

## Comments

### 2026-10-18

`feb0cdc` (`feature/logs-v2`, merged into `main`) does most of this. Production reads `CONSOLE_URL`, `MAIL_FROM` and `SMTP_*` from the environment, the compose file passes them to Console, and `deploy/setup` asks for them. Not checked against this ticket yet: whether the invite, reset and Monitor email paths say mail isn't set up when SMTP is unset, and whether a test checks that production reads the settings.

### 2026-10-22

On `feature/email-settings`, SMTP and the sender move from env vars to instance settings that instance admins set in Console (Settings → Email, or over MCP), and `ApplicationMailer.configured?` reads them. Production no longer reads `SMTP_*` or `MAIL_FROM`; `CONSOLE_URL` stays in the environment. Still not checked against this ticket: whether the invite, reset and Monitor email paths say mail isn't set up.
