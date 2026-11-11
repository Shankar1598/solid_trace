import { test, expect, Page } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import { resolve } from 'node:path'
import { watchErrors } from '../support/screens'
import { codeAfter } from '../support/totp'

// Require two-factor: an admin who has Two-factor turns it on on the
// Organization's settings page; a member without it who signs in is taken
// to the set-up page, which names the Organization and lets them sign out,
// and once they set it up they reach the Organization. Each run makes its
// own Organization, its admin (with an Authenticator app of a known secret)
// and member; the seeded admin never gets Two-factor.

const run = Date.now()
const slug = `flow-require-2fa-${run}`
const orgName = `Flow Require ${run}`
const adminEmail = `flow-require-admin-${run}@solidtrace.local`
const memberEmail = `flow-require-member-${run}@solidtrace.local`
const password = 'flow-password-1'
const adminSecret = 'JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP'

const rails = (code: string) =>
  execFileSync('mise', ['exec', '--', 'bin/rails', 'runner', code], {
    cwd: resolve(__dirname, '../..'), stdio: 'pipe', timeout: 120_000,
  }).toString().trim()

test.use({ storageState: { cookies: [], origins: [] } })

test.beforeAll(() => {
  rails(`org = Organization.create!(name: ${JSON.stringify(orgName)}, slug: ${JSON.stringify(slug)}); ` +
    `admin = User.create!(name: "Flow Require Admin", email: ${JSON.stringify(adminEmail)}, password: ${JSON.stringify(password)}, ` +
    `authenticator_secret: ${JSON.stringify(adminSecret)}, authenticator_confirmed_at: Time.current); ` +
    `member = User.create!(name: "Flow Require Member", email: ${JSON.stringify(memberEmail)}, password: ${JSON.stringify(password)}); ` +
    `org.organization_users.create!(user: admin, role: :admin); org.organization_users.create!(user: member, role: :member)`)
})

test.afterAll(() => {
  rails(`emails = [${JSON.stringify(adminEmail)}, ${JSON.stringify(memberEmail)}]; ` +
    `OrganizationUser.where(user: User.where(email: emails)).delete_all; User.where(email: emails).destroy_all; ` +
    `Organization.where(slug: ${JSON.stringify(slug)}).destroy_all`)
})

const signIn = async (page: Page, email: string) => {
  await page.goto('/login')
  await page.fill('input[type=email]', email)
  await page.fill('input[type=password]', password)
  await page.click('button[type=submit]')
}

test('an admin requires Two-factor, and a member without it sets it up and reaches the Organization', async ({ page }) => {
  test.setTimeout(150_000)
  const errors = watchErrors(page)

  // The admin, with Two-factor, sees who'd be asked and turns it on.
  await signIn(page, adminEmail)
  await page.waitForURL(url => url.pathname === '/login/second_step')
  const adminCode = await codeAfter(adminSecret, -1)
  await page.getByLabel('Code from your authenticator app').fill(adminCode.code)
  await page.getByRole('button', { name: 'Continue' }).click()
  await page.waitForURL(url => url.pathname === `/${slug}/overview`)

  await page.goto(`/${slug}/settings/organization`)
  const card = page.getByRole('region', { name: 'Require two-factor' })
  await expect(card.getByText("1 member doesn’t have Two-factor yet, and would be asked to set it up.")).toBeVisible()
  await card.getByRole('button', { name: 'Require two-factor' }).click()
  await expect(card.getByText(/^On\./)).toBeVisible()
  await expect(card.getByRole('button', { name: 'Stop requiring two-factor' })).toBeVisible()
  await page.goto(`/${slug}/overview`)
  await page.getByTitle('Sign out').click()
  await page.waitForURL(url => url.pathname === '/login')

  // The member is taken to set-up, told why, and can sign out from there.
  await signIn(page, memberEmail)
  await page.waitForURL(url => url.pathname === '/authenticator_app/new' && url.searchParams.get('org') === slug)
  await expect(page.getByRole('status').filter({ hasText: `${orgName} requires Two-factor.` })).toBeVisible()
  await page.getByRole('button', { name: 'Sign out' }).click()
  await page.waitForURL(url => url.pathname === '/login')

  await signIn(page, memberEmail)
  await page.waitForURL(url => url.pathname === '/authenticator_app/new' && url.searchParams.get('org') === slug)
  const secret = (await page.getByLabel('Secret key').textContent())!.replace(/\s/g, '')
  const memberCode = await codeAfter(secret, -1)
  await page.getByLabel('2. Enter the code the app shows').fill(memberCode.code)
  await page.getByRole('button', { name: 'Turn on Two-factor' }).click()

  // The Recovery codes, shown once, then on to the Organization.
  await page.waitForURL(url => url.pathname === '/recovery_codes')
  await page.getByRole('link', { name: 'Continue' }).click()
  await page.waitForURL(url => url.pathname === `/${slug}/overview`)

  expect(errors).toEqual([])
})
