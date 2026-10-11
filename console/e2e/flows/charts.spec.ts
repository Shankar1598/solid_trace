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
const legend = (page: Page) => page.locator('[data-chart-series]')
const viz = (page: Page, name: string) => page.getByRole('group', { name: 'Chart' }).getByRole('button', { name })
/** The histogram strip over the result, which a table over time stands in for. */
const strip = (page: Page) => page.locator('[data-histogram-strip]')

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
  await expect(viz(page, 'Line')).toHaveAttribute('aria-pressed', 'true')
  await expect(page).not.toHaveURL(/viz=/)

  for (const [name, value, kind] of [['Area', 'area', 'Area'], ['Bars', 'bars', 'Bar']] as const) {
    await viz(page, name).click()
    await expect(page).toHaveURL(new RegExp(`viz=${value}`))
    await expect(chart(page)).toHaveAttribute('aria-label', new RegExp(`^${kind} chart of `))
  }
  // Line is a time chart's own form, so it leaves no trace in the URL.
  await viz(page, 'Line').click()
  await expect(page).not.toHaveURL(/viz=/)
  await expect(chart(page)).toHaveAttribute('aria-label', /^Line chart of /)

  await viz(page, 'Bars').click()
  await expect(page).toHaveURL(/viz=bars/)
  await page.reload()
  await settled(page)
  await expect(viz(page, 'Bars')).toHaveAttribute('aria-pressed', 'true')
  await expect(chart(page)).toHaveAttribute('aria-label', /^Bar chart of /)

  // Table: the switch stays where it was, the chart goes, and the histogram strip stays away.
  const at = (await viz(page, 'Table').boundingBox())!.y
  await viz(page, 'Table').click()
  await expect(page).toHaveURL(/viz=table/)
  await expect(chart(page)).toHaveCount(0)
  await expect(viz(page, 'Table')).toHaveAttribute('aria-pressed', 'true')
  expect((await viz(page, 'Table').boundingBox())!.y).toBe(at)
  await expect(strip(page)).toHaveCount(0)
  await page.reload()
  await settled(page)
  await expect(viz(page, 'Table')).toHaveAttribute('aria-pressed', 'true')
  await expect(chart(page)).toHaveCount(0)
  expect(errors).toEqual([])
})

test('Bars over categories leaves no trace, so the next time series is a line', async ({ page }) => {
  const errors = watchErrors(page)
  await openLogs(page, last24h('query'))
  await runQuery(page, `${SOURCE}\n| summarize count() by service`)
  await viz(page, 'Table').click()
  await expect(page).toHaveURL(/viz=table/)
  await viz(page, 'Bars').click()
  await expect(page).not.toHaveURL(/viz=/)

  await runQuery(page, `${SOURCE}\n| summarize count() by bin_auto(time)`)
  await expect(chart(page)).toHaveAttribute('aria-label', /^Line chart of /)
  expect(errors).toEqual([])
})

test('a first run of a Query over time hides the histogram strip before it answers', async ({ page }) => {
  const errors = watchErrors(page)
  await openLogs(page, last24h('query'))
  await expect(strip(page)).toHaveCount(1)
  let release = () => {}
  const held = new Promise<void>(resolve => { release = resolve })
  await page.route(/\/logs\/query\?/, async route => {
    await held
    await route.continue()
  })

  const box = page.getByLabel('Query', { exact: true })
  await box.fill(`${SOURCE}\n| summarize count() by bin_auto(time), service`)
  await box.press('Control+Enter')
  await expect(page.getByText('Running the Query…')).toBeVisible()
  await expect(strip(page)).toHaveCount(0)
  release()
  await settled(page)
  await expect(chart(page)).toBeVisible()
  await expect(strip(page)).toHaveCount(0)
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
  await expect(viz(page, 'Number')).toHaveAttribute('aria-pressed', 'true')
  // The number is its table, shown once.
  await expect(page.locator('.sticky.top-0').filter({ hasText: 'count_' })).toHaveCount(0)
  // Not a time chart: the histogram strip stays.
  await expect(page.getByText(/error rate/)).toBeVisible()

  await runQuery(page, `${SOURCE}\n| summarize count() by service`)
  await expect(chart(page)).toHaveAttribute('aria-label', 'Bar chart of count_ by service')
  await expect(chart(page).locator('.recharts-bar-rectangle')).toHaveCount(4)
  await expect(viz(page, 'Bars')).toHaveAttribute('aria-pressed', 'true')
  await expect(viz(page, 'Line')).toHaveCount(0)

  // Table shows a single number's cell.
  await runQuery(page, `${SOURCE}\n| summarize count()`)
  await viz(page, 'Table').click()
  await expect(page.locator('.sticky.top-0').filter({ hasText: 'count_' })).toBeVisible()
  await expect(page.locator('[data-chart-stat]')).toHaveCount(0)
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

/** Each legend entry's swatch colour, by its name. */
async function legendColors(page: Page): Promise<Record<string, string>> {
  return Object.fromEntries(await legend(page).evaluateAll(buttons => buttons.map(b => [
    (b as HTMLElement).innerText.trim(), getComputedStyle(b.querySelector('span')!).backgroundColor,
  ])))
}

test('a series keeps its colour when a filter takes another away', async ({ page }) => {
  const errors = watchErrors(page)
  await openLogs(page, last24h('query'))
  await runQuery(page, `${SOURCE}\n| summarize count() by bin_auto(time), service`)
  const before = await legendColors(page)
  expect(Object.keys(before).sort()).toEqual(['api', 'billing', 'web', 'worker'])

  await runQuery(page, `${SOURCE}\n| where service != "api"\n| summarize count() by bin_auto(time), service`)
  const after = await legendColors(page)
  expect(Object.keys(after).sort()).toEqual(['billing', 'web', 'worker'])
  for (const [name, color] of Object.entries(after)) expect(color, name).toBe(before[name])
  expect(errors).toEqual([])
})

test('columns that measure different things get a panel each over one time axis', async ({ page }) => {
  const errors = watchErrors(page)
  await openLogs(page, last24h('query'))
  await runQuery(page, `${SOURCE}\n| summarize avg(duration_ms), count() by bin_auto(time), service`)
  await expect(page.locator('[data-chart-panel]')).toHaveText(['avg_duration_ms', 'count_'])
  await expect(chart(page).locator('.recharts-surface')).toHaveCount(2)
  // A service wears one colour in both panels, with one legend entry.
  expect((await legend(page).allInnerTexts()).sort()).toEqual(['api', 'billing', 'web', 'worker'])

  await runQuery(page, `${SOURCE}\n| summarize min(duration_ms), avg(duration_ms), max(duration_ms) by bin_auto(time)`)
  await expect(page.locator('[data-chart-panel]')).toHaveCount(0)
  await expect(chart(page).locator('.recharts-line')).toHaveCount(3)
  expect(errors).toEqual([])
})

test('a bin the range covers in part says so in the tooltip', async ({ page }) => {
  const errors = watchErrors(page)
  // A range starting 7 minutes into an hour: its first hourly bin holds 53 minutes of Logs.
  const to = new Date(Math.floor(Date.now() / 3_600_000) * 3_600_000 + 7 * 60_000)
  const from = new Date(to.getTime() - 24 * 3_600_000)
  await openLogs(page, `?${new URLSearchParams({ from: from.toISOString(), to: to.toISOString(), mode: 'query' })}`)
  await runQuery(page, `${SOURCE}\n| summarize count() by bin(time, 1h)`)
  const box = (await chart(page).locator('.recharts-cartesian-grid').boundingBox())!

  await page.mouse.move(box.x + 2, box.y + box.height / 2)
  await expect(page.locator('.recharts-tooltip-wrapper')).toContainText('partial bin')
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2)
  await expect(page.locator('.recharts-tooltip-wrapper')).not.toContainText('partial bin')
  expect(errors).toEqual([])
})

test('on a phone the legend entries are targets a finger can hit', async ({ page }) => {
  const errors = watchErrors(page)
  await page.setViewportSize({ width: 390, height: 844 })
  await openLogs(page, last24h('query'))
  await runQuery(page, `${SOURCE}\n| summarize count() by bin_auto(time), service`)
  for (const box of await legend(page).evaluateAll(buttons => buttons.map(b => b.getBoundingClientRect().height))) {
    expect(box).toBeGreaterThanOrEqual(24)
  }
  expect(errors).toEqual([])
})
