# 03: Instance settings over MCP

Spec: [../spec.md](../spec.md)

**What to build:** An Instance admin's MCP client calls `get_instance_settings` and gets the same sections as Settings → Instance, from 01's answer builder, with a link to the page. That includes EventStore's group once 02 has landed. `update_instance_settings` changes SQL queries, the default Retention and log-all-reads, each optional. It can't change Sign-up or anything that needs a restart. Validation errors come back as tool errors. Both refuse a user who isn't an Instance admin, as the email settings tools do, and take no Organization. Both are registered on the MCP server and documented in the MCP docs, and the server instructions' Instance admin line names them.

**Blocked by:** 01

**Status:** resolved (merged into `feature/instance-settings` at 851dbdc; 24 MCP and integration test runs pass)

- [x] A user who isn't an Instance admin is refused by both tools
- [x] `get_instance_settings` answers the sections and no secret's value
- [x] `update_instance_settings` changes the three settings; a bad Retention is a tool error
- [x] A `sign_up_open` argument changes nothing
- [x] Both are in the MCP docs and the server instructions
- [x] MCP tool tests (like the email settings tools test) pass
