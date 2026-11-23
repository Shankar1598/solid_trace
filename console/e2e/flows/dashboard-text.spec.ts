import { expect, test, type Locator, type Page } from '@playwright/test'
import { ORG } from '../support/data'
import { watchErrors } from '../support/screens'

// Dashboard Text blocks: a member adds a Text block of Markdown with a link,
// sees it drawn with the link opening in a new tab and with no raw HTML or
// image, adds one with no title in a Section from its menu, changes and
// deletes them, and a Refresh runs nothing for them.

const block = (page: Page, title: string) => page.locator(`[data-text-block="${title}"]`)
const section = (page: Page, title: string) => page.getByRole('region', { name: title })

/** Writes a Text block in the open editor and adds it. */
async function addText(page: Page, text: string, title?: string) {
  const editor = page.getByRole('dialog')
  if (title) await editor.getByLabel('Title').fill(title)
  await editor.getByLabel('Markdown').fill(text)
  await editor.getByRole('button', { name: 'Add text' }).click()
  await expect(editor).toHaveCount(0)
}

/** Opens a Text block's menu and picks an item. */
async function menu(page: Page, card: Locator, name: string, item: string) {
  await card.getByRole('button', { name: `Menu for ${name}` }).click()
  await page.getByRole('menuitem', { name: item }).click()
}

test('a member adds Text blocks of Markdown, in no Section and in a Section, changes and deletes them', async ({ page }) => {
  const errors = watchErrors(page)
  const name = `Flow text ${Date.now()}`
  // A Text block runs nothing: no chart data is asked for on this page.
  const dataCalls: string[] = []
  page.on('request', request => { if (/\/charts\/\d+\/data/.test(request.url())) dataCalls.push(request.url()) })

  await page.goto(`/${ORG}/dashboards`)
  await page.getByRole('button', { name: 'New dashboard' }).first().click()
  await page.getByLabel('Name').fill(name)
  await page.getByRole('button', { name: 'Create dashboard' }).click()
  await expect(page.getByRole('heading', { name })).toBeVisible()

  // The editor's preview draws the Markdown as it is typed.
  await page.getByRole('button', { name: 'Add text' }).click()
  const editor = page.getByRole('dialog')
  await editor.getByLabel('Markdown').fill('## Runbook')
  await expect(editor.locator('[data-text-preview]').getByRole('heading', { name: 'Runbook' })).toBeVisible()
  await addText(page, [
    '## Runbook',
    '',
    '- **Restart** the `worker`',
    '- See [the wiki](https://wiki.example/run)',
    '',
    '<b class="raw">raw</b>',
    '',
    '![the graph](https://img.example/a.png)',
  ].join('\n'), 'Read me')

  const top = block(page, 'Read me')
  await expect(top.getByRole('heading', { name: 'Runbook' })).toBeVisible()
  await expect(top.locator('strong')).toHaveText('Restart')
  const link = top.getByRole('link', { name: 'the wiki' })
  await expect(link).toHaveAttribute('href', 'https://wiki.example/run')
  await expect(link).toHaveAttribute('target', '_blank')
  await expect(link).toHaveAttribute('rel', 'noopener noreferrer')
  await expect(top.locator('b.raw')).toHaveCount(0)
  await expect(top.locator('img')).toHaveCount(0)
  await expect(top.getByRole('link', { name: 'the graph' })).toHaveAttribute('href', 'https://img.example/a.png')

  // A Text block with no title, in a Section, from the Section's menu.
  await page.getByRole('button', { name: 'Add section' }).click()
  await page.getByRole('dialog').getByLabel('Title').fill('Notes')
  await page.getByRole('dialog').getByRole('button', { name: 'Add section' }).click()
  await expect(section(page, 'Notes')).toBeVisible()
  await page.getByRole('button', { name: 'Menu for section Notes' }).click()
  await page.getByRole('menuitem', { name: 'Add text here' }).click()
  await expect(page.getByRole('dialog').getByRole('combobox', { name: 'Section' })).toContainText('Notes')
  await addText(page, 'Call the on-call.')
  const inSection = section(page, 'Notes').locator('[data-text-block=""]')
  await expect(inSection).toContainText('Call the on-call.')

  // Change the first block's text; a reload keeps it.
  await menu(page, top, 'Read me', 'Edit')
  await expect(page.getByRole('dialog').getByLabel('Title')).toHaveValue('Read me')
  await page.getByRole('dialog').getByLabel('Markdown').fill('Restart the **worker** first.')
  await page.getByRole('dialog').getByRole('button', { name: 'Save text' }).click()
  await expect(page.getByRole('dialog')).toHaveCount(0)
  await page.reload()
  await expect(block(page, 'Read me')).toContainText('Restart the worker first.')

  // Refresh runs the charts again; a Text block has none to run.
  await page.getByRole('button', { name: 'Refresh' }).click()
  await expect(block(page, 'Read me')).toContainText('Restart the worker first.')

  // Delete the block in the Section, confirming.
  await menu(page, section(page, 'Notes').locator('[data-text-block=""]'), 'text', 'Delete')
  await page.getByRole('alertdialog').getByRole('button', { name: 'Delete text' }).click()
  await expect(section(page, 'Notes').locator('[data-text-block]')).toHaveCount(0)
  expect(dataCalls).toEqual([])

  await page.getByRole('button', { name: 'Dashboard settings' }).click()
  await page.getByRole('dialog').getByRole('button', { name: 'Delete dashboard' }).click()
  await page.getByRole('alertdialog').getByRole('button', { name: 'Delete dashboard' }).click()
  await expect(page).toHaveURL(new RegExp(`/${ORG}/dashboards$`))
  expect(errors).toEqual([])
})
