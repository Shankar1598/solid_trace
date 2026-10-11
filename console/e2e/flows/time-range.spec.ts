import { test, expect, type Page } from '@playwright/test'
import { openLogs, pickQuickRange, settled, timeRange } from '../support/logs'
import { watchErrors } from '../support/screens'

// The time-range picker in the explorer's bar: a quick range runs at one
// click, a typed range is read back before Apply, start and end dates go in
// the URL as UTC ISO times, and ranges past the Project's Retention are
// turned away with the reason.

const picker = (page: Page) => page.getByRole('dialog')
const params = (page: Page) => new URL(page.url()).searchParams

test('a quick range runs at one click', async ({ page }) => {
  const errors = watchErrors(page)
  await openLogs(page)

  await pickQuickRange(page, '1 hr')
  await settled(page)

  await expect(picker(page)).toHaveCount(0)
  await expect(page).toHaveURL(/range=1h/)
  await expect(timeRange(page)).toHaveText('Last hour')
  // The current range is marked when the picker opens again.
  await timeRange(page).click()
  await expect(picker(page).getByRole('button', { name: '1 hr', exact: true })).toHaveAttribute('aria-pressed', 'true')
  expect(errors).toEqual([])
})

test('a typed range is read back, then applied', async ({ page }) => {
  const errors = watchErrors(page)
  await openLogs(page)
  await timeRange(page).click()

  // The box has focus on opening.
  await page.keyboard.type('2h')
  await expect(picker(page).getByText(/^Last 2 hours: .* until now$/)).toBeVisible()
  await picker(page).getByRole('button', { name: 'Apply' }).click()
  await settled(page)

  await expect(page).toHaveURL(/range=2h/)
  await expect(timeRange(page)).toHaveText('Last 2 hours')

  // Enter applies too; weeks become days.
  await timeRange(page).click()
  await page.getByLabel('Type a range').fill('1w')
  await page.getByLabel('Type a range').press('Enter')
  await settled(page)
  await expect(page).toHaveURL(/range=7d/)
  expect(errors).toEqual([])
})

test('a typed range it cannot read says what it can', async ({ page }) => {
  const errors = watchErrors(page)
  await openLogs(page)
  await timeRange(page).click()
  await page.getByLabel('Type a range').fill('next tuesday')

  await expect(picker(page).getByText('Try 15m, 2h, 3d, 1w, last 3 hours, today or yesterday.')).toBeVisible()
  await expect(picker(page).getByRole('button', { name: 'Apply' })).toBeDisabled()
  expect(errors).toEqual([])
})

test('start and end dates go in the URL as UTC ISO times', async ({ page }) => {
  const errors = watchErrors(page)
  await openLogs(page)
  await timeRange(page).click()

  await page.getByLabel('Start').fill('2026-10-10T10:00')
  await page.getByLabel('End').fill('2026-10-10T12:30')
  await picker(page).getByRole('button', { name: 'Apply' }).click()
  await settled(page)

  // The browser's local times, as UTC.
  const expected = await page.evaluate(() => [new Date('2026-10-10T10:00').toISOString(), new Date('2026-10-10T12:30').toISOString()])
  expect([params(page).get('from'), params(page).get('to')]).toEqual(expected)
  expect(params(page).get('range')).toBeNull()
  await expect(timeRange(page)).toHaveText('Oct 10 10:00 – 12:30')

  // An end before the start is turned away.
  await timeRange(page).click()
  await page.getByLabel('End').fill('2026-10-10T09:00')
  await expect(picker(page).getByText('The start must be before the end.')).toBeVisible()
  await expect(picker(page).getByRole('button', { name: 'Apply' })).toBeDisabled()
  expect(errors).toEqual([])
})

test('yesterday is the whole day before, in local time', async ({ page }) => {
  const errors = watchErrors(page)
  await openLogs(page)
  await timeRange(page).click()
  await page.getByLabel('Type a range').fill('yesterday')
  await page.getByLabel('Type a range').press('Enter')
  await settled(page)

  const expected = await page.evaluate(() => {
    const today = new Date()
    today.setHours(0, 0, 0, 0)
    const yesterday = new Date(today)
    yesterday.setDate(today.getDate() - 1)
    return [yesterday.toISOString(), today.toISOString()]
  })
  expect([params(page).get('from'), params(page).get('to')]).toEqual(expected)
  expect(errors).toEqual([])
})

test('ranges past the Retention are turned away, saying why', async ({ page }) => {
  const errors = watchErrors(page)
  // The dev instance keeps Logs for 30 days.
  await openLogs(page)
  await timeRange(page).click()

  const longest = picker(page).getByRole('button', { name: /^90 days/ })
  await expect(longest).toHaveAttribute('aria-disabled', 'true')
  await longest.hover()
  await expect(page.getByText(/^Longer than this Project's Retention: it keeps Logs for \d+ days\.$/)).toBeVisible()
  // A click picks nothing.
  await longest.click({ force: true })
  await expect(picker(page)).toBeVisible()
  await expect(page).toHaveURL(/range=24h/)

  await page.getByLabel('Type a range').fill('90d')
  await expect(picker(page).getByText(/^Longer than this Project's Retention/)).toBeVisible()
  await expect(picker(page).getByRole('button', { name: 'Apply' })).toBeDisabled()
  expect(errors).toEqual([])
})

test('Cancel and Escape leave the range as it was', async ({ page }) => {
  const errors = watchErrors(page)
  await openLogs(page)
  await timeRange(page).click()
  await page.getByLabel('Type a range').fill('3h')
  await picker(page).getByRole('button', { name: 'Cancel' }).click()
  await expect(picker(page)).toHaveCount(0)

  await timeRange(page).click()
  // Each opening starts afresh.
  await expect(page.getByLabel('Type a range')).toHaveValue('')
  await page.keyboard.press('Escape')
  await expect(picker(page)).toHaveCount(0)
  await expect(page).toHaveURL(/range=24h/)
  await expect(timeRange(page)).toHaveText('Last 24 hours')
  expect(errors).toEqual([])
})
