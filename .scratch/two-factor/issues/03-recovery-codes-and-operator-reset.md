# 03: Recovery codes and the operator reset

Spec: [../spec.md](../spec.md) (Two-factor: Recovery codes, lost factors)

**What to build:** When a user first gets Two-factor they see 10 Recovery codes once, to copy or download. A Recovery code works once, at the second step or as proof when removing a factor or making a new set. Settings → User says how many are left. A user who has lost everything asks the operator, who runs `bin/rails users:reset_two_factor EMAIL`.

**Blocked by:** 02

**Status:** ready-for-agent

- [ ] Getting Two-factor makes a set of 10 Recovery codes, shown once with copy and download as a text file; Console keeps only digests
- [ ] Settings → User shows how many are left, and "Make new Recovery codes" (needs a code or Recovery code) replaces the set and shows the new one once
- [ ] The second step accepts a Recovery code once, through 02's one place; a Recovery code sign-in's notice says how many are left
- [ ] A Recovery code is accepted as proof for removing a factor
- [ ] Removing the last factor deletes the Recovery codes
- [ ] `bin/rails users:reset_two_factor EMAIL` removes the Authenticator app and the Recovery codes and ends the user's Sessions; documented in the install docs
- [ ] Rails integration tests: shown once, used once, count, new set, removal with one; the rake task as the existing rake tasks are tested
- [ ] The Playwright `two-factor` flow also signs in with a Recovery code
