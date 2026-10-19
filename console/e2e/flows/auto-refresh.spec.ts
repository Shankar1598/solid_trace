import { expect, test, type Page } from '@playwright/test'
import { ORG } from '../support/data'
import { last24h, openLogs } from '../support/logs'
import { watchErrors } from '../support/screens'

// Auto-refresh: the Logs explorer and a new Dashboard run again every 15
// seconds over a relative range, until another interval or Off is picked. The
// page's clock is Playwright's, so the 15 seconds pass at once.

const SOURCE = "['solid-trace-project']"

const picker = (page: Page) => page.getByRole('combobox', { name: 'Auto-refresh' })

/** Counts the requests whose URL matches `pattern` from now on. */
function countRequests(page: Page, pattern: RegExp) {
  const seen = { count: 0 }
  page.on('request', request => {
    if (pattern.test(request.url())) seen.count++
  })
  return seen
}

async function choose(page: Page, option: string) {
  await picker(page).click()
  await page.getByRole('option', { name: option, exact: true }).click()
}

test('the explorer runs again every 15s, and not once Off is picked', async ({ page }) => {
  const errors = watchErrors(page)
  await page.clock.install()
  await openLogs(page)
  await expect(picker(page)).toHaveText(/15s/)

  const runs = countRequests(page, /\/logs\/query\?/)
  await page.clock.runFor('00:16')
  await expect.poll(() => runs.count).toBe(1)
  // The run is quiet: the URL and the stream stay as they were.
  await expect(page).toHaveURL(/range=24h/)

  await choose(page, 'Off')
  await page.clock.runFor('00:31')
  expect(runs.count).toBe(1)

  // The choice is kept in the browser.
  await page.reload()
  await expect(picker(page)).toHaveText(/Off/)
  expect(errors).toEqual([])
})

test('a fixed range never runs again', async ({ page }) => {
  await page.clock.install()
  await openLogs(page, last24h())
  await expect(picker(page)).toHaveAttribute('title', 'Auto-refresh paused: the range is fixed')

  const runs = countRequests(page, /\/logs\/query\?/)
  await page.clock.runFor('00:31')
  expect(runs.count).toBe(0)
})

test('a new Dashboard runs its charts again every 15s', async ({ page }) => {
  const errors = watchErrors(page)
  const name = `Flow auto-refresh ${Date.now()}`
  await page.clock.install()
  await page.goto(`/${ORG}/dashboards`)
  await page.getByRole('button', { name: 'New dashboard' }).first().click()
  await page.getByLabel('Name').fill(name)
  await page.getByRole('button', { name: 'Create dashboard' }).click()
  await expect(page.getByRole('heading', { name })).toBeVisible()
  await expect(picker(page)).toHaveText(/15s/)

  await page.getByRole('button', { name: 'Add chart' }).click()
  const editor = page.getByRole('dialog')
  await editor.getByRole('combobox', { name: 'Project' }).first().click()
  await page.getByRole('option', { name: 'SolidTrace Project', exact: true }).click()
  await editor.getByRole('group', { name: 'Edit the Query in' }).getByRole('button', { name: 'Query' }).click()
  await editor.getByLabel('Query', { exact: true }).fill(`${SOURCE}\n| summarize count()`)
  await editor.getByLabel('Title').fill('Count')
  await editor.getByRole('group', { name: 'Viz' }).getByRole('button', { name: 'Stat' }).click()
  await editor.getByRole('button', { name: 'Add chart' }).click()
  await expect(editor).toHaveCount(0)
  await expect(page.getByRole('status', { name: "Running the chart's Query" })).toHaveCount(0)

  const runs = countRequests(page, /\/charts\/\d+\/data\?/)
  await page.clock.runFor('00:16')
  await expect.poll(() => runs.count).toBe(1)

  // Off is saved on the Dashboard: it stays off after a reload.
  await choose(page, 'Off')
  await page.reload()
  await expect(picker(page)).toHaveText(/Off/)

  await page.getByRole('button', { name: 'Dashboard settings' }).click()
  await page.getByRole('dialog').getByRole('button', { name: 'Delete dashboard' }).click()
  await page.getByRole('alertdialog').getByRole('button', { name: 'Delete dashboard' }).click()
  await expect(page).toHaveURL(new RegExp(`/${ORG}/dashboards$`))
  expect(errors).toEqual([])
})
