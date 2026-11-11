import { test, expect } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import { resolve } from 'node:path'
import { ORG } from '../support/data'
import { watchErrors } from '../support/screens'

// Passkeys with Chromium's virtual authenticator, over CDP: on
// http://localhost, where Passkeys are offered once the Console URL says so,
// add one on Settings → User, sign out, and sign in with it alone: from the
// email field's autofill, which the virtual authenticator answers as soon as
// the page asks, then, in a browser without autofill, from "Sign in with a
// passkey", with Remember me. The run sets the Console URL to the flow's own address and
// puts back what was there afterwards. Each run makes its own user, a member
// of the seed Organization so that signing in lands on its Overview.

const email = `flow-passkeys-${Date.now()}@solidtrace.local`
const password = 'flow-password-1'
const consoleUrl = new URL(process.env.E2E_BASE_URL ?? 'http://localhost:4001').origin
const day = 24 * 3600
let previousConsoleUrl = ''

const rails = (code: string) =>
  execFileSync('mise', ['exec', '--', 'bin/rails', 'runner', code], {
    cwd: resolve(__dirname, '../..'), stdio: 'pipe', timeout: 120_000,
  }).toString().trim()

test.use({ storageState: { cookies: [], origins: [] } })

test.beforeAll(() => {
  previousConsoleUrl = rails(`print InstanceSetting.current.console_url.to_s; ` +
    `InstanceSetting.current.update!(console_url: ${JSON.stringify(consoleUrl)}); ` +
    `user = User.create!(name: "Flow Passkeys", email: ${JSON.stringify(email)}, password: ${JSON.stringify(password)}); ` +
    `OrganizationUser.create!(organization: Organization.find_by!(slug: ${JSON.stringify(ORG)}), user: user, role: :member)`)
})

test.afterAll(() => {
  rails(`InstanceSetting.current.update!(console_url: ${previousConsoleUrl ? JSON.stringify(previousConsoleUrl) : 'nil'}); ` +
    `User.where(email: ${JSON.stringify(email)}).each { |user| OrganizationUser.where(user: user).delete_all; user.destroy! }`)
})

test('a Passkey is added on Settings → User and signs in alone, with Remember me', async ({ page }) => {
  test.setTimeout(120_000)
  const errors = watchErrors(page)

  const cdp = await page.context().newCDPSession(page)
  await cdp.send('WebAuthn.enable')
  await cdp.send('WebAuthn.addVirtualAuthenticator', {
    options: {
      protocol: 'ctap2', transport: 'internal', hasResidentKey: true, hasUserVerification: true,
      isUserVerified: true, automaticPresenceSimulation: true,
    },
  })

  await page.goto('/login')
  await page.getByLabel('Email').fill(email)
  await page.getByLabel('Password').fill(password)
  await page.getByRole('button', { name: 'Log in' }).click()
  await page.waitForURL(url => url.pathname === `/${ORG}/overview`)

  await page.goto('/user_settings')
  const card = page.getByTestId('passkeys')
  await expect(card.getByText('Sign in with your fingerprint, face or PIN instead of your password.')).toBeVisible()
  // Named by default from the browser and OS.
  await expect(card.getByLabel('Name')).toHaveValue(/^Chrome on /)
  await card.getByLabel('Name').fill('Flow laptop')
  await card.getByRole('button', { name: 'Add a passkey' }).click()
  await expect(page.getByText('Passkey added.')).toBeVisible()
  const item = card.getByRole('listitem', { name: 'Flow laptop' })
  await expect(item.getByText('Never used')).toBeVisible()

  const sessionLasts = async (signedInAt: number) => {
    const cookie = (await page.context().cookies()).find(c => c.name === 'session_id')!
    return cookie.expires - signedInAt
  }

  // No email, no password: the autofill's Passkey alone, without Remember me.
  await page.goto(`/${ORG}/overview`)
  let signedInAt = Date.now() / 1000
  await page.getByTitle('Sign out').click()
  await page.waitForURL(url => url.pathname === '/login')
  await page.waitForURL(url => url.pathname === `/${ORG}/overview`)
  expect(Math.abs(await sessionLasts(signedInAt) - day)).toBeLessThan(60)

  // A browser without autofill, opening the sign-in page afresh: the button,
  // with Remember me.
  await page.addInitScript(() => {
    PublicKeyCredential.isConditionalMediationAvailable = async () => false
  })
  await page.context().clearCookies()
  await page.goto('/login')
  await page.getByRole('checkbox', { name: 'Remember me' }).click()
  signedInAt = Date.now() / 1000
  await page.getByRole('button', { name: 'Sign in with a passkey' }).click()
  await page.waitForURL(url => url.pathname === `/${ORG}/overview`)
  expect(Math.abs(await sessionLasts(signedInAt) - 7 * day)).toBeLessThan(60)

  await page.goto('/user_settings')
  await expect(card.getByRole('listitem', { name: 'Flow laptop' }).getByText(/Last used/)).toBeVisible()

  expect(errors).toEqual([])
})
