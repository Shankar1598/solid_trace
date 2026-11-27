import { expect, test, type Locator, type Page } from '@playwright/test'
import { ORG, PROJECT } from '../support/data'
import { watchErrors } from '../support/screens'

// Dashboard Sections: a member adds a Section, adds a chart in it from its
// menu, folds it to its header and finds it folded after a reload, unfolds
// it, moves a chart in no Section into it with the chart editor, renames it,
// and deletes it, which keeps its charts in no Section.

const SOURCE = `['${PROJECT}']`

const card = (page: Page, title: string) => page.locator(`[data-chart-card="${title}"]`)
const cards = (page: Page) => page.locator('[data-chart-card]')
const section = (page: Page, title: string) => page.getByRole('region', { name: title })

/** Picks an option of a Select, found by its accessible name. */
async function choose(page: Page, select: Locator, option: string) {
  await select.click()
  await page.getByRole('option', { name: option, exact: true }).click()
}

/** Waits for each of `count` charts shown to have run. */
async function chartsRan(page: Page, count: number) {
  await expect(page.getByRole('status', { name: "Running the chart's Query" })).toHaveCount(0)
  await expect(cards(page)).toHaveCount(count)
}

/** Adds a stat chart of the Logs count through the open chart editor. */
async function addStat(page: Page, title: string) {
  const editor = page.getByRole('dialog')
  // The editor starts on the first Project by name, which is another one once `bin/rails dev:data` has run.
  await choose(page, editor.getByRole('combobox', { name: 'Project' }).first(), 'SolidTrace Project')
  await editor.getByRole('group', { name: 'Edit the Query in' }).getByRole('button', { name: 'Query' }).click()
  await editor.getByLabel('Query', { exact: true }).fill(`${SOURCE}\n| summarize count()`)
  await expect(editor.locator('[data-chart-preview] [data-chart-stat]')).toBeVisible()
  await editor.getByLabel('Title').fill(title)
  await editor.getByRole('group', { name: 'Viz' }).getByRole('button', { name: 'Stat' }).click()
  await editor.getByRole('button', { name: 'Add chart' }).click()
  await expect(editor).toHaveCount(0)
}

test('a member groups charts in a Section, folds and unfolds it, and deletes it, keeping its charts', async ({ page }) => {
  const errors = watchErrors(page)
  const name = `Flow sections ${Date.now()}`

  await page.goto(`/${ORG}/dashboards`)
  await page.getByRole('button', { name: 'New dashboard' }).first().click()
  await page.getByLabel('Name').fill(name)
  await page.getByRole('button', { name: 'Create dashboard' }).click()
  await expect(page.getByRole('heading', { name })).toBeVisible()

  // A chart in no Section shows at the top.
  await page.getByRole('button', { name: 'Add chart' }).click()
  await addStat(page, 'Top total')
  await chartsRan(page, 1)

  // Add a Section; it shows its header and says it has no charts.
  await page.getByRole('button', { name: 'Add section' }).click()
  const dialog = page.getByRole('dialog')
  await dialog.getByLabel('Title').fill('Errors')
  await dialog.getByRole('button', { name: 'Add section' }).click()
  await expect(dialog).toHaveCount(0)
  await expect(section(page, 'Errors')).toBeVisible()
  await expect(section(page, 'Errors').getByText(/No charts in this section/)).toBeVisible()

  // Add chart here opens the editor with the Section picked; the chart shows in it.
  await page.getByRole('button', { name: 'Menu for section Errors' }).click()
  await page.getByRole('menuitem', { name: 'Add chart here' }).click()
  await expect(page.getByRole('dialog').getByRole('combobox', { name: 'Section' })).toContainText('Errors')
  await addStat(page, 'Section total')
  await chartsRan(page, 2)
  await expect(section(page, 'Errors').locator('[data-chart-card="Section total"]')).toBeVisible()
  await expect(section(page, 'Errors').locator('[data-chart-card="Top total"]')).toHaveCount(0)

  // Fold the Section: only its header shows, with the count of its charts.
  const toggle = page.getByRole('button', { name: 'Fold Errors' })
  await expect(toggle).toHaveAttribute('aria-expanded', 'true')
  await toggle.click()
  const unfold = page.getByRole('button', { name: 'Unfold Errors' })
  await expect(unfold).toHaveAttribute('aria-expanded', 'false')
  await expect(unfold).toContainText('1 chart')
  await expect(card(page, 'Section total')).toHaveCount(0)
  await expect(card(page, 'Top total')).toBeVisible()

  // The fold is kept in this browser, so a reload shows the Section folded.
  await page.reload()
  await chartsRan(page, 1)
  await expect(page.getByRole('button', { name: 'Unfold Errors' })).toBeVisible()

  // Unfold it: its chart shows and runs again.
  await page.getByRole('button', { name: 'Unfold Errors' }).click()
  await chartsRan(page, 2)
  await expect(section(page, 'Errors').locator('[data-chart-card="Section total"]')).toBeVisible()

  // The chart editor moves the chart in no Section into the Section.
  await card(page, 'Top total').getByRole('button', { name: 'Menu for Top total' }).click()
  await page.getByRole('menuitem', { name: 'Edit' }).click()
  const editor = page.getByRole('dialog')
  await expect(editor.getByLabel('Title')).toHaveValue('Top total')
  await choose(page, editor.getByRole('combobox', { name: 'Section' }), 'Errors')
  await editor.getByRole('button', { name: 'Save chart' }).click()
  await expect(editor).toHaveCount(0)
  await expect(section(page, 'Errors').locator('[data-chart-card]')).toHaveCount(2)

  // Rename the Section.
  await page.getByRole('button', { name: 'Menu for section Errors' }).click()
  await page.getByRole('menuitem', { name: 'Rename' }).click()
  await page.getByRole('dialog').getByLabel('Title').fill('Failures')
  await page.getByRole('dialog').getByRole('button', { name: 'Save' }).click()
  await expect(section(page, 'Failures')).toBeVisible()

  // Delete it, confirming; its charts stay, in no Section.
  await page.getByRole('button', { name: 'Menu for section Failures' }).click()
  await page.getByRole('menuitem', { name: 'Delete' }).click()
  const confirm = page.getByRole('alertdialog')
  await expect(confirm).toContainText('Its 2 charts stay on the Dashboard')
  await confirm.getByRole('button', { name: 'Delete section' }).click()
  await expect(section(page, 'Failures')).toHaveCount(0)
  await chartsRan(page, 2)

  await page.getByRole('button', { name: 'Dashboard settings' }).click()
  await page.getByRole('dialog').getByRole('button', { name: 'Delete dashboard' }).click()
  await page.getByRole('alertdialog').getByRole('button', { name: 'Delete dashboard' }).click()
  await expect(page).toHaveURL(new RegExp(`/${ORG}/dashboards$`))
  expect(errors).toEqual([])
})
