# Sign-in

A user signs in to Console with an email and a password, and arrives on their Organization's pages. A wrong password keeps the user on the login page. An admin sees the admin-only controls. A member sees the same pages, but cannot change them.

## Sub-features

- `signin-password`: the seed admin signs in and leaves `/login`.
- `signin-member`: the seed member signs in and sees the Organization's pages.
- `signin-roles`: a member sees a Project's Factory section read-only, and an admin can edit it.
- `signin-remember`: without Remember me a session lasts 24 hours, and with it 7 days (`remember-me.spec.ts`).
- `signin-two-factor`: an authenticator app asks for a code after the password (`two-factor.spec.ts`).

## How to get to it (user POV)

- Open `/login` (`/` sends a signed-out user there). Type the email and the password, then press the submit button.

## Driving it with verify

Preconditions:

- The baseline stack is up, and `"$V" doctor` is all `ok`.
- The sign-in rate limit is not used up: fewer than 1000 sign-ins for each address in the last 3 minutes (10 in production).

- **Admin signs in.** `"$V" snap --path /solid-trace-org/logs --name signin-admin`. The JSON line has `status` 200 and a `url` that does not start with `/login`. `signin-admin.aria.txt` has the navigation `Main` with the links `Logs` and `Dashboards`.
- **Member signs in.** `"$V" snap --path /solid-trace-org/projects/solid-trace-project --name signin-member --as member`. Status 200. The ARIA snapshot names the Project.
- **Roles on one page.** `"$V" flow -- e2e/flows/runs.spec.ts -g "only an Admin"`. The test passes: the admin's `#factory-repository` in `[data-section="factory"]` can be edited and saved, and the member's field is `readonly` and has no `Save` button.
- **Remember me.** `"$V" flow -- e2e/flows/remember-me.spec.ts`. The test passes.
- **Two-factor.** `"$V" flow -- e2e/flows/two-factor.spec.ts`. The test passes.

## Gotchas

- `snap` and every flow sign in again. After 1000 sign-ins in 3 minutes for one address in development (10 in production), the login page comes back with a rate-limit error. That is the limit, not a bug.
- Global setup saves the session of each worker's admin in `console/tmp/playwright-auth/` (`admin.json` for worker 0, `admin-wN.json` for worker N). Flows that need another user sign in with a new browser context (`signedInPage` in `e2e/support/factoryFlow.ts`).
- The seed passwords are public development values. They exist only because `db:seed` runs in development and test.
