import { test, expect } from '@playwright/test'
import { addFilter, builderRow, last24h, logCount, openLogs, settled, streamRows } from '../support/logs'
import { watchErrors } from '../support/screens'

// The Builder, as a member uses it: a Where filter added in steps narrows the
// stream and the histogram, flips and goes; a Summarize turns the stream into
// a table; More adds a search, a sort and a limit.

test('a Where filter narrows the stream and the histogram, flips, and is removed', async ({ page }) => {
  const errors = watchErrors(page)
  await openLogs(page, last24h())
  const all = await logCount(page)
  expect(all).toBeGreaterThan(0)

  await addFilter(page, 'service', '==', 'billing')
  const where = builderRow(page, 'Where')
  await expect(where.getByRole('button', { name: 'service == "billing"', exact: true })).toBeVisible()
  await expect(page).toHaveURL(/q=.*where/)
  const billing = await logCount(page)
  expect(billing).toBeGreaterThan(0)
  expect(billing).toBeLessThan(all)
  // Every Log in the stream is billing's.
  const services = await streamRows(page).locator('> div > div:nth-child(3)').allInnerTexts()
  expect(services.length).toBeGreaterThan(0)
  expect(new Set(services.map(s => s.trim()))).toEqual(new Set(['billing']))

  // Flip it: edit the clause to !=.
  await where.getByRole('button', { name: 'service == "billing"', exact: true }).click()
  await page.getByLabel('Operator').selectOption('!=')
  await page.getByRole('button', { name: 'Apply' }).click()
  await settled(page)
  await expect(where.getByRole('button', { name: 'service != "billing"', exact: true })).toBeVisible()
  expect(await logCount(page)).toBe(all - billing)

  // Remove it: all the Logs again.
  await where.getByRole('button', { name: /Remove service != "billing"/ }).click()
  await settled(page)
  await expect(where.getByRole('button', { name: /^Remove/ })).toHaveCount(0)
  expect(await logCount(page)).toBe(all)
  expect(errors).toEqual([])
})

test('a Summarize shows a table, and More adds a search, a sort and a limit', async ({ page }) => {
  const errors = watchErrors(page)
  await openLogs(page)

  // count() by service.
  await page.getByRole('button', { name: 'Add a function' }).click()
  await page.getByRole('option', { name: /^count/ }).click()
  await settled(page)
  await page.getByRole('button', { name: 'Group by a Field or time' }).click()
  await page.getByLabel('Group by time or a Field…').fill('service')
  await page.getByRole('option', { name: /^service/ }).first().click()
  await settled(page)
  await expect(page).toHaveURL(/summarize/)
  const table = page.locator('.sticky.top-0').filter({ hasText: 'count_' })
  await expect(table).toBeVisible()
  await expect(table).toContainText('service')
  await expect(page.getByText('billing', { exact: true }).first()).toBeVisible()

  // More: a search…
  await page.getByRole('button', { name: 'Add a search, sort or limit' }).click()
  await page.getByRole('option', { name: /^search/ }).click()
  await page.getByLabel('Text', { exact: true }).fill('orders')
  await page.getByRole('button', { name: 'Apply' }).click()
  await settled(page)
  await expect(builderRow(page, 'Search')).toContainText('"orders"')
  await expect(page).toHaveURL(/search/)

  // …a sort by the count, descending…
  await page.getByRole('button', { name: 'Add a sort or limit' }).click()
  await page.getByRole('option', { name: /^sort by/ }).click()
  await page.getByRole('option', { name: /^count_/ }).click()
  await settled(page)
  await expect(builderRow(page, 'Sort by')).toContainText('count_')
  await expect(page).toHaveURL(/sort/)

  // …and a limit of 1: one row, the busiest service.
  await page.getByRole('button', { name: 'Add a limit' }).click()
  await page.getByLabel('Rows').fill('1')
  await page.getByRole('button', { name: 'Apply' }).click()
  await settled(page)
  await expect(builderRow(page, 'Limit')).toContainText('1')
  await expect(page).toHaveURL(/take|limit/)
  const rows = page.locator('.sticky.top-0').filter({ hasText: 'count_' }).locator('xpath=following-sibling::div[1]/div')
  await expect(rows).toHaveCount(1)

  // Show only this, from the table's service cell: a Where filter, and the table stays.
  const busiest = (await rows.first().locator('> div').first().innerText()).trim()
  await rows.first().getByRole('button', { name: busiest, exact: true }).click()
  await page.getByRole('menuitem', { name: 'Show only this' }).click()
  await settled(page)
  await expect(builderRow(page, 'Where').getByRole('button', { name: `service == "${busiest}"`, exact: true })).toBeVisible()
  await expect(rows).toHaveCount(1)
  await expect(rows.first()).toContainText(busiest)
  expect(errors).toEqual([])
})

test('Cancel stops a run, and Run starts it again', async ({ page }) => {
  const errors = watchErrors(page)
  await openLogs(page)
  // Hold the next run, so it is still going when Cancel is pressed.
  let held = true
  await page.route(/\/logs\/(query|histogram)\?/, route => (held ? undefined : route.continue()))
  await page.getByRole('button', { name: 'Run', exact: true }).click()
  await page.getByRole('button', { name: 'Cancel' }).click()
  await expect(page.getByText('Query cancelled. Run it again to see Logs.')).toBeVisible()
  await expect(page.getByText('Not counted.')).toBeVisible()

  held = false
  await page.getByRole('button', { name: 'Run', exact: true }).click()
  await settled(page)
  await expect(streamRows(page).first()).toBeVisible()
  expect(await logCount(page)).toBeGreaterThan(0)
  expect(errors).toEqual([])
})
