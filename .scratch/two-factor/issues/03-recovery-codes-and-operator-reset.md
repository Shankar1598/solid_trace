# 03: Recovery codes and the operator reset

Spec: [../spec.md](../spec.md) (Two-factor: Recovery codes, lost factors)

**What to build:** When a user first gets Two-factor they see 10 Recovery codes once, to copy or download. A Recovery code works once, at the second step or as proof when removing a factor or making a new set. Settings → User says how many are left. A user who has lost everything asks the operator, who runs `bin/rails users:reset_two_factor EMAIL`.

**Blocked by:** 02

**Status:** resolved

- [x] Getting Two-factor makes a set of 10 Recovery codes, shown once with copy and download as a text file; Console keeps only digests
- [x] Settings → User shows how many are left, and "Make new Recovery codes" (needs a code or Recovery code) replaces the set and shows the new one once
- [x] The second step accepts a Recovery code once, through 02's one place; a Recovery code sign-in's notice says how many are left
- [x] A Recovery code is accepted as proof for removing a factor
- [x] Removing the last factor deletes the Recovery codes
- [x] `bin/rails users:reset_two_factor EMAIL` removes the Authenticator app and the Recovery codes and ends the user's Sessions; documented in the install docs
- [x] Rails integration tests: shown once, used once, count, new set, removal with one; the rake task as the existing rake tasks are tested
- [x] The Playwright `two-factor` flow also signs in with a Recovery code

## Comments

- The operator's command follows the existing rake tasks' convention: `bin/rails users:reset_two_factor EMAIL=<their email>` (a bare `EMAIL` argument would be read as another task). Passkeys join the reset in 06 (`User#reset_two_factor`).
- A Recovery code is 16 characters from a 32-letter alphabet (80 bits), shown as `xxxx-xxxx-xxxx-xxxx`, kept as a plain SHA-256 digest: long enough that no key or slow hash is needed, and it survives a changed `SECRET_KEY_BASE`, when Recovery codes are how users get back in. Case, spaces and dashes don't matter when typed.
- A new set is shown once on its own page, `/recovery_codes` (RecoveryCodesController#show), from the flash (the encrypted session cookie) for the one request after it's made; Continue goes back to Settings → User or the Organization set-up came from. `POST /recovery_codes` makes a new set for a second-step answer, rate-limited as sign-in is.
- `SecondStepFields` shows one field at a time, the app's code or (after "Use a Recovery code instead") a Recovery code, so the second step, removing the app and making a new set all take either.
- 02's tests now expect confirming the app to go by `/recovery_codes`; its helpers moved to `test/support/two_factor_test_helper.rb`, shared with `recovery_codes_test.rb`.
