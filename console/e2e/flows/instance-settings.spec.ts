import { test, expect } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import { resolve } from 'node:path'
import { MEMBER, ORG, PROJECT } from '../support/data'
import { watchErrors } from '../support/screens'

// Settings → Instance: the seeded admin, an instance admin, changes
// log-all-reads, log-MCP-reads, the default Retention, whether Console
// sends its own Issues and traces, the text-match budget and the Log read threads, and sees them after a reload. Log MCP reads shows checked and can't change while Log all reads
// is on. Changing the hours on local disk asks first, saying what it does:
// Cancel leaves it unsaved, confirming saves it. Without a bucket it can't
// change, and says it does nothing.
// Sign-up is read-only, and Console's environment and EventStore's config
// (from its GET /config) show with secrets only as set or not. A member who isn't an instance admin sees no Instance link. The
// settings are put back as they were after.
// Console URL and Ingest URL show what a blank one uses; a Console URL with
// the other scheme than Console's HTTPS mode is refused, and a saved Ingest
// URL is the Project page's DSN at once. HTTPS is a restart-to-change row.

const rails = (code: string) =>
  execFileSync('mise', ['exec', '--', 'bin/rails', 'runner', code], {
    cwd: resolve(__dirname, '../..'), stdio: 'pipe', timeout: 120_000,
  }).toString().trim().split('\n').pop()!

let saved = '{}'

test.beforeAll(() => {
  saved = rails('print InstanceSetting.current.slice(:log_all_reads, :log_mcp_reads, :default_log_retention_days, ' +
    ':send_console_issues, :trace_console, :logs_text_match_budget_mb, :logs_read_threads, :local_cache_hours, :console_url, :ingest_url).to_json')
  rails('InstanceSetting.current.update!(log_all_reads: false, log_mcp_reads: true, default_log_retention_days: 30, ' +
    'send_console_issues: true, trace_console: true, logs_text_match_budget_mb: 4096, logs_read_threads: 0, local_cache_hours: 24, console_url: nil, ingest_url: nil)')
})

test.afterAll(() => {
  rails(`InstanceSetting.current.update!(JSON.parse(${JSON.stringify(saved)}))`)
})

test('an instance admin changes log-all-reads, log-MCP-reads, the default Retention, Console\'s Sentry, the text-match budget and the Log read threads, and sees Sign-up read-only', async ({ page }) => {
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
  const sendIssues = page.getByRole('checkbox', { name: 'Send Console\'s Issues' })
  const traceConsole = page.getByRole('checkbox', { name: 'Trace Console' })
  await expect(sendIssues).toBeChecked()
  await expect(traceConsole).toBeChecked()

  const budget = page.getByLabel('Text-match budget (MiB)')
  await expect(budget).toHaveValue('4096')
  await expect(page.getByTestId('text-match-budget-description')).toContainText('Dashboard Charts and Monitors are never refused')
  const readThreads = page.getByLabel('Log read threads')
  await expect(readThreads).toHaveValue('0')
  await expect(page.getByTestId('read-threads-description')).toContainText('use more CPU, which ingest shares')

  await page.getByLabel('Default Log Retention (days)').fill('0')
  await budget.fill('-1')
  await readThreads.fill('65')
  await page.getByRole('button', { name: 'Save changes' }).click()
  await expect(page.getByText('must be greater than 0')).toBeVisible()
  await expect(page.getByText('must be greater than or equal to 0')).toBeVisible()
  await expect(page.getByText('must be in 0..64')).toBeVisible()

  await logMcpReads.click()
  await expect(logMcpReads).not.toBeChecked()
  await logAllReads.click()
  await expect(logMcpReads).toBeChecked()
  await expect(logMcpReads).toBeDisabled()
  await page.getByLabel('Default Log Retention (days)').fill('21')
  await budget.fill('10240')
  await readThreads.fill('8')
  await sendIssues.click()
  await traceConsole.click()
  await page.getByRole('button', { name: 'Save changes' }).click()
  await expect(page.getByText('Instance settings saved')).toBeVisible()

  await page.reload()
  await expect(logAllReads).toBeChecked()
  await expect(logMcpReads).toBeChecked()
  await expect(logMcpReads).toBeDisabled()
  await expect(page.getByLabel('Default Log Retention (days)')).toHaveValue('21')
  await expect(budget).toHaveValue('10240')
  await expect(readThreads).toHaveValue('8')
  await expect(sendIssues).not.toBeChecked()
  await expect(traceConsole).not.toBeChecked()

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
  await expect(eventStoreGroup.getByTestId('setting-logs_heavy_read_slots')).toHaveText(/^(\d+|No cap)$/)
  await expect(eventStoreGroup.getByTestId('setting-SOLIDTRACE_CLICKHOUSE_PASSWORD')).toHaveText(/^(Set|Not set)$/)

  expect(errors).toEqual([])
})

test('changing the hours on local disk asks first: Cancel leaves them unsaved, confirming saves them', async ({ page }) => {
  const errors = watchErrors(page)
  await page.goto(`/${ORG}/settings/instance`)
  const hours = page.getByLabel('Hours on local disk')
  await expect(hours).toHaveValue('24')
  await expect(page.getByTestId('local-cache-hours-description')).toContainText('before they move to the bucket')
  // The dev stack's EventStore may have no bucket.
  if (await hours.isDisabled()) {
    await expect(page.getByTestId('local-cache-hours-no-bucket')).toContainText('this does nothing')
    expect(errors).toEqual([])
    return
  }

  await hours.fill('6')
  await page.getByRole('button', { name: 'Save changes' }).click()
  const confirm = page.getByRole('alertdialog')
  await expect(confirm).toContainText('from 24 to 6')
  await expect(confirm).toContainText('Logs older than 6 hours are read from the bucket, which is slower than local disk')
  await expect(confirm).toContainText('counts toward the disk limit; parts already in the bucket stay there')
  await expect(confirm).toContainText('so change it rarely')
  await confirm.getByRole('button', { name: 'Cancel' }).click()
  await expect(confirm).toBeHidden()
  await page.reload()
  await expect(hours).toHaveValue('24')

  await hours.fill('6')
  await page.getByRole('button', { name: 'Save changes' }).click()
  await page.getByRole('button', { name: 'Change to 6 hours' }).click()
  await expect(page.getByText('Instance settings saved')).toBeVisible()
  await page.reload()
  await expect(hours).toHaveValue('6')

  expect(errors).toEqual([])
})

test('an instance admin saves Console\'s and ingest\'s URLs, and the Project page\'s DSN follows ingest\'s', async ({ page }) => {
  const errors = watchErrors(page)
  await page.goto(`/${ORG}/settings/instance`)
  const consoleUrl = page.getByLabel('Console URL')
  const ingestUrl = page.getByLabel('Ingest URL')
  await expect(consoleUrl).toHaveValue('')
  await expect(ingestUrl).toHaveValue('')
  await expect(page.getByTestId('console-url-fallback')).toContainText(/^Blank uses (CONSOLE_URL|the address Console was reached at): http/)
  await expect(page.getByTestId('ingest-url-fallback')).toContainText(/^Blank uses (PUBLIC_INGEST_URL|Console's host at port 4000): http/)

  const consoleGroup = page.getByRole('region', { name: 'Console' })
  const https = consoleGroup.getByTestId('setting-CONSOLE_URL')
  await expect(https).toHaveText(/^(On|Off)$/)
  await expect(consoleGroup.getByTestId('setting-PUBLIC_INGEST_URL')).toHaveCount(0)
  const otherScheme = (await https.textContent()) === 'On' ? 'http' : 'https'

  // The other scheme than Console started in is refused, and nothing saves.
  await consoleUrl.fill(`${otherScheme}://console.example.com`)
  await page.getByRole('button', { name: 'Save changes' }).click()
  await expect(page.getByText('HTTPS changes only with CONSOLE_URL\'s scheme in deploy/.env and a restart')).toBeVisible()
  await consoleUrl.fill('')

  await ingestUrl.fill('https://ingest.example.com:4443/')
  await page.getByRole('button', { name: 'Save changes' }).click()
  await expect(page.getByText('Instance settings saved')).toBeVisible()
  await page.reload()
  await expect(ingestUrl).toHaveValue('https://ingest.example.com:4443')

  await page.goto(`/${ORG}/projects/${PROJECT}`)
  await expect(page.locator('input[readonly]').first()).toHaveValue(/^https:\/\/[^@/]+@ingest\.example\.com:4443\/\d+$/)

  // Cleared, the DSN goes back to the fallback.
  await page.goto(`/${ORG}/settings/instance`)
  await ingestUrl.fill('')
  await page.getByRole('button', { name: 'Save changes' }).click()
  await expect(page.getByText('Instance settings saved')).toBeVisible()
  await page.goto(`/${ORG}/projects/${PROJECT}`)
  await expect(page.locator('input[readonly]').first()).not.toHaveValue(/ingest\.example\.com/)

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
