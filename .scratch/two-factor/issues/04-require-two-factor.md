# 04: Require two-factor on an Organization

Spec: [../spec.md](../spec.md) (Require two-factor)

**What to build:** An Organization's admin who has Two-factor turns on Require two-factor on the Organization's settings page, after seeing how many members don't have it. From that moment, a member without Two-factor who opens any of its pages is taken to the set-up page, told which Organization requires it and returned there once set up, and their API tokens are refused on it over MCP. Their other Organizations work as before. A member can't remove their last factor while one of their Organizations requires it.

**Blocked by:** 02

**Status:** resolved

- [x] Organizations have Require two-factor, off by default
- [x] The Organization's settings page offers the switch only to an admin who has Two-factor, and shows the count of members without it before turning it on; no MCP tool reads or changes it
- [x] Every page of a requiring Organization sends a member without Two-factor to the set-up page, naming the Organization and returning there once set up; it applies at once to Sessions already signed in; pages outside an Organization still work
- [x] MCP calls on a requiring Organization from a user without Two-factor are refused with a message to set it up in Console; calls on their other Organizations work
- [x] An Invited member of a requiring Organization lands on the set-up page after setting their password from the Invite link
- [x] Removing the last factor is refused while any of the user's Organizations requires Two-factor, naming them
- [x] Rails integration tests: the switch's guard and count, page redirect and return, an already signed-in Session, MCP refusal per Organization, the Invite link, removal refused
- [x] Playwright `require-two-factor` flow: an admin turns it on, a member without Two-factor is taken to set-up, sets it up and reaches the Organization

## Comments

- The check is in `CurrentOrganization#set_current_org`. A browser request (an Inertia visit or a plain HTML load) is redirected to `/authenticator_app/new?org=<slug>`; an XHR or JSON request under the Organization gets a 403, so a poll doesn't follow a redirect to an HTML page. `/<org>/settings/user` redirects too, as it's one of the Organization's pages; `/user_settings` and the set-up page still work, and the set-up page now has a Sign out link.
- The set-up page's `return_organization` gains `requires_two_factor`; when it's true the page says "<Org> requires Two-factor." Its other prop shapes are unchanged.
- Removing a factor is decided in one place, `User::TwoFactor#remove_factor(answer) { change }` (private). It checks the second-step answer, makes the change, and, when no factor is left, either refuses with `User::TwoFactor::Required` (naming the requiring Organizations) or throws the Recovery codes away, all in one transaction. A refused removal therefore doesn't use up the code or Recovery code that was given. `remove_authenticator_app(answer)` goes through it; 06's Passkey removal should too. The operator's `reset_two_factor` doesn't, so Require two-factor never blocks it.
- The settings page's prop is `require_two_factor: { on, can_change, you_have_two_factor, members_without_two_factor }`. Invited members count as members without Two-factor. A change is a PUT of `organization[require_two_factor]`; anyone else who sends that param is refused with an alert, and nothing in the request is saved.
- Migration `20261111144000_add_require_two_factor_to_organizations` (03 has `…143000`).

