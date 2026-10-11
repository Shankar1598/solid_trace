import { expect, test, type Page } from '@playwright/test'
import { logsPath } from '../support/data'
import { builderRow, settled } from '../support/logs'
import { shoot, useTheme, watchErrors } from '../support/screens'

// Charts in the Logs explorer, for reading by eye at both sizes in both
// themes: a time series per service from the Builder, its Area, Bars and
// Table forms, series of Levels, the tooltip, a series hidden, a drag to
// zoom, bars by service, a single number, and loading, empty and error.

test.beforeEach(async ({ page }, info) => { await useTheme(page, info) })

const SOURCE = "['solid-trace-project']"

/** The explorer over the last 24 h, fixed so runs a moment apart chart the same bins, running q. */
async function openChart(page: Page, q: string, extra: Record<string, string> = {}) {
  const to = new Date(Math.floor(Date.now() / 60_000) * 60_000)
  const from = new Date(to.getTime() - 24 * 3600_000)
  const params = new URLSearchParams({ from: from.toISOString(), to: to.toISOString(), mode: 'query', q: `${SOURCE}\n| ${q}`, ...extra })
  await page.goto(logsPath(`?${params}`))
  await settled(page)
}

const chart = (page: Page) => page.getByRole('figure')

test.describe('Charts', () => {
  test('Builder: count by service over time', async ({ page }) => {
    const errors = watchErrors(page)
    await openChart(page, 'summarize count() by bin_auto(time), service', { mode: 'builder' })
    await expect(builderRow(page, 'Summarize')).toContainText('bin_auto(time)')
    await expect(chart(page)).toBeVisible()
    await page.mouse.move(0, 0)
    await shoot(page, 'charts-builder-line')
    expect(errors).toEqual([])
  })

  test('the tooltip', async ({ page }) => {
    const errors = watchErrors(page)
    await openChart(page, 'summarize count() by bin_auto(time), service')
    const box = (await chart(page).locator('.recharts-cartesian-grid').boundingBox())!
    await page.mouse.move(box.x + box.width * 0.55, box.y + box.height / 2)
    await expect(page.locator('.recharts-tooltip-wrapper')).toContainText('api')
    await shoot(page, 'charts-tooltip')
    expect(errors).toEqual([])
  })

  test('a series hidden from the legend', async ({ page }) => {
    const errors = watchErrors(page)
    await openChart(page, 'summarize count() by bin_auto(time), service')
    await page.locator('[data-chart-series]', { hasText: 'api' }).click()
    await page.mouse.move(0, 0)
    await shoot(page, 'charts-legend-hidden')
    expect(errors).toEqual([])
  })

  for (const viz of ['area', 'bars', 'table'] as const) {
    test(`as ${viz}`, async ({ page }) => {
      const errors = watchErrors(page)
      await openChart(page, 'summarize count() by bin_auto(time), service', { viz })
      await page.mouse.move(0, 0)
      await shoot(page, `charts-${viz}`)
      expect(errors).toEqual([])
    })
  }

  test('the chart focused from the keyboard', async ({ page }) => {
    const errors = watchErrors(page)
    await openChart(page, 'summarize count() by bin_auto(time), service')
    const surface = chart(page).locator('svg.recharts-surface')
    await page.locator('[data-chart-legend] button[aria-pressed]').last().focus()
    for (let i = 0; i < 8 && !(await surface.evaluate(el => el === document.activeElement)); i++) await page.keyboard.press('Tab')
    await expect(surface).toBeFocused()
    await shoot(page, 'charts-keyboard-focus')
    expect(errors).toEqual([])
  })

  test('series of Levels, stacked', async ({ page }) => {
    const errors = watchErrors(page)
    await openChart(page, 'summarize count() by bin(time, 1h), level', { viz: 'bars' })
    await page.mouse.move(0, 0)
    await shoot(page, 'charts-levels-bars')
    expect(errors).toEqual([])
  })

  test('series of Levels, lines', async ({ page }) => {
    const errors = watchErrors(page)
    await openChart(page, 'summarize count() by bin_auto(time), level')
    await page.mouse.move(0, 0)
    await shoot(page, 'charts-levels-line')
    expect(errors).toEqual([])
  })

  test('an average with gaps', async ({ page }) => {
    const errors = watchErrors(page)
    await openChart(page, 'where level == "error"\n| summarize avg(duration_ms), count() by bin(time, 30m)')
    await page.mouse.move(0, 0)
    await shoot(page, 'charts-avg-gaps')
    expect(errors).toEqual([])
  })

  test('panels: an average and a count by service', async ({ page }) => {
    const errors = watchErrors(page)
    await openChart(page, 'summarize avg(duration_ms), count() by bin_auto(time), service')
    await page.mouse.move(0, 0)
    await shoot(page, 'charts-panels')
    expect(errors).toEqual([])
  })

  test('dragging to zoom', async ({ page }) => {
    const errors = watchErrors(page)
    await openChart(page, 'summarize count() by bin(time, 1h)')
    const box = (await chart(page).locator('.recharts-cartesian-grid').boundingBox())!
    const y = box.y + box.height / 2
    await page.mouse.move(box.x + box.width * 0.3, y)
    await page.mouse.down()
    await page.mouse.move(box.x + box.width * 0.45, y, { steps: 5 })
    await page.mouse.move(box.x + box.width * 0.6, y, { steps: 5 })
    await shoot(page, 'charts-drag')
    await page.mouse.up()
    await settled(page)
    await page.mouse.move(0, 0)
    await shoot(page, 'charts-zoomed')
    expect(errors).toEqual([])
  })

  test('bars by service', async ({ page }) => {
    const errors = watchErrors(page)
    await openChart(page, 'summarize count() by service')
    await page.mouse.move(0, 0)
    await shoot(page, 'charts-bars-by-service')
    expect(errors).toEqual([])
  })

  test('bars by service and Level', async ({ page }) => {
    const errors = watchErrors(page)
    await openChart(page, 'summarize count() by service, level')
    await page.mouse.move(0, 0)
    await shoot(page, 'charts-bars-service-level')
    expect(errors).toEqual([])
  })

  test('a series hidden from the legend stays hidden after a zoom', async ({ page }) => {
    const errors = watchErrors(page)
    await openChart(page, 'summarize count() by bin(time, 1h), service')
    await page.locator('[data-chart-series]', { hasText: 'api' }).click()
    const box = (await chart(page).locator('.recharts-cartesian-grid').boundingBox())!
    const y = box.y + box.height / 2
    await page.mouse.move(box.x + box.width * 0.3, y)
    await page.mouse.down()
    await page.mouse.move(box.x + box.width * 0.6, y, { steps: 8 })
    await page.mouse.up()
    await settled(page)
    await expect(page.locator('[data-chart-series]', { hasText: 'api' })).toHaveAttribute('aria-pressed', 'false')
    await page.mouse.move(0, 0)
    await shoot(page, 'charts-hidden-after-zoom')
    expect(errors).toEqual([])
  })

  test('bars across for many categories', async ({ page }) => {
    const errors = watchErrors(page)
    await openChart(page, 'summarize count() by message')
    await page.mouse.move(0, 0)
    await shoot(page, 'charts-many-categories')
    expect(errors).toEqual([])
  })

  test('bars by a number Attribute, in its own order', async ({ page }) => {
    const errors = watchErrors(page)
    await openChart(page, 'summarize count() by http.status')
    await page.mouse.move(0, 0)
    await shoot(page, 'charts-bars-status')
    expect(errors).toEqual([])
  })

  test('a single number', async ({ page }) => {
    const errors = watchErrors(page)
    await openChart(page, 'summarize count()')
    await expect(page.locator('[data-chart-stat]')).toBeVisible()
    await shoot(page, 'charts-stat')
    expect(errors).toEqual([])
  })

  test('many series', async ({ page }) => {
    const errors = watchErrors(page)
    await openChart(page, 'summarize count() by bin(time, 1h), service, level')
    await page.mouse.move(0, 0)
    await shoot(page, 'charts-many-series')
    expect(errors).toEqual([])
  })

  test('loading', async ({ page }) => {
    const errors = watchErrors(page)
    await page.route(/\/logs\/(query|histogram)\?/, () => {})
    const params = new URLSearchParams({ range: '24h', mode: 'query', q: `${SOURCE}\n| summarize count() by bin_auto(time), service` })
    await page.goto(logsPath(`?${params}`))
    await expect(page.getByText('Running the Query…')).toBeVisible()
    await shoot(page, 'charts-loading', { idle: false })
    expect(errors).toEqual([])
  })

  test('a mistake keeps the last chart', async ({ page }) => {
    const errors = watchErrors(page)
    await openChart(page, 'summarize count() by bin_auto(time), service')
    const box = page.getByLabel('Query', { exact: true })
    await box.fill(`${SOURCE}\n| summarize count() by bin_auto(time), service\n| where`)
    await page.getByRole('button', { name: 'Run', exact: true }).click()
    await expect(page.getByText(/^Line 3, column \d+: /)).toBeVisible()
    await expect(chart(page)).toBeVisible()
    await page.mouse.move(0, 0)
    await shoot(page, 'charts-error')
    expect(errors).toEqual([])
  })

  test('no rows', async ({ page }) => {
    const errors = watchErrors(page)
    await openChart(page, 'where service == "no-such-service"\n| summarize count() by bin_auto(time)')
    await expect(page.getByText(/^No rows/)).toBeVisible()
    await shoot(page, 'charts-empty')
    expect(errors).toEqual([])
  })
})
