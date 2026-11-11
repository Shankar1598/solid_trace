import { test, expect } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import { resolve } from 'node:path'
import { ORG } from '../support/data'
import { watchErrors } from '../support/screens'
import { codeAfter } from '../support/totp'

// Two-factor with an Authenticator app: set it up on Settings → User with a
// code computed from the secret the page shows, sign out, sign in with the
// password and a code, then remove it with a code. Each run makes its own
// user, a member of the seed Organization so that signing in lands on its
// Overview; the seeded admin never gets Two-factor.

const email = `flow-two-factor-${Date.now()}@solidtrace.local`
const password = 'flow-password-1'

const rails = (code: string) =>
  execFileSync('mise', ['exec', '--', 'bin/rails', 'runner', code], {
    cwd: resolve(__dirname, '../..'), stdio: 'pipe', timeout: 120_000,
  }).toString().trim()

test.use({ storageState: { cookies: [], origins: [] } })

test.beforeAll(() => {
  rails(`user = User.create!(name: "Flow Two-factor", email: ${JSON.stringify(email)}, password: ${JSON.stringify(password)}); ` +
    `OrganizationUser.create!(organization: Organization.find_by!(slug: ${JSON.stringify(ORG)}), user: user, role: :member)`)
})

test.afterAll(() => {
  rails(`User.where(email: ${JSON.stringify(email)}).each { |user| OrganizationUser.where(user: user).delete_all; user.destroy! }`)
})

test('an authenticator app is set up with a code, asked for after the password, and removed with a code', async ({ page }) => {
  test.setTimeout(120_000)
  const errors = watchErrors(page)
  const signIn = async () => {
    await page.goto('/login')
    await page.fill('input[type=email]', email)
    await page.fill('input[type=password]', password)
    await page.click('button[type=submit]')
  }

  await signIn()
  await page.waitForURL(url => url.pathname === `/${ORG}/overview`)

  await page.goto('/user_settings')
  await page.getByRole('link', { name: 'Set up an authenticator app' }).click()
  await expect(page.getByRole('heading', { name: 'Set up Two-factor' })).toBeVisible()
  await expect(page.getByRole('img', { name: 'QR code for your authenticator app' }).locator('svg')).toBeVisible()
  const secret = (await page.getByLabel('Secret key').textContent())!.replace(/\s/g, '')

  // A wrong code doesn't turn it on.
  await page.getByLabel('2. Enter the code the app shows').fill('000000')
  await page.getByRole('button', { name: 'Turn on Two-factor' }).click()
  await expect(page.getByText("That code didn't work. Try the newest one from your app.")).toBeVisible()

  let used = await codeAfter(secret, -1)
  await page.getByLabel('2. Enter the code the app shows').fill(used.code)
  await page.getByRole('button', { name: 'Turn on Two-factor' }).click()
  await page.waitForURL(url => url.pathname === '/user_settings')
  await expect(page.getByText('Two-factor is on.')).toBeVisible()
  await expect(page.getByText(/^On: signing in asks for a code/)).toBeVisible()

  await page.getByTitle('Sign out').click()
  await page.waitForURL(url => url.pathname === '/login')

  await signIn()
  await page.waitForURL(url => url.pathname === '/login/second_step')
  used = await codeAfter(secret, used.step)
  await page.getByLabel('Code from your authenticator app').fill(used.code)
  await page.getByRole('button', { name: 'Continue' }).click()
  await page.waitForURL(url => url.pathname === `/${ORG}/overview`)

  await page.goto('/user_settings')
  used = await codeAfter(secret, used.step)
  await page.getByLabel('Code from your authenticator app').fill(used.code)
  await page.getByRole('button', { name: 'Remove authenticator app' }).click()
  await expect(page.getByText('Authenticator app removed. Two-factor is off.')).toBeVisible()
  await expect(page.getByRole('link', { name: 'Set up an authenticator app' })).toBeVisible()

  expect(errors).toEqual([])
})
