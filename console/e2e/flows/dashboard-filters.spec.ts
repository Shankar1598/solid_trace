import { expect, test, type Locator, type Page, type Response } from '@playwright/test'
import { ORG } from '../support/data'
import { shoot, useTheme, watchErrors } from '../support/screens'

// Dashboard filters: a member adds a select, a multiple select and a search
// box in the Filters dialog, and charts that name them in their Queries. Each
// value picked runs again only the charts that name its filter, sits in the
// URL and survives a reload; All brings the first numbers back. Removing a
// filter that a chart names asks first, and the chart then says so.

const SOURCE = "['solid-trace-project']"
const LEVELS = 'error\nwarn\ninfo'

const card = (page: Page, title: string) => page.locator(`[data-chart-card="${title}"]`)
const cards = (page: Page) => page.locator('[data-chart-card]')
const stat = (page: Page, title: string) => card(page, title).locator('[data-chart-stat]')
const numberOf = async (page: Page, title: string) => Number((await stat(page, title).textContent())!.replace(/\D/g, ''))
const isData = (response: Response) => /\/charts\/\d+\/data\?/.test(response.url())

async function choose(page: Page, select: Locator, option: string) {
  await select.click()
  await page.getByRole('option', { name: option, exact: true }).click()
}

async function chartsRan(page: Page, count: number) {
  await expect(page.getByRole('status', { name: "Running the chart's Query" })).toHaveCount(0)
  await expect(cards(page)).toHaveCount(count)
}

/** Adds a stat chart of a Query on the dev Project, written in Query mode. */
async function addStatChart(page: Page, title: string, query: string) {
  await page.getByRole('button', { name: 'Add chart' }).click()
  const editor = page.getByRole('dialog')
  await choose(page, editor.getByRole('combobox', { name: 'Project' }).first(), 'SolidTrace Project')
  await expect(editor.locator('[data-chart-preview]').getByRole('figure')).toBeVisible()
  await editor.getByRole('group', { name: 'Edit the Query in' }).getByRole('button', { name: 'Query' }).click()
  await editor.getByLabel('Query', { exact: true }).fill(query)
  await editor.getByLabel('Title').fill(title)
  await editor.getByRole('group', { name: 'Viz' }).getByRole('button', { name: 'Stat' }).click()
  await editor.getByRole('button', { name: 'Add chart' }).click()
  await expect(editor).toHaveCount(0)
}

/** Fills the dialog's next new filter. */
async function addFilter(dialog: Locator, n: number, filter: { name: string; label: string; kind?: 'Search box'; multiple?: boolean; options?: string }) {
  await dialog.getByRole('button', { name: 'Add filter' }).click()
  await dialog.getByLabel(`Filter ${n} name`).fill(filter.name)
  await dialog.getByLabel(`Filter ${n} label`).fill(filter.label)
  if (filter.kind) await dialog.getByRole('group', { name: `Filter ${n} kind` }).getByRole('button', { name: filter.kind }).click()
  if (filter.multiple) await dialog.getByRole('group', { name: `Filter ${n}` }).getByRole('checkbox').click()
  if (filter.options) await dialog.getByLabel(`Filter ${n} options`).fill(filter.options)
}

test('a member filters a Dashboard\'s charts from its filter bar, and the URL keeps the values', async ({ page }) => {
  test.setTimeout(150_000)
  await useTheme(page, test.info())
  const errors = watchErrors(page)
  const name = `Filters flow ${Date.now()}`

  await page.goto(`/${ORG}/dashboards`)
  await page.getByRole('button', { name: 'New dashboard' }).first().click()
  await page.getByLabel('Name').fill(name)
  await page.getByRole('button', { name: 'Create dashboard' }).click()
  await expect(page.getByRole('heading', { name })).toBeVisible()
  // A fixed range, so each run of a chart counts the same Logs.
  const to = new Date(Math.floor(Date.now() / 60_000) * 60_000)
  const from = new Date(to.getTime() - 24 * 3600_000)
  await page.goto(`${new URL(page.url()).pathname}?from=${from.toISOString()}&to=${to.toISOString()}`)

  // The Filters dialog adds a select, a multiple select and a search box.
  await page.getByRole('button', { name: 'Filters', exact: true }).click()
  const dialog = page.getByRole('dialog')
  await addFilter(dialog, 1, { name: 'level', label: 'Level', options: LEVELS })
  await addFilter(dialog, 2, { name: 'levels', label: 'Levels', multiple: true, options: LEVELS })
  await addFilter(dialog, 3, { name: 'q', label: 'Find', kind: 'Search box' })
  await shoot(page, 'dashboard-filters-dialog')
  await dialog.getByRole('button', { name: 'Save' }).click()
  await expect(dialog).toHaveCount(0)
  const level = page.getByRole('combobox', { name: 'Level', exact: true })
  const levels = page.getByRole('combobox', { name: 'Levels', exact: true })
  await expect(level).toHaveText(/Level: All/)

  await addStatChart(page, 'Level count', `${SOURCE}\n| where level == $level\n| summarize count()`)
  await addStatChart(page, 'Total', `${SOURCE}\n| summarize count()`)
  await addStatChart(page, 'Levels count', `${SOURCE}\n| where level == $levels\n| summarize count()`)
  await addStatChart(page, 'Found', `${SOURCE}\n| search $q\n| summarize count()`)
  await chartsRan(page, 4)
  const all = await numberOf(page, 'Total')
  expect(all).toBeGreaterThan(0)
  await expect(stat(page, 'Level count')).toHaveText(await stat(page, 'Total').textContent() ?? '')

  // Picking error runs only the chart that names $level, with it in the URL.
  const runs: string[] = []
  page.on('request', request => { if (/\/charts\/\d+\/data\?/.test(request.url())) runs.push(request.url()) })
  let ran = page.waitForResponse(r => isData(r) && new URL(r.url()).searchParams.get('v_level') === 'error')
  await choose(page, level, 'error')
  await ran
  await chartsRan(page, 4)
  await expect(page).toHaveURL(/[?&]v_level=error(&|$)/)
  await expect(level).toHaveText(/Level: error/)
  await expect.poll(() => numberOf(page, 'Level count')).not.toBe(all)
  const errorsOnly = await numberOf(page, 'Level count')
  expect(errorsOnly).toBeGreaterThan(0)
  expect(errorsOnly).toBeLessThan(all)
  expect(runs).toHaveLength(1)
  expect(new URL(runs[0]).searchParams.getAll('v_level')).toEqual(['error'])
  await shoot(page, 'dashboard-filters-bar')

  // The chart editor's preview runs with the bar's values.
  await page.getByRole('button', { name: 'Menu for Level count' }).click()
  const preview = page.waitForRequest(r => r.method() === 'POST' && new URL(r.url()).pathname.endsWith('/charts/preview'))
  await page.getByRole('menuitem', { name: 'Edit' }).click()
  expect((await preview).postDataJSON().parameters).toEqual({ level: ['error'] })
  await page.getByRole('dialog').getByRole('button', { name: 'Cancel' }).click()

  // A reload keeps the value, and its chart's number.
  await page.reload()
  await chartsRan(page, 4)
  await expect(level).toHaveText(/Level: error/)
  expect(await numberOf(page, 'Level count')).toBe(errorsOnly)

  // All drops the filter again.
  await choose(page, level, 'All')
  await expect(page).not.toHaveURL(/v_level=/)
  await expect.poll(() => numberOf(page, 'Level count')).toBe(all)

  // A multiple select holds two levels, as a repeated key.
  ran = page.waitForResponse(r => isData(r) && new URL(r.url()).searchParams.getAll('v_levels').length === 2)
  await levels.click()
  await page.getByRole('option', { name: 'error', exact: true }).click()
  await page.getByRole('option', { name: 'warn', exact: true }).click()
  await page.keyboard.press('Escape')
  await ran
  await chartsRan(page, 4)
  await expect(page).toHaveURL(/v_levels=error&v_levels=warn/)
  await expect(levels).toHaveText(/Levels: error, warn/)
  await expect.poll(() => numberOf(page, 'Levels count')).toBeGreaterThan(errorsOnly)
  expect(await numberOf(page, 'Levels count')).toBeLessThan(all)

  // A search box runs on Enter, and clearing it is All.
  const find = page.getByRole('textbox', { name: 'Find' })
  ran = page.waitForResponse(r => isData(r) && new URL(r.url()).searchParams.get('v_q') === 'Upstream')
  await find.fill('Upstream')
  await find.press('Enter')
  await ran
  await chartsRan(page, 4)
  await expect(page).toHaveURL(/v_q=Upstream/)
  const found = await numberOf(page, 'Found')
  expect(found).toBeGreaterThan(0)
  expect(found).toBeLessThan(all)
  await page.getByRole('button', { name: 'Clear Find' }).click()
  await expect(page).not.toHaveURL(/v_q=/)
  await expect.poll(() => numberOf(page, 'Found')).toBe(all)

  // Removing a filter a chart names asks first; the chart then says so.
  await page.getByRole('button', { name: 'Filters', exact: true }).click()
  await dialog.getByRole('button', { name: 'Remove filter 3' }).click()
  await expect(dialog.getByRole('alert')).toContainText('Found names $q and will run it as All.')
  await dialog.getByRole('button', { name: 'Remove filter', exact: true }).click()
  await dialog.getByRole('button', { name: 'Save' }).click()
  await expect(dialog).toHaveCount(0)
  await expect(page.getByRole('textbox', { name: 'Find' })).toHaveCount(0)
  await expect(card(page, 'Found')).toContainText("$q isn't one of this Dashboard's filters")
  await expect(page).toHaveURL(/v_levels=error&v_levels=warn/)

  await page.getByRole('button', { name: 'Dashboard settings' }).click()
  await page.getByRole('button', { name: 'Delete dashboard' }).click()
  await page.getByRole('alertdialog').getByRole('button', { name: 'Delete dashboard' }).click()
  await expect(page.getByRole('heading', { name: 'Dashboards', exact: true })).toBeVisible()
  expect(errors).toEqual([])
})

test('a member makes a chart in the Builder with a filter as a chip\'s value, and can\'t save a Query naming one the Dashboard lacks', async ({ page }) => {
  test.setTimeout(120_000)
  await useTheme(page, test.info())
  const errors = watchErrors(page)
  const name = `Builder filters flow ${Date.now()}`

  await page.goto(`/${ORG}/dashboards`)
  await page.getByRole('button', { name: 'New dashboard' }).first().click()
  await page.getByLabel('Name').fill(name)
  await page.getByRole('button', { name: 'Create dashboard' }).click()
  await expect(page.getByRole('heading', { name })).toBeVisible()
  const to = new Date(Math.floor(Date.now() / 60_000) * 60_000)
  const from = new Date(to.getTime() - 24 * 3600_000)
  await page.goto(`${new URL(page.url()).pathname}?from=${from.toISOString()}&to=${to.toISOString()}`)

  await page.getByRole('button', { name: 'Filters', exact: true }).click()
  const dialog = page.getByRole('dialog')
  await addFilter(dialog, 1, { name: 'level', label: 'Level', options: LEVELS })
  await addFilter(dialog, 2, { name: 'q', label: 'Find', kind: 'Search box' })
  await dialog.getByRole('button', { name: 'Save' }).click()
  await expect(dialog).toHaveCount(0)
  await addStatChart(page, 'Total', `${SOURCE}\n| summarize count()`)

  // In the Builder, the level chip's value is the level filter, and the search is the q filter.
  await page.getByRole('button', { name: 'Add chart' }).click()
  const editor = page.getByRole('dialog')
  await choose(page, editor.getByRole('combobox', { name: 'Project' }).first(), 'SolidTrace Project')
  await expect(editor.locator('[data-chart-preview]').getByRole('figure')).toBeVisible()
  await editor.getByRole('button', { name: 'Remove bin_auto(time)' }).click()
  await editor.getByRole('button', { name: 'Add a filter' }).click()
  await page.getByLabel('Filter on a Field…').fill('level')
  await page.getByRole('option', { name: /^level\b/ }).first().click()
  await shoot(page, 'dashboard-filters-builder-picker')
  await page.getByRole('option', { name: /^\$level/ }).click()
  await expect(editor.getByRole('button', { name: 'level == $level', exact: true })).toBeVisible()
  const previewed = page.waitForRequest(r => r.method() === 'POST' && new URL(r.url()).pathname.endsWith('/charts/preview') &&
    r.postDataJSON()?.query === `${SOURCE}\n| where level == $level\n| search $q\n| summarize count()`)
  await editor.getByRole('button', { name: 'Add a search, sort or limit' }).click()
  await page.getByRole('option', { name: /^search/ }).click()
  await page.getByRole('option', { name: /^\$q/ }).click()
  await expect(editor.getByRole('button', { name: 'Remove search $q' })).toBeVisible()
  await previewed
  await shoot(page, 'dashboard-filters-builder-chip')
  await editor.getByLabel('Title').fill('Level chip')
  await editor.getByRole('group', { name: 'Viz' }).getByRole('button', { name: 'Stat' }).click()
  await editor.getByRole('button', { name: 'Add chart' }).click()
  await expect(editor).toHaveCount(0)
  await chartsRan(page, 2)
  const all = await numberOf(page, 'Total')
  await expect.poll(() => numberOf(page, 'Level chip')).toBe(all)

  // The saved chart follows the bar.
  const ran = page.waitForResponse(r => isData(r) && new URL(r.url()).searchParams.get('v_level') === 'error')
  await choose(page, page.getByRole('combobox', { name: 'Level', exact: true }), 'error')
  await ran
  await chartsRan(page, 2)
  await expect.poll(() => numberOf(page, 'Level chip')).not.toBe(all)
  const errorsOnly = await numberOf(page, 'Level chip')
  expect(errorsOnly).toBeGreaterThan(0)
  expect(errorsOnly).toBeLessThan(all)

  // Opened again, the Builder reads the filter chip and the search filter back.
  await page.getByRole('button', { name: 'Menu for Level chip' }).click()
  await page.getByRole('menuitem', { name: 'Edit' }).click()
  await expect(editor.getByRole('button', { name: 'level == $level', exact: true })).toBeVisible()
  await expect(editor.getByRole('button', { name: 'Remove search $q' })).toBeVisible()
  await editor.getByRole('button', { name: 'Cancel' }).click()
  await expect(editor).toHaveCount(0)

  // A Query naming a filter the Dashboard lacks can't be saved.
  await page.getByRole('button', { name: 'Add chart' }).click()
  await choose(page, editor.getByRole('combobox', { name: 'Project' }).first(), 'SolidTrace Project')
  await editor.getByRole('group', { name: 'Edit the Query in' }).getByRole('button', { name: 'Query' }).click()
  await editor.getByLabel('Query', { exact: true }).fill(`${SOURCE}\n| where level == $nope\n| summarize count()`)
  await expect(editor.getByText("Line 2, column 18: $nope isn't one of this Dashboard's filters (level, q).").first()).toBeVisible()
  await expect(editor.getByRole('button', { name: 'Add chart' })).toBeDisabled()
  await editor.getByRole('button', { name: 'Cancel' }).click()
  await expect(cards(page)).toHaveCount(2)

  await page.getByRole('button', { name: 'Dashboard settings' }).click()
  await page.getByRole('button', { name: 'Delete dashboard' }).click()
  await page.getByRole('alertdialog').getByRole('button', { name: 'Delete dashboard' }).click()
  await expect(page.getByRole('heading', { name: 'Dashboards', exact: true })).toBeVisible()
  expect(errors).toEqual([])
})
