import { test, expect, type Page } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import { resolve } from 'node:path'
import { logsPath } from '../support/data'
import { addFilter, builderRow, last24h, openLogs, settled, streamRows } from '../support/logs'
import { expectFits, failedLoad, shoot, useTheme, watchErrors } from '../support/screens'

// Every state of the Logs v1 explorer the flows click through, for reading by
// eye at both sizes in both themes: the Builder and its steps, Query and SQL
// modes, the histogram zoomed, the inspector, and loading, empty and error.

test.beforeEach(async ({ page }, info) => { await useTheme(page, info) })

const SOURCE = "['solid-trace-project']"
const inspector = (page: Page) => page.getByRole('complementary', { name: 'Log inspector' })

/** Opens a stream row in the inspector, clicking near its start: a cell's right end holds its Copy button. */
async function openRow(page: Page, n: number) {
  await streamRows(page).nth(n).click({ position: { x: 60, y: 15 } })
  await expect(inspector(page)).toBeVisible()
}

test.describe('Builder', () => {
  test('loaded', async ({ page }) => {
    const errors = watchErrors(page)
    await openLogs(page)
    await shoot(page, 'v1-builder')
    expect(errors).toEqual([])
  })

  test('loading', async ({ page }) => {
    const errors = watchErrors(page)
    // Hold the run and the histogram so their loading states stay on screen.
    await page.route(/\/logs\/(query|histogram)\?/, () => {})
    await page.goto(logsPath(last24h()))
    await expect(page.getByText('Counting Logs…')).toBeVisible()
    await shoot(page, 'v1-builder-loading', { idle: false })
    expect(errors).toEqual([])
  })

  test('EventStore unavailable', async ({ page }) => {
    // Console answers 502 when EventStore is down; the browser logs each as a failed load.
    const errors = watchErrors(page, [failedLoad(502)])
    await page.route(/\/logs\/(query|histogram)\?/, route => route.fulfill({
      status: 502, contentType: 'application/json', body: JSON.stringify({ error: 'EventStore is unavailable. Try again shortly.' }),
    }))
    await page.goto(logsPath(last24h()))
    await expect(page.getByText('EventStore is unavailable. Try again shortly.').first()).toBeVisible()
    // On a phone the summary line's message wraps, inside the strip.
    await expectFits(page.getByText(/^Not counted: /))
    await shoot(page, 'v1-builder-unavailable')
    expect(errors).toEqual([])
  })

  test('cancelled', async ({ page }) => {
    const errors = watchErrors(page)
    await openLogs(page)
    await page.route(/\/logs\/(query|histogram)\?/, () => {})
    await page.getByRole('button', { name: 'Run', exact: true }).click()
    await page.getByRole('button', { name: 'Cancel' }).click()
    await expect(page.getByText('Query cancelled. Run it again to see Logs.')).toBeVisible()
    await shoot(page, 'v1-builder-cancelled', { idle: false })
    expect(errors).toEqual([])
  })

  test('filter: the Field step', async ({ page }) => {
    const errors = watchErrors(page)
    await openLogs(page)
    await page.getByRole('button', { name: 'Add a filter' }).click()
    await expect(page.getByLabel('Filter on a Field…')).toBeVisible()
    await shoot(page, 'v1-builder-filter-field')
    await page.getByLabel('Filter on a Field…').fill('http')
    await shoot(page, 'v1-builder-filter-field-typed')
    expect(errors).toEqual([])
  })

  test('filter: the operator and value step', async ({ page }) => {
    const errors = watchErrors(page)
    await openLogs(page)
    await page.getByRole('button', { name: 'Add a filter' }).click()
    await page.getByLabel('Filter on a Field…').fill('service')
    await page.getByRole('option', { name: /^service/ }).first().click()
    await expect(page.getByLabel('Operator')).toBeVisible()
    await shoot(page, 'v1-builder-filter-value')
    await page.getByLabel('Operator').selectOption('in')
    await page.getByLabel('Values').fill('api, ')
    await shoot(page, 'v1-builder-filter-list')
    expect(errors).toEqual([])
  })

  test('filtered', async ({ page }) => {
    const errors = watchErrors(page)
    await openLogs(page)
    await addFilter(page, 'service', '==', 'billing')
    await addFilter(page, 'level', '!=', 'debug')
    await shoot(page, 'v1-builder-filtered')
    expect(errors).toEqual([])
  })

  test('no Logs match', async ({ page }) => {
    const errors = watchErrors(page)
    await openLogs(page)
    await addFilter(page, 'service', '==', 'no-such-service')
    await page.mouse.move(0, 0)
    await shoot(page, 'v1-builder-empty')
    expect(errors).toEqual([])
  })

  test('Summarize: a table', async ({ page }) => {
    const errors = watchErrors(page)
    await openLogs(page)
    await page.getByRole('button', { name: 'Add a function' }).click()
    await shoot(page, 'v1-builder-summarize-fn')
    await page.getByRole('option', { name: /^count/ }).click()
    await settled(page)
    await page.getByRole('button', { name: 'Group by a Field or time' }).click()
    await expect(page.getByLabel('Group by time or a Field…')).toBeVisible()
    await shoot(page, 'v1-builder-summarize-by')
    await page.getByLabel('Group by time or a Field…').fill('service')
    await page.getByRole('option', { name: /^service/ }).first().click()
    await settled(page)
    // by leads its group on one line, also when the clauses wrap on a phone.
    const summarize = builderRow(page, 'Summarize')
    const by = (await summarize.getByText('by', { exact: true }).boundingBox())!
    const group = (await summarize.getByRole('button', { name: 'service', exact: true }).boundingBox())!
    expect(Math.abs(by.y + by.height / 2 - (group.y + group.height / 2))).toBeLessThan(4)
    await shoot(page, 'v1-builder-summarize')
    // A cell's value menu, which filters from the table.
    await page.getByRole('button', { name: 'billing', exact: true }).first().click()
    await expect(page.getByRole('menuitem', { name: 'Show only this' })).toBeVisible()
    await shoot(page, 'v1-builder-summarize-value-menu')
    expect(errors).toEqual([])
  })

  test('More: search, sort and limit', async ({ page }) => {
    const errors = watchErrors(page)
    await openLogs(page)
    await page.getByRole('button', { name: 'Add a search, sort or limit' }).click()
    await shoot(page, 'v1-builder-more')
    await page.getByRole('option', { name: /^search/ }).click()
    await page.getByLabel('Text', { exact: true }).fill('orders')
    await shoot(page, 'v1-builder-more-search')
    await page.getByRole('button', { name: 'Apply' }).click()
    await settled(page)
    await page.getByRole('button', { name: 'Add a sort or limit' }).click()
    await page.getByRole('option', { name: /^sort by/ }).click()
    await shoot(page, 'v1-builder-more-sort')
    await page.getByLabel('Sort by a Field…').fill('level')
    await page.getByRole('option', { name: /^level/ }).first().click()
    await settled(page)
    await page.getByRole('button', { name: 'Add a limit' }).click()
    await shoot(page, 'v1-builder-more-limit')
    await page.getByRole('button', { name: '100', exact: true }).click()
    await settled(page)
    await expect(builderRow(page, 'Limit')).toBeVisible()
    await shoot(page, 'v1-builder-search-sort-limit')
    expect(errors).toEqual([])
  })

  test('time range picker', async ({ page }) => {
    const errors = watchErrors(page)
    await openLogs(page)
    await page.getByRole('combobox', { name: 'Time range' }).click()
    await expect(page.getByRole('option', { name: 'Last 7 days' })).toBeVisible()
    await shoot(page, 'v1-range-picker')
    expect(errors).toEqual([])
  })
})

test.describe('Query mode', () => {
  test('a Query run', async ({ page }) => {
    const errors = watchErrors(page)
    await openLogs(page)
    await page.getByRole('button', { name: 'Query', exact: true }).click()
    const box = page.getByLabel('Query', { exact: true })
    await box.fill(`${SOURCE}\n| where level == "error"\n| summarize count() by service, bin(time, 1h)`)
    await box.press('Control+Enter')
    await settled(page)
    await shoot(page, 'v1-query')
    expect(errors).toEqual([])
  })

  test('a mistake', async ({ page }) => {
    const errors = watchErrors(page)
    await openLogs(page)
    await page.getByRole('button', { name: 'Query', exact: true }).click()
    const box = page.getByLabel('Query', { exact: true })
    await box.fill(`${SOURCE}\n| where level ==`)
    await page.getByRole('button', { name: 'Run', exact: true }).click()
    await expect(page.getByText(/^Line 2, column \d+: /)).toBeVisible()
    await shoot(page, 'v1-query-error')
    expect(errors).toEqual([])
  })

  test('leaving for the Builder asks', async ({ page }) => {
    const errors = watchErrors(page)
    await openLogs(page)
    await page.getByRole('button', { name: 'Query', exact: true }).click()
    const box = page.getByLabel('Query', { exact: true })
    await box.fill(`${SOURCE}\n| where level == "warn"\n| project time, service, message`)
    await box.press('Control+Enter')
    await settled(page)
    await page.getByRole('button', { name: 'Builder', exact: true }).click()
    await expect(page.getByText(/The Builder can't show/)).toBeVisible()
    await shoot(page, 'v1-query-leaving')
    expect(errors).toEqual([])
  })
})

test.describe('SQL mode', () => {
  const sqlQueries = (on: boolean) => execFileSync('mise', ['exec', '--', 'bin/rails', 'instance_settings:set', `SQL_QUERIES_ENABLED=${on}`], {
    cwd: resolve(__dirname, '../..'), stdio: 'pipe', timeout: 120_000,
  })
  test.beforeAll(() => { sqlQueries(true) })
  test.afterAll(() => { sqlQueries(false) })

  test('opened, run, and leaving', async ({ page }) => {
    const errors = watchErrors(page)
    await openLogs(page)
    await page.getByRole('button', { name: 'SQL', exact: true }).click()
    const box = page.getByLabel('SQL', { exact: true })
    await expect(box).toHaveValue(/FROM logs/i)
    await shoot(page, 'v1-sql-waiting')
    await box.fill('SELECT service, level, count(*) AS n\nFROM logs\nGROUP BY service, level\nORDER BY n DESC')
    await box.press('Control+Enter')
    await settled(page)
    await expect(page.locator('.sticky.top-0').filter({ hasText: 'service' })).toBeVisible()
    await shoot(page, 'v1-sql-table')
    await page.getByRole('button', { name: 'Builder', exact: true }).click()
    await expect(page.getByText(/SQL can't be turned back into a Query/)).toBeVisible()
    await shoot(page, 'v1-sql-leaving')
    expect(errors).toEqual([])
  })

  test('running and cancelled', async ({ page }) => {
    const errors = watchErrors(page)
    await openLogs(page)
    await page.getByRole('button', { name: 'SQL', exact: true }).click()
    await expect(page.getByLabel('SQL', { exact: true })).toHaveValue(/FROM logs/i)
    // Hold the SQL so its run stays on screen.
    await page.route(/\/logs\/sql$/, () => {})
    await page.getByRole('button', { name: 'Run', exact: true }).click()
    await expect(page.getByText('Running the SQL…')).toBeVisible()
    await shoot(page, 'v1-sql-running', { idle: false })
    await page.getByRole('button', { name: 'Cancel' }).click()
    await expect(page.getByText('SQL cancelled. Run it again.')).toBeVisible()
    await shoot(page, 'v1-sql-cancelled', { idle: false })
    expect(errors).toEqual([])
  })

  test('a mistake', async ({ page }) => {
    const errors = watchErrors(page, [failedLoad(400)])
    await openLogs(page)
    await page.getByRole('button', { name: 'SQL', exact: true }).click()
    const box = page.getByLabel('SQL', { exact: true })
    // Longer than a phone is wide: its LINE scrolls sideways there, the caret under the mistake.
    await box.fill('SELECT service, level, count(*) AS n FROM logs WHERE nope = 1 GROUP BY service, level')
    await page.getByRole('button', { name: 'Run', exact: true }).click()
    await expect(page.getByRole('alert')).toContainText('Referenced column "nope" not found')
    await shoot(page, 'v1-sql-error')
    expect(errors).toEqual([])
  })
})

test.describe('Histogram', () => {
  test('dragging and zoomed', async ({ page }) => {
    const errors = watchErrors(page)
    await openLogs(page)
    const strip = page.locator('.cursor-crosshair')
    const box = (await strip.boundingBox())!
    const y = box.y + box.height / 2
    await page.mouse.move(box.x + box.width * 0.55, y)
    await page.mouse.down()
    await page.mouse.move(box.x + box.width * 0.7, y, { steps: 5 })
    await shoot(page, 'v1-histogram-dragging')
    await page.mouse.up()
    await settled(page)
    await expect(page).toHaveURL(/from=/)
    await shoot(page, 'v1-histogram-zoomed')
    expect(errors).toEqual([])
  })
})

test.describe('Inspector', () => {
  test('Table and JSON', async ({ page }) => {
    const errors = watchErrors(page)
    await openLogs(page)
    await openRow(page, 1)
    await inspector(page).getByRole('tab', { name: 'Table' }).click()
    await page.waitForTimeout(300)
    await shoot(page, 'v1-inspector-table')
    // A line's buttons, on hover.
    const row = inspector(page).locator('div.group', { has: page.getByText('service', { exact: true }) }).first()
    await row.hover()
    await row.getByRole('button').first().hover()
    await page.waitForTimeout(400)
    await shoot(page, 'v1-inspector-line-actions')
    await inspector(page).getByRole('tab', { name: 'JSON' }).click()
    await page.mouse.move(0, 0)
    await shoot(page, 'v1-inspector-json')
    expect(errors).toEqual([])
  })

  test('the value menu', async ({ page }) => {
    const errors = watchErrors(page)
    await openLogs(page)
    const row = streamRows(page).nth(1)
    const service = (await row.locator('> div > div:nth-child(3)').innerText()).trim()
    await openRow(page, 1)
    await inspector(page).locator('header').getByRole('button', { name: service, exact: true }).click()
    await expect(page.getByRole('menuitem', { name: 'Show only this' })).toBeVisible()
    await shoot(page, 'v1-inspector-value-menu')
    expect(errors).toEqual([])
  })
})
