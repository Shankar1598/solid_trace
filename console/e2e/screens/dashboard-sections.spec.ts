import { expect, test, type Locator, type Page } from '@playwright/test'
import { ORG } from '../support/data'
import { shoot, useTheme, watchErrors } from '../support/screens'

// Dashboard Sections, for reading by eye: a chart in no Section, an open
// Section of two charts, a folded Section, a Section's menu, and an empty
// Section.

test.beforeEach(async ({ page }, info) => { await useTheme(page, info) })

const SOURCE = "['solid-trace-project']"

async function choose(page: Page, select: Locator, option: string) {
  await select.click()
  await page.getByRole('option', { name: option, exact: true }).click()
}

async function addChart(page: Page, title: string, q: string, viz: 'Stat' | 'Line' | 'Bar', sectionName?: string) {
  if (sectionName) {
    await page.getByRole('button', { name: `Menu for section ${sectionName}` }).click()
    await page.getByRole('menuitem', { name: 'Add chart here' }).click()
  } else {
    await page.getByRole('button', { name: 'Add chart' }).first().click()
  }
  const editor = page.getByRole('dialog')
  await choose(page, editor.getByRole('combobox', { name: 'Project' }).first(), 'SolidTrace Project')
  await editor.getByRole('group', { name: 'Edit the Query in' }).getByRole('button', { name: 'Query' }).click()
  await editor.getByLabel('Query', { exact: true }).fill(`${SOURCE}\n| ${q}`)
  await editor.getByLabel('Title').fill(title)
  await editor.getByRole('group', { name: 'Viz' }).getByRole('button', { name: viz }).click()
  await editor.getByRole('button', { name: 'Add chart' }).click()
  await expect(editor).toHaveCount(0)
}

async function addSection(page: Page, title: string) {
  await page.getByRole('button', { name: 'Add section' }).first().click()
  const dialog = page.getByRole('dialog')
  await dialog.getByLabel('Title').fill(title)
  await dialog.getByRole('button', { name: 'Add section' }).click()
  await expect(dialog).toHaveCount(0)
}

const settled = async (page: Page) =>
  expect(page.getByRole('status', { name: "Running the chart's Query" })).toHaveCount(0)

test('Dashboard Sections', async ({ page }) => {
  test.setTimeout(240_000)
  const errors = watchErrors(page)
  const name = `Sections review ${Date.now()}`
  await page.goto(`/${ORG}/dashboards`)
  await page.getByRole('button', { name: 'New dashboard' }).first().click()
  await page.getByLabel('Name').fill(name)
  await page.getByRole('button', { name: 'Create dashboard' }).click()
  await expect(page.getByRole('heading', { name })).toBeVisible()

  await addChart(page, 'All Logs', 'summarize count()', 'Stat')
  await addSection(page, 'Errors')
  await addChart(page, 'Errors over time', 'where level == "error" | summarize count() by bin_auto(time)', 'Line', 'Errors')
  await addChart(page, 'Errors by service', 'where level == "error" | summarize count() by service', 'Bar', 'Errors')
  await addSection(page, 'Latency')
  await addChart(page, 'Logs by level', 'summarize count() by level', 'Bar', 'Latency')
  await addSection(page, 'Empty section')
  await page.getByRole('button', { name: 'Fold Latency' }).click()
  await settled(page)
  await page.mouse.move(0, 0)
  await shoot(page, 'dashboard-sections', { fullPage: true })

  await page.getByRole('button', { name: 'Menu for section Errors' }).click()
  await shoot(page, 'dashboard-sections-menu')
  await page.keyboard.press('Escape')

  await page.getByRole('button', { name: 'Fold Errors' }).focus()
  await shoot(page, 'dashboard-sections-focus')

  await page.getByRole('button', { name: 'Dashboard settings' }).click()
  await page.getByRole('dialog').getByRole('button', { name: 'Delete dashboard' }).click()
  await page.getByRole('alertdialog').getByRole('button', { name: 'Delete dashboard' }).click()
  await expect(page).toHaveURL(new RegExp(`/${ORG}/dashboards$`))
  expect(errors).toEqual([])
})
