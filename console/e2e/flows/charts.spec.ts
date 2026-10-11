import { expect, test, type Page } from '@playwright/test'
import { builderRow, last24h, openLogs, runBy, settled, streamRows } from '../support/logs'
import { watchErrors } from '../support/screens'

// Charts in the Logs explorer: a Summarize draws its table as a chart over
// time, bars or a single number above the table. The switch over it picks
// Line, Area, Bars or the table alone and stays in the URL; dragging across a
// time chart narrows the range.

const SOURCE = "['solid-trace-project']"

/** The chart over the table, by its accessible name: "Line chart of api, billing … over time". */
const chart = (page: Page) => page.getByRole('figure')
/** The legend's buttons, one per series, each showing or hiding it. */
const legend = (page: Page) => page.locator('[data-chart-legend] button[aria-pressed]')
const viz = (page: Page, name: string) => page.getByRole('radiogroup', { name: 'Chart' }).getByRole('radio', { name })

async function runQuery(page: Page, text: string) {
  await page.getByRole('button', { name: 'Query', exact: true }).click()
  const box = page.getByLabel('Query', { exact: true })
  await box.fill(text)
  await runBy(page, () => box.press('Control+Enter'))
}

test('Summarize count by service in the Builder draws a series per service over time', async ({ page }) => {
  const errors = watchErrors(page)
  await openLogs(page, last24h())
  await expect(page.getByText(/error rate/)).toBeVisible()

  // One click: count() over time in automatic bins.
  await page.getByRole('button', { name: 'Add a function' }).click()
  await page.getByRole('option', { name: /^count/ }).click()
  await settled(page)
  await expect(builderRow(page, 'Summarize')).toContainText('bin_auto(time)')
  await expect(chart(page)).toHaveAttribute('aria-label', 'Line chart of count_ over time')

  // by service: a series each, toggled from the legend.
  await page.getByRole('button', { name: 'Group by a Field or time' }).click()
  await page.getByLabel('Group by time or a Field…').fill('service')
  await page.getByRole('option', { name: /^service/ }).first().click()
  await settled(page)
  await expect(chart(page)).toHaveAttribute('aria-label', /^Line chart of .*\bapi\b.*\bbilling\b.* over time$/)
  await expect(legend(page)).toHaveCount(4)
  expect((await legend(page).allInnerTexts()).sort()).toEqual(['api', 'billing', 'web', 'worker'])
  const series = 4
  await expect(chart(page).locator('.recharts-line')).toHaveCount(series)
  await legend(page).filter({ hasText: 'billing' }).click()
  await expect(legend(page).filter({ hasText: 'billing' })).toHaveAttribute('aria-pressed', 'false')
  await expect(chart(page).locator('.recharts-line-curve')).toHaveCount(series - 1)

  // The chart stands in for the histogram strip; the table stays below it.
  await expect(page.getByText(/error rate/)).toHaveCount(0)
  await expect(page.locator('.sticky.top-0').filter({ hasText: 'count_' })).toContainText('service')
  expect(errors).toEqual([])
})

test('the chart switch changes the URL, survives a reload, and Table shows the table alone', async ({ page }) => {
  const errors = watchErrors(page)
  await openLogs(page, last24h('query'))
  await runQuery(page, `${SOURCE}\n| summarize count() by bin_auto(time), level`)
  await expect(viz(page, 'Line')).toHaveAttribute('aria-checked', 'true')
  await expect(page).not.toHaveURL(/viz=/)

  for (const [name, value, kind] of [['Area', 'area', 'Area'], ['Bars', 'bars', 'Bar'], ['Line', 'line', 'Line']] as const) {
    await viz(page, name).click()
    await expect(page).toHaveURL(new RegExp(`viz=${value}`))
    await expect(chart(page)).toHaveAttribute('aria-label', new RegExp(`^${kind} chart of `))
  }

  await viz(page, 'Bars').click()
  await expect(page).toHaveURL(/viz=bars/)
  await page.reload()
  await settled(page)
  await expect(viz(page, 'Bars')).toHaveAttribute('aria-checked', 'true')
  await expect(chart(page)).toHaveAttribute('aria-label', /^Bar chart of /)

  // Table: the switch stays, the chart goes, and the histogram strip comes back.
  await viz(page, 'Table').click()
  await expect(page).toHaveURL(/viz=table/)
  await expect(chart(page)).toHaveCount(0)
  await expect(viz(page, 'Table')).toHaveAttribute('aria-checked', 'true')
  await expect(page.getByText(/error rate/)).toBeVisible()
  await page.reload()
  await settled(page)
  await expect(viz(page, 'Table')).toHaveAttribute('aria-checked', 'true')
  await expect(chart(page)).toHaveCount(0)
  expect(errors).toEqual([])
})

test('dragging across a time chart narrows the range to the bins dragged over', async ({ page }) => {
  const errors = watchErrors(page)
  await openLogs(page, last24h('query'))
  await runQuery(page, `${SOURCE}\n| summarize count() by bin(time, 1h)`)
  const before = new URL(page.url()).searchParams
  const box = (await chart(page).locator('.recharts-cartesian-grid').boundingBox())!

  const y = box.y + box.height / 2
  await page.mouse.move(box.x + box.width * 0.3, y)
  await page.mouse.down()
  await page.mouse.move(box.x + box.width * 0.45, y, { steps: 5 })
  await page.mouse.move(box.x + box.width * 0.6, y, { steps: 5 })
  await runBy(page, () => page.mouse.up())

  const after = new URL(page.url()).searchParams
  const from = new Date(after.get('from')!)
  const to = new Date(after.get('to')!)
  expect(from.getTime()).toBeGreaterThan(new Date(before.get('from')!).getTime())
  expect(to.getTime()).toBeLessThan(new Date(before.get('to')!).getTime())
  // Whole hours: the 1h bins dragged over, about 30% of the day.
  expect(from.getUTCMinutes()).toBe(0)
  expect((to.getTime() - from.getTime()) / 3_600_000).toBeGreaterThanOrEqual(5)
  expect((to.getTime() - from.getTime()) / 3_600_000).toBeLessThanOrEqual(10)
  await expect(page.getByLabel('Time range')).toContainText('–')
  await expect(chart(page)).toBeVisible()
  expect(errors).toEqual([])
})

test('summarize count() shows a single number, and by service shows bars', async ({ page }) => {
  const errors = watchErrors(page)
  await openLogs(page, last24h('query'))

  await runQuery(page, `${SOURCE}\n| summarize count()`)
  const stat = page.locator('[data-chart-stat]')
  await expect(stat).toContainText('count_')
  await expect(stat).toContainText(/\d/)
  await expect(viz(page, 'Number')).toHaveAttribute('aria-checked', 'true')
  // Not a time chart: the histogram strip stays.
  await expect(page.getByText(/error rate/)).toBeVisible()

  await runQuery(page, `${SOURCE}\n| summarize count() by service`)
  await expect(chart(page)).toHaveAttribute('aria-label', 'Bar chart of count_ by service')
  await expect(chart(page).locator('.recharts-bar-rectangle')).toHaveCount(4)
  await expect(viz(page, 'Bars')).toHaveAttribute('aria-checked', 'true')
  await expect(viz(page, 'Line')).toHaveCount(0)
  expect(errors).toEqual([])
})

test('the keyboard reaches the chart and sees its focus ring; a mouse press draws none', async ({ page }) => {
  const errors = watchErrors(page)
  await openLogs(page, last24h('query'))
  await runQuery(page, `${SOURCE}\n| summarize count() by bin_auto(time), service`)
  const surface = chart(page).locator('svg.recharts-surface')
  const ring = () => surface.evaluate(el => ({ focused: el === document.activeElement, outline: getComputedStyle(el).outlineStyle }))

  // Tab on from the legend: through the switch, onto the chart.
  await legend(page).last().focus()
  for (let i = 0; i < 8 && !(await ring()).focused; i++) await page.keyboard.press('Tab')
  expect(await ring()).toEqual({ focused: true, outline: 'solid' })

  await page.mouse.click(10, 10)
  const box = (await surface.boundingBox())!
  await page.mouse.click(box.x + box.width / 2, box.y + box.height / 2)
  expect(await ring()).toEqual({ focused: true, outline: 'none' })
  expect(errors).toEqual([])
})

test('removing the one function a click added takes the Builder back to the Log stream', async ({ page }) => {
  const errors = watchErrors(page)
  await openLogs(page, last24h())
  await page.getByRole('button', { name: 'Add a function' }).click()
  await page.getByRole('option', { name: /^count/ }).click()
  await settled(page)
  await expect(chart(page)).toBeVisible()

  await page.getByRole('button', { name: 'Remove count()' }).click()
  await settled(page)
  await expect(builderRow(page, 'Summarize')).not.toContainText('bin_auto(time)')
  await expect(chart(page)).toHaveCount(0)
  await expect(streamRows(page).first()).toBeVisible()
  expect(new URL(page.url()).searchParams.get('q') ?? '').not.toContain('summarize')
  expect(errors).toEqual([])
})

test('a count by an Attribute is charted, over time and as bars', async ({ page }) => {
  const errors = watchErrors(page)
  await openLogs(page, last24h('query'))
  await runQuery(page, `${SOURCE}\n| summarize count() by bin_auto(time), http.path`)
  await expect(chart(page)).toHaveAttribute('aria-label', /^Line chart of .*\/.* over time$/)
  await expect(page.getByText(/error rate/)).toHaveCount(0)

  await runQuery(page, `${SOURCE}\n| summarize count() by http.status`)
  await expect(chart(page)).toHaveAttribute('aria-label', 'Bar chart of count_ by http.status')
  expect(errors).toEqual([])
})
