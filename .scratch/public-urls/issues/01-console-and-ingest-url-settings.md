# 01: Console URL and Ingest URL as instance settings

Spec: [../spec.md](../spec.md)

**What to build:** An Instance admin sets Console URL and Ingest URL on Settings → Instance or with `update_instance_settings`, and every link and DSN Console builds follows them at once: emails sent by jobs, invite links, MCP links, the MCP URL on User settings, DSNs, the Logs snippets and `get_project_setup`. Blank keeps today's behaviour (env var, then the request's address or Console's host at port 4000). A Console URL whose scheme differs from Console's boot-time HTTPS mode is refused.

**Blocked by:** none

**Status:** resolved

- [x] Migration adds two nullable string columns to `instance_settings`; both in `InstanceSetting::EDITABLE`, validated and normalised (trailing slash dropped, blank → nil)
- [x] One resolver each for Console URL and Ingest URL (setting → env → request/derived); every caller listed in the spec goes through it, and no other code reads `CONSOLE_URL`/`PUBLIC_INGEST_URL` or `default_url_options[:host]` for links
- [x] Mailers (Issue emails, invite, password reset, any other) build links from the resolver at send time, not from boot-time `default_url_options`
- [x] Saving a Console URL with the other scheme than the boot-time HTTPS mode is refused on the page and over MCP
- [x] Settings → Instance shows both fields with the fallback a blank one uses; `CONSOLE_URL`/`PUBLIC_INGEST_URL` leave the restart-to-change list as URLs; HTTPS shows there as a restart-to-change row
- [x] `get_instance_settings`/`update_instance_settings` include both; MCP docs updated
- [x] ADR recording URLs as settings and HTTPS staying a deploy choice (amends ADR 0010's "the host mustn't change" for Console)
- [x] Targeted Rails tests (626 runs), vitest (505), typecheck pass [x]; the instance-settings Playwright flow covers saving a URL and seeing a DSN follow it

## Comments

2026-11-10, implementer: built in 5 commits (6f318265..bd6ca9cb). Resolver `PublicUrls` (`console/app/services/public_urls.rb`): `console_url` = setting → `CONSOLE_URL` → request; `ingest_url` = setting → `PUBLIC_INGEST_URL` → resolved Console host at :4000. Migration `20261110120000_add_public_urls_to_instance_settings` (schema.rb edited by hand). Notifiers (Slack, PagerDuty, webhooks, monitor messages) also go through it. Settings → Instance drops the `CONSOLE_URL`/`PUBLIC_INGEST_URL` rows for an "HTTPS" row. A changed Console URL with the other scheme than boot-time `force_ssl` is refused; the rake task takes `CONSOLE_URL_SETTING`/`INGEST_URL_SETTING`. ADR 0017. Differences from the spec: with `CONSOLE_URL` set, invite/MCP links and the derived ingest host follow it rather than the request, before any save. Left: the copy-link buttons use `window.location.origin` (the page you're on). Glossary candidates: Console URL, Ingest URL.

2026-11-10, implementer: cc156a7a keeps `OrganizationMailer#invite_link_email` accepting the old `console_url:` keyword, so invite emails queued before the deploy still send (test/mailers/organization_mailer_test.rb, 2 runs pass).

2026-11-11, merger: merged into `feature/two-factor` on 2026-11-11 by the user's choice, so Two-factor 05 (Passkeys) builds on the Console URL setting. The instance-settings flow passed 4 of 4 on `feature/public-urls`, after c4466479 loosened its DSN check to the dev Project's key (`testkey123`, not 32 hex); the DSN did follow the Ingest URL. On the merge, `require_two_factor_test.rb` now passes `request_url:` to `MemberInvite`; typecheck and 352 Rails runs (instance settings, two-factor, mailers, invites, `test/mcp`, `test/integration`) pass.
