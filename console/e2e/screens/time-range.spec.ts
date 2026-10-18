import { test, expect, type Page } from '@playwright/test'
import { openLogs } from '../support/logs'
import { shoot, useTheme, watchErrors } from '../support/screens'

// The time-range picker open: its quick ranges with the current one marked,
// a typed range read back, a typed range it can't read, and a quick range
// longer than the Project's Retention with its reason.

test.beforeEach(async ({ page }, info) => { await useTheme(page, info) })

const picker = (page: Page) => page.getByRole('dialog')

async function openPicker(page: Page) {
  await page.getByRole('button', { name: /^Time range/ }).click()
  await expect(picker(page).getByText('Quick range')).toBeVisible()
}

test('time range picker open', async ({ page }) => {
  const errors = watchErrors(page)
  await openLogs(page)
  await openPicker(page)
  await shoot(page, 'time-range-open')
  expect(errors).toEqual([])
})

test('time range picker reading a typed range', async ({ page }) => {
  const errors = watchErrors(page)
  await openLogs(page)
  await openPicker(page)
  await page.getByLabel('Type a range').fill('last 3 hours')
  await expect(picker(page).getByText(/^Last 3 hours: /)).toBeVisible()
  await shoot(page, 'time-range-typed')
  await page.getByLabel('Type a range').fill('next tuesday')
  await expect(picker(page).getByText(/^Try 15m/)).toBeVisible()
  await shoot(page, 'time-range-typed-error')
  expect(errors).toEqual([])
})

test('time range picker past the Retention', async ({ page }) => {
  const errors = watchErrors(page)
  await openLogs(page)
  await openPicker(page)
  await picker(page).getByRole('button', { name: /^90 days/ }).hover()
  await expect(page.getByText(/Longer than this Project's Retention/).last()).toBeVisible()
  await shoot(page, 'time-range-retention')
  expect(errors).toEqual([])
})
