import { test, expect, type Page } from '@playwright/test'
import { builderRow, last24h, logCount, openLogs, settled, streamRows } from '../support/logs'
import { watchErrors } from '../support/screens'

// The inspector: a Log opened from the stream, its Table and JSON tabs, j and
// k stepping through the stream, Show only and Hide from its values, and Esc
// or the close button.

const inspector = (page: Page) => page.getByRole('complementary', { name: 'Log inspector' })
/** The inspector's heading: "Log" and the Log's local time. */
const heading = (page: Page) => inspector(page).getByRole('heading', { level: 2 })
/** The stream row picked, shaded. */
const selectedRow = (page: Page) => streamRows(page).locator('> div.bg-primary\\/10')
/** Where to click a row to open it: near its start, as a cell's right end holds its Copy button. */
const AT_START = { position: { x: 60, y: 15 } }

test('a Log opens in the inspector, switches tabs and steps with j and k', async ({ page }) => {
  const errors = watchErrors(page)
  await openLogs(page)

  const rows = streamRows(page)
  const firstMessage = (await rows.nth(0).locator('> div > div').last().innerText()).trim()
  await rows.nth(0).click(AT_START)
  await expect(inspector(page)).toBeVisible()
  await expect(inspector(page)).toContainText(firstMessage)
  const first = await heading(page).innerText()

  // Table, then JSON.
  await expect(inspector(page).getByRole('tab', { name: 'Table' })).toHaveAttribute('aria-selected', 'true')
  await expect(inspector(page).getByText('Attributes ·', { exact: false })).toBeVisible()
  await inspector(page).getByRole('tab', { name: 'JSON' }).click()
  await expect(inspector(page).getByRole('tab', { name: 'JSON' })).toHaveAttribute('aria-selected', 'true')
  await expect(inspector(page).getByText('"message"')).toBeVisible()
  // Fold all shuts the attributes; Unfold all opens them again.
  await expect(inspector(page).getByText('"region"')).toBeVisible()
  await inspector(page).getByRole('button', { name: 'Fold all', exact: true }).click()
  await expect(inspector(page).getByText('"region"')).toHaveCount(0)
  await expect(inspector(page).getByText(/\{ … \d+ keys? \}/)).toBeVisible()
  await inspector(page).getByRole('button', { name: 'Unfold all', exact: true }).click()
  await expect(inspector(page).getByText('"region"')).toBeVisible()
  await inspector(page).getByRole('tab', { name: 'Table' }).click()

  // j: the older Log, the next row down; k: back up.
  await page.keyboard.press('j')
  await expect(heading(page)).not.toHaveText(first)
  await expect(selectedRow(page)).toHaveCount(1)
  const second = await heading(page).innerText()
  await page.keyboard.press('j')
  await expect(heading(page)).not.toHaveText(second)
  await page.keyboard.press('k')
  await expect(heading(page)).toHaveText(second)
  await page.keyboard.press('k')
  await expect(heading(page)).toHaveText(first)
  // The newest Log has none newer.
  await expect(inspector(page).getByRole('button', { name: 'Newer Log (k)' })).toHaveAttribute('aria-disabled', 'true')

  // Esc closes it; so does the close button.
  await page.keyboard.press('Escape')
  await expect(inspector(page)).toHaveCount(0)
  await rows.nth(2).click(AT_START)
  await expect(inspector(page)).toBeVisible()
  await inspector(page).getByRole('button', { name: 'Close (Esc)' }).click()
  await expect(inspector(page)).toHaveCount(0)
  expect(errors).toEqual([])
})

test('Show only and Hide from a value in the inspector add a Where filter', async ({ page }) => {
  const errors = watchErrors(page)
  await openLogs(page, last24h())
  const all = await logCount(page)

  await streamRows(page).nth(0).click(AT_START)
  const serviceRow = inspector(page).locator('div.group', { has: page.getByText('service', { exact: true }) }).first()
  const service = (await serviceRow.locator('> div').nth(1).innerText()).trim()
  await serviceRow.hover()
  await serviceRow.getByRole('button', { name: `Show only service == "${service}"` }).click()
  await settled(page)
  const where = builderRow(page, 'Where')
  await expect(where.getByRole('button', { name: `service == "${service}"`, exact: true })).toBeVisible()
  const shown = await logCount(page)
  expect(shown).toBeLessThan(all)

  // Hide from the tag menu of the header's service pill: the Logs of the others.
  await where.getByRole('button', { name: `Remove service == "${service}"` }).click()
  await settled(page)
  const row = streamRows(page).nth(0)
  const pillService = (await row.locator('> div > div:nth-child(3)').innerText()).trim()
  await row.click(AT_START)
  await inspector(page).locator('header').getByRole('button', { name: pillService, exact: true }).click()
  await page.getByRole('menuitem', { name: 'Hide this' }).click()
  await settled(page)
  await expect(where.getByRole('button', { name: `service != "${pillService}"`, exact: true })).toBeVisible()
  expect(await logCount(page)).toBeLessThan(all)
  expect(errors).toEqual([])
})

test('a value in the inspector becomes a column, and counts Logs by its Field', async ({ page }) => {
  const errors = watchErrors(page)
  await openLogs(page)
  await streamRows(page).nth(0).click(AT_START)
  const line = (name: string) => inspector(page).locator('div.group', { has: page.getByText(name, { exact: true }) }).first()

  // region, an Attribute, as a column of the stream; it stays in the URL.
  await line('region').hover()
  await line('region').getByRole('button', { name: 'Add as a column' }).click()
  await expect(page.locator('[data-stream-header]')).toContainText('region')
  await expect(page).toHaveURL(/col=region/)
  await line('region').hover()
  await expect(line('region').getByRole('button', { name: 'Remove the column' })).toHaveAttribute('aria-pressed', 'true')

  // Count Logs by service: a Summarize, its table in place of the stream, and the inspector closes.
  await line('service').hover()
  await line('service').getByRole('button', { name: 'Count Logs by service' }).click()
  await settled(page)
  await expect(inspector(page)).toHaveCount(0)
  await expect(builderRow(page, 'Summarize')).toContainText('count()')
  await expect(builderRow(page, 'Summarize')).toContainText('service')
  await expect(page.locator('.sticky.top-0').filter({ hasText: 'count_' })).toContainText('service')
  expect(errors).toEqual([])
})
