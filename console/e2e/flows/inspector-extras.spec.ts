import { test, expect, type Page } from '@playwright/test'
import { builderRow, openLogs, settled, streamRows } from '../support/logs'
import { watchErrors } from '../support/screens'

// The inspector's ways out to other Logs: Logs around this one, which reads
// the Log's Service a minute either side with the Log highlighted, and Chart
// this field on a number, which charts its average over time.

const inspector = (page: Page) => page.getByRole('complementary', { name: 'Log inspector' })
const heading = (page: Page) => inspector(page).getByRole('heading', { level: 2 })
const selectedRow = (page: Page) => streamRows(page).locator('> div.bg-primary\\/10')
const line = (page: Page, name: string) => inspector(page).locator('div.group', { has: page.getByText(name, { exact: true }) }).first()
/** Where to click a row to open it: near its start, as a cell's right end holds its button. */
const AT_START = { position: { x: 60, y: 15 } }

test('Logs around this one shows its Service a minute either side, with it highlighted', async ({ page }) => {
  const errors = watchErrors(page)
  await openLogs(page)

  const row = streamRows(page).nth(5)
  const message = (await row.locator('> div > div').last().innerText()).trim()
  await row.click(AT_START)
  const opened = await heading(page).innerText()
  const service = (await line(page, 'service').locator('> div').nth(1).innerText()).trim()

  await inspector(page).getByRole('button', { name: 'Logs around this one' }).click()
  await settled(page)

  await expect(builderRow(page, 'Where').getByRole('button', { name: `service == "${service}"`, exact: true })).toBeVisible()
  const search = new URL(page.url()).searchParams
  expect(new Date(search.get('to')!).getTime() - new Date(search.get('from')!).getTime()).toBe(120_000)
  // The inspector stays on the Log, and the stream shades its row in view.
  await expect(heading(page)).toHaveText(opened)
  await expect(selectedRow(page)).toHaveCount(1)
  await expect(selectedRow(page)).toContainText(message)
  await expect(selectedRow(page)).toBeInViewport()
  const services = await streamRows(page).locator('> div > div:nth-child(3)').allInnerTexts()
  expect(new Set(services.map(s => s.trim()))).toEqual(new Set([service]))
  expect(errors).toEqual([])
})

test('Chart this field charts a number\'s average over time', async ({ page }) => {
  const errors = watchErrors(page)
  await openLogs(page, `?range=24h&q=${encodeURIComponent("['solid-trace-project']\n| where isnotnull(duration_ms)")}`)
  await expect(builderRow(page, 'Where')).toContainText('duration_ms')

  await streamRows(page).nth(0).click(AT_START)
  // Only numbers offer it.
  await line(page, 'service').hover()
  await expect(line(page, 'service').getByRole('button', { name: /^Chart this field/ })).toHaveCount(0)
  await line(page, 'duration_ms').hover()
  await line(page, 'duration_ms').getByRole('button', { name: 'Chart this field: avg(duration_ms) over time' }).click()
  await settled(page)

  await expect(inspector(page)).toHaveCount(0)
  await expect(builderRow(page, 'Summarize')).toContainText('avg(duration_ms)')
  await expect(page.getByRole('figure')).toHaveAttribute('aria-label', 'Line chart of avg_duration_ms over time')
  expect(errors).toEqual([])
})
