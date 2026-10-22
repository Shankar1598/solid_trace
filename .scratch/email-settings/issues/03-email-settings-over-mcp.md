# 03: Email settings over MCP

**Status:** ready-for-agent
**Spec:** [spec.md](../spec.md)
**Blocked by:** 01

**What to build:** MCP tools for instance admins to read and change the email settings and send a test email, like the page.

- [ ] `get_email_settings`: host, port, username, From, whether a password is saved and readable, and whether email is set up, ending with the Console link to the Email page, as other tools do (`mcp_tools/links.rb`). It never returns the password.
- [ ] `update_email_settings`: the arguments are optional and only the ones given change. A password argument sets it, and `clear_password: true` clears it. It returns what `get_email_settings` returns, and validation errors come back as tool errors. The tool description says a password passed here goes through the agent's context, so an admin may prefer the page.
- [ ] `send_test_email`: `to` defaults to the caller's email. It's sent synchronously and returns success or the SMTP error's message.
- [ ] All three refuse anyone who isn't an instance admin, with a tool error ("Only an instance admin can …"), following how `base.rb` refuses non-admins. They take no Organization: the settings are install-wide. Check how `base.rb` handles tools without an Organization argument; `whoami` is one.
- [ ] `whoami` says whether the caller is an instance admin.
- [ ] Register the tools wherever `mcp_server.rb` lists them, and update the server instructions or tool counts if they name the tools (grep for `34 tools` or a list in the docs and spec).
- [ ] Tests next to the existing MCP tool tests:
  - an instance admin reads and updates;
  - the password is never in a response;
  - partial updates;
  - refused for a non-instance-admin;
  - the test email's success and failure.
