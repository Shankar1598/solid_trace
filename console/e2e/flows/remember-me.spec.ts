import { test, expect, type Page } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import { resolve } from 'node:path'
import { ORG } from '../support/data'
import { watchErrors } from '../support/screens'

// Remember me on the sign-in page: unticked, the Session's cookie expires 24
// hours from signing in; ticked, 7 days. Each run makes its own user, a
// member of the seed Organization so that signing in lands on its Overview.

const email = `flow-remember-${Date.now()}@solidtrace.local`
const password = 'remember-password-1'
const hour = 3600

const rails = (code: string) =>
  execFileSync('mise', ['exec', '--', 'bin/rails', 'runner', code], {
    cwd: resolve(__dirname, '../..'), stdio: 'pipe', timeout: 120_000,
  }).toString().trim()

test.use({ storageState: { cookies: [], origins: [] } })

test.beforeAll(() => {
  rails(`user = User.create!(name: "Flow Remember", email: ${JSON.stringify(email)}, password: ${JSON.stringify(password)}); ` +
    `OrganizationUser.create!(organization: Organization.find_by!(slug: ${JSON.stringify(ORG)}), user: user, role: :member)`)
})

test.afterAll(() => {
  rails(`User.where(email: ${JSON.stringify(email)}).each { |user| OrganizationUser.where(user: user).delete_all; user.destroy! }`)
})

async function signIn(page: Page, rememberMe: boolean) {
  await page.goto('/login')
  await page.getByLabel('Email').fill(email)
  await page.getByLabel('Password').fill(password)
  const box = page.getByRole('checkbox', { name: 'Remember me' })
  await expect(box).not.toBeChecked()
  if (rememberMe) await box.click()
  const signedInAt = Date.now() / 1000
  await page.getByRole('button', { name: 'Log in' }).click()
  await page.waitForURL(url => url.pathname === `/${ORG}/overview`, { waitUntil: 'commit' })
  const cookie = (await page.context().cookies()).find(c => c.name === 'session_id')!
  return cookie.expires - signedInAt
}

test('without Remember me the Session lasts 24 hours, with it 7 days', async ({ page }) => {
  const errors = watchErrors(page)

  const lasts = await signIn(page, false)
  expect(Math.abs(lasts - 24 * hour)).toBeLessThan(60)

  await page.context().clearCookies()
  const remembered = await signIn(page, true)
  expect(Math.abs(remembered - 7 * 24 * hour)).toBeLessThan(60)

  expect(errors).toEqual([])
})
