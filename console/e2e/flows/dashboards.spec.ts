import { expect, test, type Locator, type Page } from '@playwright/test'
import { ORG } from '../support/data'
import { last24h, openLogs, pickQuickRange, runBy } from '../support/logs'
import { failedLoad, watchErrors } from '../support/screens'

// Dashboards: a member makes one, adds a chart from the explorer's Add to
// dashboard and another in the chart editor, changes the range and sees the
// charts run again, moves a chart and finds it moved after a reload, then
// deletes a chart and the Dashboard.

const SOURCE = "['solid-trace-project']"

const card = (page: Page, title: string) => page.locator(`[data-chart-card="${title}"]`)
const cards = (page: Page) => page.locator('[data-chart-card]')

/** Picks an option of a Select, found by its accessible name. */
async function choose(page: Page, select: Locator, option: string) {
  await select.click()
  await page.getByRole('option', { name: option, exact: true }).click()
}

/** Waits for each of `count` charts to have run: its data answered, and nothing left running. */
async function chartsRan(page: Page, count: number) {
  await expect(page.getByRole('status', { name: "Running the chart's Query" })).toHaveCount(0)
  await expect(cards(page)).toHaveCount(count)
}

async function size(locator: Locator) {
  const box = await locator.boundingBox()
  if (!box) throw new Error('not on screen')
  return { width: Math.round(box.width), height: Math.round(box.height) }
}

test('a member builds a Dashboard from the explorer and the editor, ranges it, lays it out and deletes it', async ({ page }) => {
  const errors = watchErrors(page)
  const name = `Flow dashboard ${Date.now()}`

  // Make the Dashboard from the list; it opens empty, saying how to start.
  await page.goto(`/${ORG}/dashboards`)
  await page.getByRole('button', { name: 'New dashboard' }).first().click()
  await page.getByLabel('Name').fill(name)
  await page.getByRole('button', { name: 'Create dashboard' }).click()
  await expect(page.getByRole('heading', { name })).toBeVisible()
  await expect(page.getByText(/No charts yet/)).toBeVisible()
  const dashboardUrl = page.url()
  // The sidebar lights Dashboards alone.
  await expect(page.getByRole('link', { name: 'Dashboards' })).toHaveClass(/bg-secondary/)
  await expect(page.getByRole('link', { name: 'Logs' })).not.toHaveClass(/bg-secondary/)
  await expect(page.getByRole('link', { name: 'Projects' })).not.toHaveClass(/bg-secondary/)

  // Add to dashboard, beside the explorer's chart switch, saves the Query as drawn.
  await openLogs(page, last24h('query'))
  await page.getByLabel('Query', { exact: true }).fill(`${SOURCE}\n| summarize count() by bin_auto(time), level`)
  await runBy(page, () => page.getByLabel('Query', { exact: true }).press('Control+Enter'))
  await page.getByRole('group', { name: 'Chart' }).getByRole('button', { name: 'Bars' }).click()
  await page.getByRole('button', { name: 'Add to dashboard' }).click()
  const add = page.getByRole('dialog')
  await choose(page, add.getByRole('combobox', { name: 'Dashboard' }), name)
  await add.getByLabel('Chart title').fill('Logs by level')
  await add.getByRole('button', { name: 'Add chart' }).click()
  await expect(add.getByText(`"Logs by level" is on ${name}.`)).toBeVisible()
  await add.getByRole('link', { name: `Open ${name}` }).click()

  await expect(page.getByRole('heading', { name })).toBeVisible()
  await expect(page).not.toHaveURL(/range=/)
  await pickQuickRange(page, '1 day')
  await chartsRan(page, 1)
  await expect(card(page, 'Logs by level').getByRole('figure')).toHaveAttribute('aria-label', /^Bar chart of .* over time$/)

  // Add a chart in the editor: a Query whose single number shows as a stat.
  await page.getByRole('button', { name: 'Add chart' }).click()
  const editor = page.getByRole('dialog')
  await expect(editor.locator('[data-chart-preview]').getByRole('figure')).toBeVisible()
  await editor.getByRole('group', { name: 'Edit the Query in' }).getByRole('button', { name: 'Query' }).click()
  await editor.getByLabel('Query', { exact: true }).fill(`${SOURCE}\n| summarize count()`)
  await expect(editor.locator('[data-chart-preview] [data-chart-stat]')).toBeVisible()
  await editor.getByLabel('Title').fill('Total Logs')
  await editor.getByRole('group', { name: 'Viz' }).getByRole('button', { name: 'Stat' }).click()
  await editor.getByLabel('Unit').fill('Logs')
  await editor.getByRole('button', { name: 'Add chart' }).click()
  await expect(editor).toHaveCount(0)
  await chartsRan(page, 2)
  await expect(card(page, 'Total Logs').locator('[data-chart-stat]')).toContainText(/\d Logs$/)

  // A new range runs every chart again over it, and the URL keeps it.
  const reruns: URL[] = []
  page.on('request', request => {
    if (/\/charts\/\d+\/data\?/.test(request.url())) reruns.push(new URL(request.url()))
  })
  await pickQuickRange(page, '2 days')
  await expect(page).toHaveURL(/range=2d/)
  await chartsRan(page, 2)
  await expect.poll(() => reruns.length).toBe(2)
  for (const url of reruns) {
    const span = Date.parse(url.searchParams.get('to')!) - Date.parse(url.searchParams.get('from')!)
    expect(span).toBe(48 * 3600_000)
    expect(url.searchParams.has('query')).toBe(false)
  }

  // Edit layout: resize the first chart from its corner; the size is saved and kept after a reload.
  const first = page.locator('.react-grid-item').filter({ has: card(page, 'Logs by level') })
  const before = await size(first)
  await page.getByRole('button', { name: 'Edit layout' }).click()
  const handle = first.locator('.react-resizable-handle')
  const corner = await handle.boundingBox()
  if (!corner) throw new Error('no resize handle')
  const saved = page.waitForResponse(r => /\/layout$/.test(r.url()) && r.request().method() === 'PATCH')
  await page.mouse.move(corner.x + corner.width / 2, corner.y + corner.height / 2)
  await page.mouse.down()
  await page.mouse.move(corner.x + 60, corner.y + 160, { steps: 10 })
  await page.mouse.up()
  expect((await saved).status()).toBe(200)
  await page.getByRole('button', { name: 'Done' }).click()
  const resized = await size(first)
  expect(resized.height).toBeGreaterThan(before.height + 100)
  await page.reload()
  await chartsRan(page, 2)
  await expect(page).toHaveURL(/range=2d/)
  expect(await size(page.locator('.react-grid-item').filter({ has: card(page, 'Logs by level') }))).toEqual(resized)

  // Duplicate a chart; open it in the explorer over its Project, Query and the range shown.
  await card(page, 'Logs by level').getByRole('button', { name: 'Menu for Logs by level' }).click()
  await page.getByRole('menuitem', { name: 'Duplicate' }).click()
  await chartsRan(page, 3)
  await expect(card(page, 'Logs by level (copy)').getByRole('figure')).toHaveAttribute('aria-label', /^Bar chart of /)
  await card(page, 'Logs by level').getByRole('button', { name: 'Menu for Logs by level' }).click()
  await page.getByRole('menuitem', { name: 'Open in explorer' }).click()
  await expect(page).toHaveURL(/\/projects\/solid-trace-project\/logs\?range=2d&mode=builder&q=.*level.*&viz=bars$/)
  await expect(page.getByRole('group', { name: 'Chart' }).getByRole('button', { name: 'Bars' })).toHaveAttribute('aria-pressed', 'true')
  await page.goBack()
  await chartsRan(page, 3)

  // Delete a chart, confirming it, then the Dashboard.
  await card(page, 'Total Logs').getByRole('button', { name: 'Menu for Total Logs' }).click()
  await page.getByRole('menuitem', { name: 'Delete' }).click()
  await page.getByRole('alertdialog').getByRole('button', { name: 'Delete chart' }).click()
  await expect(card(page, 'Total Logs')).toHaveCount(0)
  await expect(cards(page)).toHaveCount(2)

  await page.getByRole('button', { name: 'Dashboard settings' }).click()
  await page.getByRole('dialog').getByRole('button', { name: 'Delete dashboard' }).click()
  await page.getByRole('alertdialog').getByRole('button', { name: 'Delete dashboard' }).click()
  await expect(page).toHaveURL(new RegExp(`/${ORG}/dashboards$`))
  await expect(page.getByRole('link', { name })).toHaveCount(0)
  await page.goto(dashboardUrl)
  await expect(page.getByRole('heading', { name })).toHaveCount(0)
  expect(errors.filter(e => !/status of 404/.test(e))).toEqual([])
})

test('a chart whose Query fails shows the error in its card, and the rest of the Dashboard works', async ({ page }) => {
  const errors = watchErrors(page, [failedLoad(422)])
  const name = `Flow broken chart ${Date.now()}`
  await page.goto(`/${ORG}/dashboards`)
  await page.getByRole('button', { name: 'New dashboard' }).first().click()
  await page.getByLabel('Name').fill(name)
  await page.getByRole('button', { name: 'Create dashboard' }).click()
  await expect(page.getByRole('heading', { name })).toBeVisible()

  // A Query that answers Logs has no table to chart: the preview and the card say what to do.
  await page.getByRole('button', { name: 'Add chart' }).click()
  const editor = page.getByRole('dialog')
  await editor.getByRole('group', { name: 'Edit the Query in' }).getByRole('button', { name: 'Query' }).click()
  await editor.getByLabel('Query', { exact: true }).fill(`${SOURCE}\n| where level == "error"`)
  await expect(editor.locator('[data-chart-preview]').getByRole('alert')).toContainText('End it with summarize')
  await editor.getByLabel('Title').fill('Errors')
  await editor.getByRole('button', { name: 'Add chart' }).click()
  await expect(editor).toHaveCount(0)
  await chartsRan(page, 1)
  await expect(card(page, 'Errors').getByRole('alert')).toContainText('This Query answers Logs, not a table')
  await expect(page.getByRole('button', { name: 'Refresh' })).toBeEnabled()

  // Editing the chart fixes it: its Query now counts the error Logs.
  await card(page, 'Errors').getByRole('button', { name: 'Menu for Errors' }).click()
  await page.getByRole('menuitem', { name: 'Edit' }).click()
  await expect(editor.getByLabel('Title')).toHaveValue('Errors')
  await editor.getByRole('group', { name: 'Edit the Query in' }).getByRole('button', { name: 'Query' }).click()
  await editor.getByLabel('Query', { exact: true }).fill(`${SOURCE}\n| where level == "error"\n| summarize count()`)
  await editor.getByRole('group', { name: 'Viz' }).getByRole('button', { name: 'Stat' }).click()
  await editor.getByRole('button', { name: 'Save chart' }).click()
  await expect(editor).toHaveCount(0)
  await chartsRan(page, 1)
  await expect(card(page, 'Errors').locator('[data-chart-stat]')).toBeVisible()
  await expect(card(page, 'Errors').getByRole('alert')).toHaveCount(0)

  // The palette jumps to the Dashboard from any page.
  await page.goto(`/${ORG}/issues`)
  await page.keyboard.press('Control+k')
  await page.getByLabel('Search the palette').fill(name)
  await page.getByRole('option', { name }).click()
  await expect(page.getByRole('heading', { name })).toBeVisible()

  await page.getByRole('button', { name: 'Dashboard settings' }).click()
  await page.getByRole('dialog').getByRole('button', { name: 'Delete dashboard' }).click()
  await page.getByRole('alertdialog').getByRole('button', { name: 'Delete dashboard' }).click()
  await expect(page).toHaveURL(new RegExp(`/${ORG}/dashboards$`))
  expect(errors).toEqual([])
})
