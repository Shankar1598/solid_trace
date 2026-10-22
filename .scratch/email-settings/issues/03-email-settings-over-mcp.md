# 03: Email settings over MCP

**Status:** resolved
**Spec:** [spec.md](../spec.md)
**Blocked by:** 01

**What to build:** MCP tools for instance admins to read and change the email settings and send a test email, like the page.

- [x] `get_email_settings`: host, port, username, From, whether a password is saved and readable, and whether email is set up, ending with the Console link to the Email page, as other tools do (`mcp_tools/links.rb`). It never returns the password.
- [x] `update_email_settings`: the arguments are optional and only the ones given change. A password argument sets it, and `clear_password: true` clears it. It returns what `get_email_settings` returns, and validation errors come back as tool errors. The tool description says a password passed here goes through the agent's context, so an admin may prefer the page.
- [x] `send_test_email`: `to` defaults to the caller's email. It's sent synchronously and returns success or the SMTP error's message.
- [x] All three refuse anyone who isn't an instance admin, with a tool error ("Only an instance admin can …"), following how `base.rb` refuses non-admins. They take no Organization: the settings are install-wide. Check how `base.rb` handles tools without an Organization argument; `whoami` is one.
- [x] `whoami` says whether the caller is an instance admin.
- [x] Register the tools wherever `mcp_server.rb` lists them, and update the server instructions or tool counts if they name the tools (grep for `34 tools` or a list in the docs and spec).
- [x] Tests next to the existing MCP tool tests:
  - an instance admin reads and updates;
  - the password is never in a response;
  - partial updates;
  - refused for a non-instance-admin;
  - the test email's success and failure.

## Comments

**2026-10-22, implementer (feature/email-settings).** Built as asked. Calls made where the ticket was silent:
- The arguments and answer use the page's names: `smtp_address`, `smtp_port`, `smtp_username`, `smtp_password`, `clear_password`, `mail_from`. `get_email_settings` answers `EmailSettingsSerializer`'s fields plus `set_up` and `problem`. An empty `smtp_password` keeps the saved one, as on the page.
- The Email page lives under an Organization, so the tools' link is the page under the caller's first Organization by name, and there's no link for a caller in none.
- The tools never call `#organization`, so they work for a caller in any number of Organizations. `McpTools::EmailSettingsTool` holds the instance-admin check and the answer.
- `send_test_email` answers `{ sent: true, to: … }`, or a tool error "The test email wasn't sent: <message>", the same message as the page.
- No doc or test counts the tools. `docs/mcp.md` gains an "Install" group and a line on who may use them; the server instructions gain one line naming them.
- Found while building it: MCP tool calls' timing lines (`CallTimings.mcp_tool`) wrote every argument, so a password given to `update_email_settings` would have reached the Meta Organization's Logs. They now filter arguments with `Rails.application.config.filter_parameters`, as Rails does for request parameters. That list also has `email`, so `invite_member`'s `email` argument now reads `[FILTERED]` in those lines; `user_email` stays. Tested in `test/services/call_timings_test.rb`.
