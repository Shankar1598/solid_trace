import { test, expect, type Page } from '@playwright/test'
import { builderRow, last24h, logCount, openLogs, settled, summaryLine } from '../support/logs'
import { watchErrors } from '../support/screens'

// The histogram strip: drag across it, or click a bar, to zoom the range;
// a per-Level count in the summary line filters on that Level.

const range = (page: Page) => {
  const params = new URL(page.url()).searchParams
  return { from: params.get('from'), to: params.get('to'), span: params.get('range') }
}

test('dragging across the strip zooms to the bars dragged over, and clicking a bar to its time', async ({ page }) => {
  const errors = watchErrors(page)
  await openLogs(page)
  const all = await logCount(page)
  expect(range(page).span).toBe('24h')

  const strip = page.locator('.cursor-crosshair')
  const box = (await strip.boundingBox())!
  const y = box.y + box.height / 2
  await page.mouse.move(box.x + box.width * 0.3, y)
  await page.mouse.down()
  await page.mouse.move(box.x + box.width * 0.45, y, { steps: 5 })
  await page.mouse.move(box.x + box.width * 0.6, y, { steps: 5 })
  await page.mouse.up()
  await settled(page)

  const dragged = range(page)
  expect(dragged.span).toBeNull()
  const span = (Date.parse(dragged.to!) - Date.parse(dragged.from!)) / 3600_000
  // About 30% of 24 h, give or take a bar at each end.
  expect(span).toBeGreaterThan(6)
  expect(span).toBeLessThan(8.5)
  await expect(page.getByRole('combobox', { name: 'Time range' })).not.toHaveText(/Last 24 hours/)
  const zoomed = await logCount(page)
  expect(zoomed).toBeLessThan(all)

  // Click one bar: its time only.
  const after = (await strip.boundingBox())!
  await page.mouse.click(after.x + after.width * 0.5, after.y + after.height / 2)
  await settled(page)
  const clicked = range(page)
  const bar = (Date.parse(clicked.to!) - Date.parse(clicked.from!)) / 60_000
  expect(bar).toBeGreaterThan(0)
  expect(bar).toBeLessThanOrEqual(5)
  expect(Date.parse(clicked.from!)).toBeGreaterThanOrEqual(Date.parse(dragged.from!))
  expect(Date.parse(clicked.to!)).toBeLessThanOrEqual(Date.parse(dragged.to!))
  expect(await logCount(page)).toBeLessThan(zoomed)
  expect(errors).toEqual([])
})

test('a Level count in the summary line filters on that Level', async ({ page }) => {
  const errors = watchErrors(page)
  await openLogs(page, last24h())
  const line = summaryLine(page)
  const warn = line.getByRole('button', { name: /^warn [\d,]+$/ })
  const warnCount = Number((await warn.innerText()).replace(/[^\d]/g, ''))
  expect(warnCount).toBeGreaterThan(0)

  await warn.click()
  await settled(page)
  await expect(builderRow(page, 'Where').getByRole('button', { name: 'level == "warn"', exact: true })).toBeVisible()
  expect(await logCount(page)).toBe(warnCount)

  // Clicking it again keeps the one filter.
  await summaryLine(page).getByRole('button', { name: /^warn [\d,]+$/ }).click()
  await settled(page)
  await expect(builderRow(page, 'Where').getByRole('button', { name: /^Remove/ })).toHaveCount(1)
  expect(errors).toEqual([])
})
