import { test, expect } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import { resolve } from 'node:path'
import { watchErrors } from '../support/screens'

// On an install with users, sign-up is closed unless the operator opens it:
// the sign-in page offers none, and the sign-up page says to ask for an
// Invite. The dev database has its seeded users, so only the setting matters.

const rails = (code: string) =>
  execFileSync('mise', ['exec', '--', 'bin/rails', 'runner', code], {
    cwd: resolve(__dirname, '../..'), stdio: 'pipe', timeout: 120_000,
  }).toString()

let wasOpen = 'false'

test.use({ storageState: { cookies: [], origins: [] } })

test.beforeAll(() => {
  wasOpen = rails('print InstanceSetting.current.sign_up_open?').trim().split('\n').pop()!
  rails('InstanceSetting.current.update!(sign_up_open: false)')
})

test.afterAll(() => {
  rails(`InstanceSetting.current.update!(sign_up_open: ${wasOpen === 'true'})`)
})

test('with sign-up closed, sign-in offers none and the sign-up page says to ask for an Invite', async ({ page }) => {
  const errors = watchErrors(page)
  await page.goto('/login')
  await expect(page.getByRole('button', { name: 'Log in' })).toBeVisible()
  await expect(page.getByRole('link', { name: 'Sign up' })).toHaveCount(0)

  await page.goto('/registration/new')
  await expect(page.getByRole('heading', { name: 'Sign-up is closed' })).toBeVisible()
  await expect(page.getByText('Ask an admin of your Organization to invite you.')).toBeVisible()
  await expect(page.getByLabel('Email')).toHaveCount(0)

  await page.getByRole('link', { name: 'Log in' }).click()
  await page.waitForURL(url => url.pathname === '/login')
  expect(errors).toEqual([])
})
