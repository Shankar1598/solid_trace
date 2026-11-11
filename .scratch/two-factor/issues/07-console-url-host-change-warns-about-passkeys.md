# 07: Changing the Console URL's host warns about Passkeys

Spec: [../spec.md](../spec.md) (Changing the Console URL's host)

**What to build:** An Instance admin saving a Console URL with a different host, while Passkeys exist for the current one, is told how many Passkeys, of how many users, would stop working and must confirm. Over MCP, `update_instance_settings` refuses such a change unless called with `confirm_passkeys_stop_working: true`, and says the counts. `bin/rails instance_settings:set` doesn't ask.

**Blocked by:** 05

**Status:** ready-for-agent

- [ ] Settings → Instance shows a confirmation with the counts before saving such a change; no confirmation when the host is unchanged or no Passkeys exist for it
- [ ] `update_instance_settings` refuses such a change without `confirm_passkeys_stop_working: true`, naming the counts; the MCP docs describe the argument
- [ ] `bin/rails instance_settings:set` changes the host without asking
- [ ] Rails integration tests for the page and the MCP tool
- [ ] Playwright: the instance-settings flow changes the host, sees the confirmation, and cancels
