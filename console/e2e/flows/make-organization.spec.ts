import { test, expect } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import { resolve } from 'node:path'
import { watchErrors } from '../support/screens'

// Someone who signs up is in no Organization, so they make one, become its
// admin and land on its Issues. Each run opens sign-up, which the dev
// database's seeded users would otherwise close, signs up its own user and
// deletes it, with its Organization, after.

const stamp = Date.now()
const email = `flow-signup-${stamp}@solidtrace.local`
const name = `Flow Org ${stamp}`
const slug = `flow-org-${stamp}`

const rails = (code: string) =>
  execFileSync('mise', ['exec', '--', 'bin/rails', 'runner', code], {
    cwd: resolve(__dirname, '../..'), stdio: 'pipe', timeout: 120_000,
  }).toString()

let wasOpen = 'false'

test.use({ storageState: { cookies: [], origins: [] } })

test.beforeAll(() => {
  wasOpen = rails('print InstanceSetting.current.sign_up_open?').trim().split('\n').pop()!
  rails('InstanceSetting.current.update!(sign_up_open: true)')
})

test.afterAll(() => {
  rails(`InstanceSetting.current.update!(sign_up_open: ${wasOpen === 'true'})`)
  rails(`Organization.where(slug: ${JSON.stringify(slug)}).each { |org| OrganizationUser.where(organization: org).delete_all; org.destroy! }; ` +
    `User.where(email: ${JSON.stringify(email)}).each { |user| OrganizationUser.where(user: user).delete_all; user.destroy! }`)
})

test('signing up leads to making an Organization, and its Issues', async ({ page }) => {
  const errors = watchErrors(page)
  await page.goto('/registration/new')
  await page.getByLabel('Name').fill('Flow Sign-up')
  await page.getByLabel('Email').fill(email)
  await page.getByLabel('Password', { exact: true }).fill('password-1')
  await page.getByLabel(/confirm/i).fill('password-1')
  await page.getByRole('button', { name: /sign up|create/i }).click()

  await page.waitForURL(url => url.pathname === '/organizations/new')
  await expect(page.getByRole('heading', { name: 'Make an Organization' })).toBeVisible()
  await expect(page.getByText('Welcome! You have signed up successfully.')).toBeVisible()

  await page.getByLabel('Name').fill(name)
  await page.getByRole('button', { name: 'Make Organization' }).click()

  await page.waitForURL(url => url.pathname === `/${slug}/issues`)
  await expect(page.getByText(`${name} is made. Add a Project to send it Events and Logs.`)).toBeVisible()
  expect(errors).toEqual([])
})
