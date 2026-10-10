import { test, expect } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import { resolve } from 'node:path'
import { last24h, logCount, openLogs, settled } from '../support/logs'
import { failedLoad, watchErrors } from '../support/screens'

// SQL mode, on an instance with SQL queries on: the Query's readable SQL,
// run only on Run, answering a table; leaving with edited SQL asks first.

const sqlQueries = (on: boolean) => execFileSync('mise', ['exec', '--', 'bin/rails', 'instance_settings:set', `SQL_QUERIES_ENABLED=${on}`], {
  cwd: resolve(__dirname, '../..'), stdio: 'pipe', timeout: 120_000,
})

test.beforeAll(() => { sqlQueries(true) })
test.afterAll(() => { sqlQueries(false) })

test('SQL mode opens with the readable SQL, runs on Run, and asks before discarding edits', async ({ page }) => {
  // The SQL with a mistake answers 400.
  const errors = watchErrors(page, [failedLoad(400)])
  await openLogs(page, last24h())
  const all = await logCount(page)

  await page.getByRole('button', { name: 'SQL', exact: true }).click()
  const box = page.getByLabel('SQL', { exact: true })
  await expect(box).toHaveValue(/FROM logs/i)
  await expect(page).toHaveURL(/mode=sql/)
  // Nothing runs until Run.
  await expect(page.getByText('Press Run to run the SQL.')).toBeVisible()

  await page.getByRole('button', { name: 'Run', exact: true }).click()
  await settled(page)
  await expect(page.getByRole('button', { name: 'Run', exact: true })).toBeVisible()
  const header = page.locator('.sticky.top-0').filter({ hasText: 'message' })
  await expect(header).toBeVisible()

  // Edited SQL, run by Ctrl+Enter: a count per Level adds up to the Logs.
  await box.fill('SELECT level, count(*) AS n FROM logs GROUP BY level ORDER BY n DESC')
  await box.press('Control+Enter')
  await settled(page)
  const table = page.locator('.sticky.top-0').filter({ hasText: /^level\s*n$/ })
  await expect(table).toBeVisible()
  const counts = await table.locator('xpath=following-sibling::div[1]/div/div[2]').allInnerTexts()
  expect(counts.length).toBeGreaterThan(1)
  expect(counts.reduce((sum, n) => sum + Number(n.replace(/,/g, '')), 0)).toBe(all)

  // A mistake in the SQL says what DuckDB said.
  await box.fill('SELECT nope FROM logs')
  await page.getByRole('button', { name: 'Run', exact: true }).click()
  await expect(page.getByText(/nope/i).last()).toBeVisible()

  // Leaving with edits asks: Stay keeps the SQL, Discard returns to the Builder.
  await page.getByRole('button', { name: 'Builder', exact: true }).click()
  await expect(page.getByText(/SQL can't be turned back into a Query/)).toBeVisible()
  await page.getByRole('button', { name: 'Stay in SQL' }).click()
  await expect(page.getByText(/SQL can't be turned back into a Query/)).toHaveCount(0)
  await expect(box).toHaveValue('SELECT nope FROM logs')

  await page.getByRole('button', { name: 'Builder', exact: true }).click()
  await page.getByRole('button', { name: 'Discard and switch' }).click()
  await settled(page)
  await expect(page).toHaveURL(/mode=builder/)
  expect(await logCount(page)).toBe(all)
  expect(errors).toEqual([])
})

test('leaving SQL mode without edits is silent', async ({ page }) => {
  const errors = watchErrors(page)
  await openLogs(page)
  await page.getByRole('button', { name: 'SQL', exact: true }).click()
  await expect(page.getByLabel('SQL', { exact: true })).toHaveValue(/FROM logs/i)
  await page.getByRole('button', { name: 'Query', exact: true }).click()
  await settled(page)
  await expect(page.getByText(/SQL can't be turned back into a Query/)).toHaveCount(0)
  await expect(page).toHaveURL(/mode=query/)
  await expect(page.getByLabel('Query', { exact: true })).toBeVisible()
  expect(errors).toEqual([])
})
