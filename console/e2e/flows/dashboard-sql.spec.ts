import { expect, test, type Locator, type Page, type Response } from '@playwright/test'
import { ORG } from '../support/data'
import { expectEditorText, holdSqlQueries, releaseSqlQueries, setSqlQueries } from '../support/logs'
import { watchErrors } from '../support/screens'

// SQL Charts on a Dashboard, with SQL queries on: a member adds a chart in
// the editor's SQL tab, which opens with the Query's readable SQL and shows
// how SQL names a filter. The SQL names a select and a search box as
// ClickHouse query parameters; the filter bar narrows the chart, and All
// gives every Log again. SQL that gives a filter another type answers its
// error in the editor, and Save stays off. With SQL queries off, the chart
// says so in place of its data, and the editor has no SQL tab.

test.beforeAll(() => { holdSqlQueries() })
test.afterAll(() => { releaseSqlQueries() })

const SOURCE = "['solid-trace-project']"
const SQL = `SELECT count() AS n FROM logs
WHERE (empty({level:Array(String)}) OR level IN {level:Array(String)})
AND ({q:String} = '' OR positionCaseInsensitive(message, {q:String}) > 0)`

// The off chart's data answers 403, a preview of a bad type 400, which the browser logs.
const REFUSED = /status of (400 \(Bad Request\)|403 \(Forbidden\))/

const card = (page: Page, title: string) => page.locator(`[data-chart-card="${title}"]`)
const stat = (page: Page, title: string) => card(page, title).locator('[data-chart-stat]')
const numberOf = async (page: Page, title: string) => Number((await stat(page, title).textContent())!.replace(/\D/g, ''))
const isData = (response: Response) => /\/charts\/\d+\/data\?/.test(response.url())

async function choose(page: Page, select: Locator, option: string) {
  await select.click()
  await page.getByRole('option', { name: option, exact: true }).click()
}

async function chartsRan(page: Page, count: number) {
  await expect(page.getByRole('status', { name: "Running the chart's Query" })).toHaveCount(0)
  await expect(page.locator('[data-chart-card]')).toHaveCount(count)
}

test('a member adds a SQL Chart that the filter bar narrows, and sees it while SQL queries are off', async ({ page }) => {
  test.setTimeout(180_000)
  const errors = watchErrors(page, [REFUSED])
  const name = `SQL chart flow ${Date.now()}`

  await page.goto(`/${ORG}/dashboards`)
  await page.getByRole('button', { name: 'New dashboard' }).first().click()
  await page.getByLabel('Name').fill(name)
  await page.getByRole('button', { name: 'Create dashboard' }).click()
  await expect(page.getByRole('heading', { name })).toBeVisible()
  const to = new Date(Math.floor(Date.now() / 60_000) * 60_000)
  const from = new Date(to.getTime() - 24 * 3600_000)
  const path = new URL(page.url()).pathname
  await page.goto(`${path}?from=${from.toISOString()}&to=${to.toISOString()}`)

  // A select of levels and a search box.
  await page.getByRole('button', { name: 'Filters', exact: true }).click()
  const filters = page.getByRole('dialog')
  await filters.getByRole('button', { name: 'Add filter' }).click()
  await filters.getByLabel('Filter 1 name').fill('level')
  await filters.getByLabel('Filter 1 label').fill('Level')
  await filters.getByLabel('Filter 1 options').fill('error\nwarn\ninfo')
  await filters.getByRole('button', { name: 'Add filter' }).click()
  await filters.getByLabel('Filter 2 name').fill('q')
  await filters.getByLabel('Filter 2 label').fill('Find')
  await filters.getByRole('group', { name: 'Filter 2 kind' }).getByRole('button', { name: 'Search box' }).click()
  await filters.getByRole('button', { name: 'Save' }).click()
  await expect(filters).toHaveCount(0)

  // A Query chart of every Log, to compare with.
  await page.getByRole('button', { name: 'Add chart' }).click()
  let editor = page.getByRole('dialog')
  await choose(page, editor.getByRole('combobox', { name: 'Project' }).first(), 'SolidTrace Project')
  await editor.getByRole('group', { name: 'Edit the Query in' }).getByRole('button', { name: 'Query' }).click()
  await editor.getByLabel('Query', { exact: true }).fill(`${SOURCE}\n| summarize count()`)
  await editor.getByLabel('Title').fill('Total')
  await editor.getByRole('group', { name: 'Viz' }).getByRole('button', { name: 'Stat' }).click()
  await editor.getByRole('button', { name: 'Add chart' }).click()
  await expect(editor).toHaveCount(0)

  // The SQL tab opens with the Query's readable SQL, and says how SQL names a filter.
  await page.getByRole('button', { name: 'Add chart' }).click()
  editor = page.getByRole('dialog')
  await choose(page, editor.getByRole('combobox', { name: 'Project' }).first(), 'SolidTrace Project')
  await editor.getByRole('group', { name: 'Edit the Query in' }).getByRole('button', { name: 'SQL' }).click()
  const sql = editor.locator('[data-chart-sql]')
  await expectEditorText(sql, /FROM logs/)
  await expect(editor.locator('[data-chart-sql-help]')).toContainText('(empty({level:Array(String)}) OR service IN {level:Array(String)})')
  await expect(editor.locator('[data-chart-sql-help]')).toContainText("({q:String} = '' OR positionCaseInsensitive(message, {q:String}) > 0)")

  // A filter of another type answers its error, and Save stays off.
  await editor.getByLabel('SQL', { exact: true }).fill('SELECT count() FROM logs WHERE level = {level:UInt8}')
  await expect(editor.locator('[data-chart-error]')).toContainText('{level:UInt8}: a Dashboard filter\'s type must be Array(String), String or Nullable(String)')
  await expect(editor.getByRole('button', { name: 'Add chart' })).toBeDisabled()

  await editor.getByLabel('SQL', { exact: true }).fill(SQL)
  await editor.getByLabel('Title').fill('SQL count')
  await editor.getByRole('group', { name: 'Viz' }).getByRole('button', { name: 'Stat' }).click()
  await expect(editor.locator('[data-chart-preview] [data-chart-stat]')).toBeVisible()
  await editor.getByRole('button', { name: 'Add chart' }).click()
  await expect(editor).toHaveCount(0)
  await chartsRan(page, 2)
  const all = await numberOf(page, 'Total')
  expect(all).toBeGreaterThan(0)
  await expect.poll(() => numberOf(page, 'SQL count')).toBe(all)

  // error narrows the SQL Chart, and only it runs again.
  const runs: string[] = []
  page.on('request', request => { if (/\/charts\/\d+\/data\?/.test(request.url())) runs.push(request.url()) })
  const level = page.getByRole('combobox', { name: 'Level', exact: true })
  let ran = page.waitForResponse(r => isData(r) && new URL(r.url()).searchParams.get('v_level') === 'error')
  await choose(page, level, 'error')
  await ran
  await chartsRan(page, 2)
  await expect.poll(() => numberOf(page, 'SQL count')).toBeLessThan(all)
  const errorsOnly = await numberOf(page, 'SQL count')
  expect(errorsOnly).toBeGreaterThan(0)
  expect(runs).toHaveLength(1)
  expect(await numberOf(page, 'Total')).toBe(all)

  // All gives every Log again.
  await choose(page, level, 'All')
  await expect(page).not.toHaveURL(/v_level=/)
  await expect.poll(() => numberOf(page, 'SQL count')).toBe(all)

  // A search box narrows it to the matching Logs, and empty is All.
  const find = page.getByRole('textbox', { name: 'Find' })
  ran = page.waitForResponse(r => isData(r) && new URL(r.url()).searchParams.get('v_q') === 'Upstream')
  await find.fill('Upstream')
  await find.press('Enter')
  await ran
  await chartsRan(page, 2)
  await expect.poll(() => numberOf(page, 'SQL count')).toBeLessThan(all)
  expect(await numberOf(page, 'SQL count')).toBeGreaterThan(0)
  await page.getByRole('button', { name: 'Clear Find' }).click()
  await expect.poll(() => numberOf(page, 'SQL count')).toBe(all)

  // With SQL queries off, the chart says so in place of its data, and the editor has no SQL tab.
  setSqlQueries(false)
  try {
    await page.reload()
    await expect(card(page, 'SQL count').locator('[data-chart-error]')).toHaveText('SQL queries are turned off on this instance.')
    await expect.poll(() => numberOf(page, 'Total')).toBe(all)
    await page.getByRole('button', { name: 'Add chart' }).click()
    editor = page.getByRole('dialog')
    await expect(editor.getByRole('group', { name: 'Edit the Query in' }).getByRole('button', { name: 'Query' })).toBeVisible()
    await expect(editor.getByRole('group', { name: 'Edit the Query in' }).getByRole('button', { name: 'SQL' })).toHaveCount(0)
    await editor.getByRole('button', { name: 'Cancel' }).click()
  } finally {
    setSqlQueries(true)
  }
  await page.reload()
  await chartsRan(page, 2)
  await expect.poll(() => numberOf(page, 'SQL count')).toBe(all)

  await page.getByRole('button', { name: 'Dashboard settings' }).click()
  await page.getByRole('button', { name: 'Delete dashboard' }).click()
  await page.getByRole('alertdialog').getByRole('button', { name: 'Delete dashboard' }).click()
  await expect(page.getByRole('heading', { name: 'Dashboards', exact: true })).toBeVisible()
  expect(errors).toEqual([])
})
