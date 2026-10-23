# Email settings are set in Console by instance admins, the password encrypted with SECRET_KEY_BASE

The SMTP server (host, port, username, password) and the sender are columns of the `instance_settings` row, not `SMTP_*` and `MAIL_FROM` env vars. **Instance admins** set them on Console's Email settings page or over the MCP server, and `ApplicationMailer` reads them as it sends each message, so a change applies to the next one in the web and jobs processes with no restart. An instance admin is a user with `users.instance_admin`: the first user to sign up, or on an install that had users, the oldest; the operator grants and revokes it with `bin/rails instance_admins:grant`/`revoke`.

The SMTP password is encrypted with Active Record Encryption (non-deterministic). Its keys are derived from `SECRET_KEY_BASE` with `Rails.application.key_generator`, in one initializer, so there's no new secret to keep. The password is write-only: the page and MCP only say whether one is saved and readable, and offer to replace or clear it. One that can't be decrypted reads as unreadable, and Console says email isn't set up and asks for it again.

We chose this because changing email meant editing `deploy/.env` and restarting, and `deploy/.env` held the password in plain text. A copy of the SQLite file alone, a backup say, now doesn't give the password away, nor do `inspect` and logs.

## Amended: the other live settings are set in Console too, but Sign-up (2026-10-24)

Instance admins now change SQL queries, the default Retention and log-all-reads on Settings → Instance or over the MCP server (`get_instance_settings`, `update_instance_settings`), as they do email. These apply at once, so they needed no restart before either; the page saves needing a shell for them. The page and the MCP tools also show, read-only, Sign-up and what Console and EventStore read at start, each secret only as whether it's set.

Sign-up stays shell-only on purpose: the page shows whether it's open and the `bin/rails instance_settings:set` command that changes it, and neither the page nor MCP can change it. Opening it lets anyone who can reach Console make an account, so a stolen instance admin session or API token mustn't be able to open the install. `instance_settings:show` and `:set` keep working for every setting. ([spec](../../.scratch/instance-settings/spec.md))

## Considered options

- **Env vars as they are:** the operator needs a shell on the VM and a restart to change email, and the password sits in plain text in `deploy/.env`.
- **A separate key env var for encryption:** one more secret for `deploy/install` to write and the operator to keep, kept next to `SECRET_KEY_BASE` in the same `deploy/.env`, so it protects nothing more.
- **Plaintext in the database:** any copy of the SQLite file gives the password away.
- **Meta Organization admins as the editors:** the Meta Organization can be deleted, and its admins are an Organization's admins, chosen for SolidTrace's own Logs. The settings apply to every Organization, so they get their own role.

## Consequences

- A changed `SECRET_KEY_BASE` makes the saved password unreadable: email stops until an instance admin enters it again.
- A shell on the VM reads both `deploy/.env` and the database, so it can decrypt the password. That isn't what this protects against.
- Upgrading drops the env vars with no import: an install with `SMTP_*` in `deploy/.env` sends no email until an instance admin fills in the page. `deploy/setup-gcp` leaves those lines, for the operator to copy from, then delete.
- `CONSOLE_URL` stays in the environment: jobs need it for links, and `setup-gcp` works it out.
- The page and the role leave room for the other instance settings. Since the amendment above, every one that applies at once but Sign-up is set in Console too. Integration secrets could be encrypted the same way later.

Source: [.scratch/email-settings/spec.md](../../.scratch/email-settings/spec.md)
