import { test, expect } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import { resolve } from 'node:path'
import { ADMIN, MEMBER, ORG } from '../support/data'
import { shoot, watchErrors } from '../support/screens'

// Settings → Email: the seeded admin, an instance admin, sets up the SMTP
// server and sends a test email, which development's letter_opener catches.
// A member who isn't an instance admin sees no Email link. The settings are
// put back as they were after.

const rails = (code: string) =>
  execFileSync('mise', ['exec', '--', 'bin/rails', 'runner', code], {
    cwd: resolve(__dirname, '../..'), stdio: 'pipe', timeout: 120_000,
  }).toString().trim()

const clearEmail = () =>
  rails('InstanceSetting.current.update!(smtp_address: nil, smtp_port: 587, smtp_username: nil, smtp_password: nil, mail_from: nil)')

test.beforeAll(clearEmail)
test.afterAll(clearEmail)

test('an instance admin sets up email, sees the password saved but not shown, and sends a test email', async ({ page }) => {
  const errors = watchErrors(page)
  await page.goto(`/${ORG}/settings/organization`)
  await page.getByRole('link', { name: 'Email', exact: true }).click()
  await expect(page.getByTestId('email-status')).toHaveText('No SMTP host: Console sends no email')
  await expect(page.getByRole('button', { name: 'Send test email' })).toBeDisabled()

  await page.getByLabel('Host', { exact: true }).fill('smtp.example.com')
  await page.getByLabel('Port', { exact: true }).fill('465')
  await page.getByLabel('Username', { exact: true }).fill('apikey')
  await page.getByLabel('Password', { exact: true }).fill('flow-s3cret')
  await page.getByLabel('Send from', { exact: true }).fill('SolidTrace <alerts@example.com>')
  await page.getByRole('button', { name: 'Save changes' }).click()
  await expect(page.getByText('Email settings saved')).toBeVisible()

  await page.reload()
  await expect(page.getByLabel('Host', { exact: true })).toHaveValue('smtp.example.com')
  await expect(page.getByLabel('Port', { exact: true })).toHaveValue('465')
  await expect(page.getByLabel('Password', { exact: true })).toHaveValue('')
  await expect(page.getByLabel('Password', { exact: true })).toHaveAttribute('placeholder', 'Saved — leave blank to keep')
  await expect(page.getByTestId('email-status')).toHaveText('Email is set up')
  expect(await page.content()).not.toContain('flow-s3cret')
  await shoot(page, 'email-settings', { fullPage: true })

  await expect(page.getByLabel('To', { exact: true })).toHaveValue(ADMIN.email)
  await page.getByRole('button', { name: 'Send test email' }).click()
  await expect(page.getByText(`Test email sent to ${ADMIN.email}`)).toBeVisible()

  expect(errors).toEqual([])
})

test.describe('as a member', () => {
  test.use({ storageState: { cookies: [], origins: [] } })

  test('a member who isn\'t an instance admin sees no Email link', async ({ page }) => {
    const errors = watchErrors(page)
    await page.goto('/login')
    await page.fill('input[type=email]', MEMBER.email)
    await page.fill('input[type=password]', MEMBER.password)
    await page.click('button[type=submit]')
    await page.waitForURL(url => !url.pathname.startsWith('/login'))

    await page.goto(`/${ORG}/settings/organization`)
    await expect(page.getByRole('link', { name: 'Account', exact: true })).toBeVisible()
    await expect(page.getByRole('link', { name: 'Email', exact: true })).toHaveCount(0)

    expect(errors).toEqual([])
  })
})
