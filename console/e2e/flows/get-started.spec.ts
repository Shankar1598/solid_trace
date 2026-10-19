import { test, expect } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import { resolve } from 'node:path'
import { ORG } from '../support/data'
import { watchErrors } from '../support/screens'

// An empty Project's Logs page: snippets with its ingest URL and key filled
// in, and Send a test Log, which shows the Log in the stream at once. Each run
// makes its own Project, since a test Log leaves it empty no more.

const name = `Empty ${Date.now()}`
const slug = name.toLowerCase().replace(' ', '-')
const firstLogs = 'Send this Project its first Logs'

test.afterAll(() => {
  // So the flows' Projects don't pile up in the Project lists.
  execFileSync('mise', ['exec', '--', 'bin/rails', 'runner', `Project.find_by(slug: ${JSON.stringify(slug)})&.destroy`], {
    cwd: resolve(__dirname, '../..'), stdio: 'pipe', timeout: 120_000,
  })
})

test('an empty Project shows how to send it Logs, and a test Log shows at once', async ({ page }) => {
  const errors = watchErrors(page)
  await page.goto(`/${ORG}/projects/new`)
  await page.getByLabel('Name').fill(name)
  await page.getByRole('button', { name: 'Create project' }).click()
  await expect(page).toHaveURL(new RegExp(`/${ORG}/projects$`))

  await page.goto(`/${ORG}/projects/${slug}/logs?range=1h`)
  await expect(page.getByRole('heading', { name: firstLogs })).toBeVisible()

  // The snippets carry the new Project's own key, and the key-only Log route.
  const shown = page.getByRole('tabpanel')
  await expect(shown).toContainText(/Authorization: Bearer [0-9a-f]{32}/)
  await expect(shown).toContainText('http://localhost:4000/api/logs')
  for (const tab of ['Vector', 'Fluent Bit', 'Sentry SDK']) {
    await page.getByRole('tab', { name: tab }).click()
    await expect(shown).toContainText(tab === 'Sentry SDK' ? /dsn: 'http:\/\/[0-9a-f]{32}@localhost:4000\/\d+'/ : /[0-9a-f]{32}/)
  }

  await page.getByRole('button', { name: 'Send a test Log' }).click()
  await expect(page.getByText(/A test Log from SolidTrace/).first()).toBeVisible({ timeout: 20_000 })
  await expect(page.getByRole('heading', { name: firstLogs })).toHaveCount(0)
  expect(errors).toEqual([])
})
