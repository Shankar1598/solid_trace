# 07: Changing the Console URL's host warns about Passkeys

Spec: [../spec.md](../spec.md) (Changing the Console URL's host)

**What to build:** An Instance admin saving a Console URL with a different host, while Passkeys exist for the current one, is told how many Passkeys, of how many users, would stop working and must confirm. Over MCP, `update_instance_settings` refuses such a change unless called with `confirm_passkeys_stop_working: true`, and says the counts. `bin/rails instance_settings:set` doesn't ask.

**Blocked by:** 05

**Status:** resolved

- [x] Settings → Instance shows a confirmation with the counts before saving such a change; no confirmation when the host is unchanged or no Passkeys exist for it
- [x] `update_instance_settings` refuses such a change without `confirm_passkeys_stop_working: true`, naming the counts; the MCP docs describe the argument
- [x] `bin/rails instance_settings:set` changes the host without asking
- [x] Rails integration tests for the page and the MCP tool
- [x] Playwright: the instance-settings flow changes the host, sees the confirmation, and cancels

## Comments

- The check is server-side only (`InstanceSetting#update_editable(changes, confirm_passkeys_stop_working:)`, using `PasskeyOffer#stranded_by`): the page saves, and a refusal with the `passkeys_stop_working` error (the counts sentence) opens the confirmation, whose "Change the host" sends again with `confirm_passkeys_stop_working: true`. The page doesn't work out hosts or fallbacks itself. It asks only once everything else in the form is valid.
- "Another host" includes a Console URL where Passkeys aren't offered (blank with no `CONSOLE_URL`, an IP, http:// off localhost). The same host on another port doesn't ask: the relying-party ID is the host.
- `update_editable` takes the changes as a positional hash now; tests that passed braceless keywords got braces.
