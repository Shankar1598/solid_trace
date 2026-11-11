# 04: Require two-factor on an Organization

Spec: [../spec.md](../spec.md) (Require two-factor)

**What to build:** An Organization's admin who has Two-factor turns on Require two-factor on the Organization's settings page, after seeing how many members don't have it. From that moment, a member without Two-factor who opens any of its pages is taken to the set-up page, told which Organization requires it and returned there once set up, and their API tokens are refused on it over MCP. Their other Organizations work as before. A member can't remove their last factor while one of their Organizations requires it.

**Blocked by:** 02

**Status:** ready-for-agent

- [ ] Organizations have Require two-factor, off by default
- [ ] The Organization's settings page offers the switch only to an admin who has Two-factor, and shows the count of members without it before turning it on; no MCP tool reads or changes it
- [ ] Every page of a requiring Organization sends a member without Two-factor to the set-up page, naming the Organization and returning there once set up; it applies at once to Sessions already signed in; pages outside an Organization still work
- [ ] MCP calls on a requiring Organization from a user without Two-factor are refused with a message to set it up in Console; calls on their other Organizations work
- [ ] An Invited member of a requiring Organization lands on the set-up page after setting their password from the Invite link
- [ ] Removing the last factor is refused while any of the user's Organizations requires Two-factor, naming them
- [ ] Rails integration tests: the switch's guard and count, page redirect and return, an already signed-in Session, MCP refusal per Organization, the Invite link, removal refused
- [ ] Playwright `require-two-factor` flow: an admin turns it on, a member without Two-factor is taken to set-up, sets it up and reaches the Organization
