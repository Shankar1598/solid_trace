# 01: Email settings live in the database

**Status:** resolved
**Spec:** [spec.md](../spec.md)

**What to build:** SMTP settings and the sender kept on `InstanceSetting`, with the password encrypted. `ApplicationMailer` reads them at send time. Instance admins exist. The env vars and `setup-gcp`'s email stage go.

- [x] Migration: `instance_settings` gains `smtp_address`, `smtp_port` (default 587), `smtp_username`, `smtp_password` (text, holds ciphertext) and `mail_from`. `users` gains `instance_admin` (boolean, default false, not null). The migration sets it on the oldest user (by `created_at`, `id`), if there is one. EventStore reads `instance_settings` by named columns (`event_store/storage/console_settings.go`), so new columns don't touch it; confirm with a grep.
- [x] Active Record Encryption keys derived from `SECRET_KEY_BASE` with `Rails.application.key_generator`, in one initializer, with no new env var or credentials. It must also boot under `SECRET_KEY_BASE_DUMMY` (the image's `assets:precompile`). `InstanceSetting` `encrypts :smtp_password`.
- [x] `InstanceSetting` methods, and nothing past what the mailer, page and MCP need:
  - the SMTP options hash, or nil when email isn't set up;
  - whether email is set up;
  - whether a password is saved, and whether it's readable. A decryption error counts as unreadable: rescue it, don't raise.

  Validations: the port is an integer from 1 to 65535. A host needs a From. From parses as an address (`Mail::Address`), with or without a display name.
- [x] Options: `address`, `port`, `user_name`/`password` and `authentication: :plain` when there's a username, `tls: true` on 465 (otherwise `enable_starttls_auto`), and `open_timeout`/`read_timeout` of about 10 s.
- [x] `ApplicationMailer`: From comes from the row (keep a placeholder when unset, as today). When the delivery method is `:smtp`, it sends with the row's options (for example a `delivery_method_options` default given as a lambda). `configured?` reads the row in production and stays true in development and test.
- [x] Remove the `SMTP_*` block from `config/environments/production.rb` (keep `default_url_options` from `CONSOLE_URL`) and `MAIL_FROM`/`SMTP_*` from `deploy/docker-compose.yml`. In `deploy/setup-gcp`, the email stage becomes a short note pointing at Console's Email settings page, and the "Send a test email?" step goes. Existing `.env` lines aren't deleted. Update the README's `setup-gcp` paragraph.
- [x] Instance admins: the first user to sign up (the `!User.exists?` path in registrations) gets `instance_admin`. The development and test seeds give it to the seeded admin. A rake task, `instance_admins:grant EMAIL=…`, `instance_admins:revoke EMAIL=…` and `instance_admins:list`, sits next to `lib/tasks/instance_settings.rake`. Update the comments in `instance_setting.rb` and that rake file that say there's no instance-admin role or UI.
- [x] Docs:
  - A `GLOSSARY.md` entry for **Instance admin**, and a mention in the Sign-up and Meta Organization entries if it fits.
  - ADR `docs/adr/0015-email-settings-in-console-encrypted-with-secret-key-base.md`: install settings set in Console by instance admins, not the environment; the password encrypted with keys derived from `SECRET_KEY_BASE`. Name it 0015, because `feature/dashboard-filters` already holds 0014. Considered options: env vars as they are, a separate key env var, plaintext, Meta Organization admins as the editors. Consequences: a changed `SECRET_KEY_BASE` means re-entering the password, and a shell on the VM reads both.
- [x] Tests:
  - the column in the database holds ciphertext, not the password;
  - a password encrypted under another key reads as unreadable, and email isn't set up;
  - the options for 587 and 465, with and without a username;
  - the mailer sends with the row's options and From (assert on the delivery method's settings with `:smtp` in the test);
  - the first sign-up gets `instance_admin` and the second doesn't;
  - the migration's oldest-user rule, if it's cheap to test; otherwise check it by hand and say so;
  - the rake task.

## Comments

**2026-10-22, implementer (feature/email-settings).** Built as asked. Calls made where the ticket was silent:
- Three migrations: the `instance_settings` columns, the `users.instance_admin` column, and a data-only `MakeOldestUserInstanceAdmin`, so the oldest-user rule is tested by calling its `up` (`test/migrations/make_oldest_user_instance_admin_test.rb`), as `NormalizeUserEmails` is.
- `InstanceSetting#email_problem` says what's missing ("No SMTP host: Console sends no email", "No sender: …", "No SMTP password for the username", or "The SMTP password can't be read, as Console's secret changed: enter it again"); `email_set_up?` is it being nil. The page's status line and MCP use it.
- A username with no saved password counts as not set up, as the spec asks for a readable password when there's a username.
- Host, username and From are trimmed, and blank becomes nil. Clearing the username clears the password in the model (`before_save`), so the page and MCP both get it.
- From is checked with `Mail::AddressList`: exactly one address with a local part and a domain, so `a@x.com, b@x.com` is refused (`Mail::Address` alone takes the first).
- `ApplicationMailer` takes From and `delivery_method_options` as lambdas; the options are the row's only when the delivery method is `:smtp`. With `:smtp` and email not set up, Action Mailer's own defaults (localhost:25) apply, as before; callers check `configured?`.
- `setup-gcp` keeps an "Email" stage (so the stage count stays 9) that says where the page is and, when `deploy/.env` still has `SMTP_ADDRESS`, says to copy the old lines into the page and delete them.
- The README also says how to grant instance admin. The GLOSSARY gains a Relationships line for it.
- Checked: production boots under `SECRET_KEY_BASE_DUMMY=1` with the encryption keys set. EventStore reads `instance_settings` by named columns only (`event_store/storage/console_settings.go`), and doesn't read `users`.
- In development, `InstanceSetting#inspect` raises on an unreadable password, as `inspect` reads every attribute; production inspects only `id`. Left as is.
