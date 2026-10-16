import { expect, test, type Page } from '@playwright/test'
import { ORG } from '../support/data'
import { editorText, last24h, openLogs, runBy, settled, streamRows } from '../support/logs'
import { watchErrors } from '../support/screens'

// Keyboard shortcuts: ? lists them all, / focuses the Query (in the Builder,
// opens the Where step), Cmd/Ctrl+Enter runs, Esc closes the top panel, and
// j and k step through Logs in the inspector. Typing in a box, single keys
// type rather than act.

const sheet = (page: Page) => page.getByRole('dialog', { name: 'Keyboard shortcuts' })
const inspector = (page: Page) => page.getByRole('complementary', { name: 'Log inspector' })

test('? shows a sheet of every shortcut, and Esc closes it', async ({ page }) => {
  const errors = watchErrors(page)
  await page.goto(`/${ORG}/issues`)
  await expect(page.getByRole('button', { name: /^Search…/ })).toBeVisible()

  await page.keyboard.press('?')
  await expect(sheet(page)).toBeVisible()
  for (const does of ['Open the command palette', 'Run the Query', 'Focus the Query', 'Close the top panel', 'Next Log in the inspector', 'Previous Log in the inspector', 'Show these shortcuts']) {
    await expect(sheet(page)).toContainText(does)
  }
  await page.keyboard.press('Escape')
  await expect(sheet(page)).toBeHidden()

  // From the palette too.
  await page.keyboard.press('Control+k')
  await page.getByLabel('Search the palette').fill('shortcuts')
  await page.getByLabel('Search the palette').press('Enter')
  await expect(sheet(page)).toBeVisible()
  expect(errors).toEqual([])
})

test('/ focuses the Query, and typing in it types rather than acts', async ({ page }) => {
  const errors = watchErrors(page)
  await openLogs(page, last24h('query'))
  const box = page.getByLabel('Query', { exact: true })
  await box.blur()
  await expect(box).not.toBeFocused()
  const before = await editorText(box)

  await page.keyboard.press('/')
  await expect(box).toBeFocused()
  // The / that focused it isn't typed; these are, ? and j included.
  await page.keyboard.type(' ?j')
  await expect.poll(() => editorText(box)).toBe(`${before} ?j`)
  await expect(sheet(page)).toBeHidden()
  expect(errors).toEqual([])
})

test('/ opens the Builder\'s Where step, and Cmd/Ctrl+Enter runs from the page', async ({ page }) => {
  const errors = watchErrors(page)
  await openLogs(page, last24h())

  await page.keyboard.press('/')
  await expect(page.getByLabel('Filter on a Field…')).toBeFocused()
  await page.keyboard.press('Escape')
  await expect(page.getByLabel('Filter on a Field…')).toBeHidden()

  await runBy(page, () => page.keyboard.press('Control+Enter'))
  await settled(page)
  expect(errors).toEqual([])
})

test('j and k step through Logs in the inspector, and Esc closes it', async ({ page }) => {
  const errors = watchErrors(page)
  await openLogs(page, last24h())

  await streamRows(page).nth(1).click({ position: { x: 60, y: 15 } })
  const heading = inspector(page).getByRole('heading', { level: 2 })
  const second = await heading.innerText()
  await page.keyboard.press('k')
  await expect(heading).not.toHaveText(second)
  await page.keyboard.press('j')
  await expect(heading).toHaveText(second)
  await page.keyboard.press('Escape')
  await expect(inspector(page)).toBeHidden()
  expect(errors).toEqual([])
})
