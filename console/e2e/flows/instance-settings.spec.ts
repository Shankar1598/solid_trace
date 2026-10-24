import { test, expect } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import { resolve } from 'node:path'
import { MEMBER, ORG } from '../support/data'
import { watchErrors } from '../support/screens'

// Settings → Instance: the seeded admin, an instance admin, changes
// log-all-reads, log-MCP-reads and the default Retention, and sees them after
// a reload. Log MCP reads shows checked and can't change while Log all reads
// is on.
// Sign-up is read-only, and Console's environment and EventStore's config
// (from its GET /config) show with secrets only as set or not. A member who isn't an instance admin sees no Instance link. The
// settings are put back as they were after.

const rails = (code: string) =>
  execFileSync('mise', ['exec', '--', 'bin/rails', 'runner', code], {
    cwd: resolve(__dirname, '../..'), stdio: 'pipe', timeout: 120_000,
  }).toString().trim().split('\n').pop()!

let saved = '{}'

test.beforeAll(() => {
  saved = rails('print InstanceSetting.current.slice(:log_all_reads, :log_mcp_reads, :default_log_retention_days).to_json')
  rails('InstanceSetting.current.update!(log_all_reads: false, log_mcp_reads: true, default_log_retention_days: 30)')
})

test.afterAll(() => {
  rails(`InstanceSetting.current.update!(JSON.parse(${JSON.stringify(saved)}))`)
})

test('an instance admin changes log-all-reads, log-MCP-reads and the default Retention, and sees Sign-up read-only', async ({ page }) => {
  const errors = watchErrors(page)
  await page.goto(`/${ORG}/settings/organization`)
  await page.getByRole('link', { name: 'Instance', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Instance' })).toBeVisible()
  await expect(page.getByTestId('sql-queries-description')).toContainText('SQL queries let every member')

  const logAllReads = page.getByRole('checkbox', { name: 'Log all reads' })
  const logMcpReads = page.getByRole('checkbox', { name: 'Log MCP reads' })
  await expect(logAllReads).not.toBeChecked()
  await expect(logMcpReads).toBeChecked()
  await expect(logMcpReads).toBeEnabled()

  await page.getByLabel('Default Retention (days)').fill('0')
  await page.getByRole('button', { name: 'Save changes' }).click()
  await expect(page.getByText('must be greater than 0')).toBeVisible()

  await logMcpReads.click()
  await expect(logMcpReads).not.toBeChecked()
  await logAllReads.click()
  await expect(logMcpReads).toBeChecked()
  await expect(logMcpReads).toBeDisabled()
  await page.getByLabel('Default Retention (days)').fill('21')
  await page.getByRole('button', { name: 'Save changes' }).click()
  await expect(page.getByText('Instance settings saved')).toBeVisible()

  await page.reload()
  await expect(logAllReads).toBeChecked()
  await expect(logMcpReads).toBeChecked()
  await expect(logMcpReads).toBeDisabled()
  await expect(page.getByLabel('Default Retention (days)')).toHaveValue('21')

  // Log MCP reads was saved off, which shows once Log all reads is off.
  await logAllReads.click()
  await expect(logMcpReads).not.toBeChecked()
  await expect(logMcpReads).toBeEnabled()
  await page.getByRole('button', { name: 'Save changes' }).click()
  await expect(page.getByText('Instance settings saved')).toBeVisible()

  await page.reload()
  await expect(logAllReads).not.toBeChecked()
  await expect(logMcpReads).not.toBeChecked()

  const signUp = page.getByTestId('sign-up')
  await expect(signUp).toContainText('bin/rails instance_settings:set SIGN_UP_OPEN=true|false')
  await expect(signUp.getByRole('checkbox')).toHaveCount(0)
  await expect(signUp.getByRole('textbox')).toHaveCount(0)

  const consoleGroup = page.getByRole('region', { name: 'Console' })
  await expect(consoleGroup.getByTestId('setting-EVENT_STORE_URL')).toHaveText(/^http/)
  await expect(consoleGroup.getByTestId('setting-SECRET_KEY_BASE')).toHaveText(/^(Set|Not set)$/)

  const eventStoreGroup = page.getByRole('region', { name: 'EventStore' })
  await expect(eventStoreGroup.getByText('clickhouse.address', { exact: true })).toBeVisible()
  await expect(eventStoreGroup.getByTestId('setting-clickhouse.address')).toHaveText(/^[\w.-]+:\d+$/)
  await expect(eventStoreGroup.getByTestId('setting-logs_disk_limit_gb')).toContainText(' bytes)')
  await expect(eventStoreGroup.getByTestId('setting-SOLIDTRACE_CLICKHOUSE_PASSWORD')).toHaveText(/^(Set|Not set)$/)

  expect(errors).toEqual([])
})

test.describe('as a member', () => {
  test.use({ storageState: { cookies: [], origins: [] } })

  test('a member who isn\'t an instance admin sees no Instance link', async ({ page }) => {
    const errors = watchErrors(page)
    await page.goto('/login')
    await page.fill('input[type=email]', MEMBER.email)
    await page.fill('input[type=password]', MEMBER.password)
    await page.click('button[type=submit]')
    await page.waitForURL(url => !url.pathname.startsWith('/login'))

    await page.goto(`/${ORG}/settings/organization`)
    await expect(page.getByRole('link', { name: 'Account', exact: true })).toBeVisible()
    await expect(page.getByRole('link', { name: 'Instance', exact: true })).toHaveCount(0)

    expect(errors).toEqual([])
  })
})
