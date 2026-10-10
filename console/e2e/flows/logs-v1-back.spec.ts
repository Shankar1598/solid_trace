import { test, expect } from '@playwright/test'
import { addFilter, builderRow, last24h, logCount, openLogs, settled } from '../support/logs'
import { watchErrors } from '../support/screens'

// Each run is a history entry: Back returns to the run before, its Query and
// its range, and runs it again.

test('Back returns to the previous run, its Query and its range', async ({ page }) => {
  const errors = watchErrors(page)
  await openLogs(page, last24h())
  const all = await logCount(page)
  const where = builderRow(page, 'Where')
  const errorChip = where.getByRole('button', { name: 'level == "error"', exact: true })

  await addFilter(page, 'level', '==', 'error')
  await expect(errorChip).toBeVisible()
  const errorLogs = await logCount(page)
  const filtered = page.url()

  // A range picked runs too.
  await page.getByRole('combobox', { name: 'Time range' }).click()
  await page.getByRole('option', { name: 'Last 6 hours' }).click()
  await settled(page)
  await expect(page).toHaveURL(/range=6h/)
  await expect(errorChip).toBeVisible()

  // Back: the 24 h range again, with the filter.
  await page.goBack()
  await settled(page)
  expect(page.url()).toBe(filtered)
  await expect(page.getByRole('combobox', { name: 'Time range' })).not.toHaveText(/Last 6 hours/)
  await expect(errorChip).toBeVisible()
  expect(await logCount(page)).toBe(errorLogs)

  // Back again: no filter.
  await page.goBack()
  await settled(page)
  await expect(where.getByRole('button', { name: /^Remove/ })).toHaveCount(0)
  expect(await logCount(page)).toBe(all)

  // Forward: the filter again.
  await page.goForward()
  await settled(page)
  await expect(errorChip).toBeVisible()
  expect(errors).toEqual([])
})

test('Back in Query mode returns to the Query run before', async ({ page }) => {
  const errors = watchErrors(page)
  await openLogs(page, last24h('query'))
  const box = page.getByLabel('Query', { exact: true })
  const first = "['solid-trace-project']\n| where level == \"warn\""
  const second = "['solid-trace-project']\n| where level == \"error\""

  await box.fill(first)
  await box.press('Control+Enter')
  await settled(page)
  const warnLogs = await logCount(page)
  await box.fill(second)
  await box.press('Control+Enter')
  await settled(page)
  await expect(page).toHaveURL(/error/)

  await page.goBack()
  await settled(page)
  await expect(page).toHaveURL(/mode=query/)
  await expect(page.getByLabel('Query', { exact: true })).toHaveValue(first)
  expect(await logCount(page)).toBe(warnLogs)
  expect(errors).toEqual([])
})
