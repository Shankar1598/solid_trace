import { test, expect } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import { resolve } from 'node:path'
import { watchErrors } from '../support/screens'

// "Forgot your password?" from the sign-in page: ask for a link, open it,
// choose a new password, and sign in with it. The link comes from rails
// runner, as the flow can't read the email. Each run makes its own user.

const email = `flow-reset-${Date.now()}@solidtrace.local`

const rails = (code: string) =>
  execFileSync('mise', ['exec', '--', 'bin/rails', 'runner', code], {
    cwd: resolve(__dirname, '../..'), stdio: 'pipe', timeout: 120_000,
  }).toString().trim()

test.use({ storageState: { cookies: [], origins: [] } })

test.beforeAll(() => {
  rails(`User.create!(name: "Flow Reset", email: ${JSON.stringify(email)}, password: "old-password-1")`)
})

test.afterAll(() => {
  rails(`User.where(email: ${JSON.stringify(email)}).destroy_all`)
})

test('a forgotten password is reset through its link, and the new one signs in', async ({ page }) => {
  const errors = watchErrors(page)
  await page.goto('/login')
  await page.getByRole('link', { name: 'Forgot your password?' }).click()
  await expect(page.getByText('Reset your password')).toBeVisible()

  await page.getByLabel('Email').fill(email.toUpperCase())
  await page.getByRole('button', { name: 'Send reset link' }).click()
  await page.waitForURL(url => url.pathname === '/login')
  await expect(page.getByText('If a user has that email, it has been sent a link to reset the password.')).toBeVisible()

  const token = rails(`print User.find_by!(email: ${JSON.stringify(email)}).password_reset_token`).split('\n').pop()!
  await page.goto(`/passwords/${encodeURIComponent(token)}/edit`)
  await page.getByLabel('New password', { exact: true }).fill('new-password-1')
  await page.getByLabel('Confirm new password').fill('other')
  await page.getByRole('button', { name: 'Save password' }).click()
  await expect(page.getByText("doesn't match Password")).toBeVisible()

  await page.getByLabel('New password', { exact: true }).fill('new-password-1')
  await page.getByLabel('Confirm new password').fill('new-password-1')
  await page.getByRole('button', { name: 'Save password' }).click()
  await page.waitForURL(url => url.pathname === '/login')
  await expect(page.getByText('Password reset. Sign in with the new one.')).toBeVisible()

  await page.fill('input[type=email]', email)
  await page.fill('input[type=password]', 'new-password-1')
  await page.click('button[type=submit]')
  await page.waitForURL(url => !url.pathname.startsWith('/login'))
  expect(errors).toEqual([])
})
