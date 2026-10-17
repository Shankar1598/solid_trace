import { expect, test, type Page } from '@playwright/test'
import { ORG } from '../support/data'
import { expectEditorText, last24h, openLogs, runBy, settled } from '../support/logs'
import { watchErrors } from '../support/screens'

// Copy link in the explorer bar, and the command palette (Cmd/Ctrl+K): a
// Project's Logs, the pages, recent Queries and actions.

test.use({ permissions: ['clipboard-read', 'clipboard-write'] })

const clipboard = (page: Page) => page.evaluate(() => navigator.clipboard.readText())

test('Copy link as shown gives the run\'s absolute range; live keeps it relative', async ({ page }) => {
  const errors = watchErrors(page)
  await openLogs(page, '?range=6h&mode=builder')

  await page.getByRole('button', { name: 'Copy link' }).click()
  await page.getByRole('menuitem', { name: /^As shown/ }).click()
  await expect(page.getByText('Link copied')).toBeVisible()
  const shown = new URL(await clipboard(page))
  expect(shown.pathname).toBe(new URL(page.url()).pathname)
  expect(shown.searchParams.get('range')).toBeNull()
  const from = new Date(shown.searchParams.get('from')!)
  const to = new Date(shown.searchParams.get('to')!)
  expect(to.getTime() - from.getTime()).toBe(6 * 3600_000)
  expect(Math.abs(Date.now() - to.getTime())).toBeLessThan(5 * 60_000)

  await page.getByRole('button', { name: 'Copy link' }).click()
  await page.getByRole('menuitem', { name: /^Live/ }).click()
  await expect.poll(() => clipboard(page)).toContain('range=6h')
  expect(errors).toEqual([])
})

const palette = (page: Page) => page.getByRole('dialog', { name: 'Command palette' })
const search = (page: Page) => page.getByLabel('Search the palette')

test('Cmd+K jumps to a recent Query from another page', async ({ page }) => {
  const errors = watchErrors(page)
  // A Query run in Query mode is recorded as recent.
  const query = `['solid-trace-project']\n| where level == "warn"\n| take ${100 + Math.floor(Math.random() * 900)}`
  await openLogs(page, last24h('query'))
  const box = page.getByLabel('Query', { exact: true })
  await box.fill(query)
  await runBy(page, () => box.press('Control+Enter'))

  await page.goto(`/${ORG}/issues`)
  await page.keyboard.press('Meta+k')
  await expect(palette(page)).toBeVisible()
  await expect(palette(page).getByRole('option').first()).toBeVisible()
  await search(page).fill(query.split('\n').pop()!)
  await expect(palette(page).getByRole('option')).toHaveCount(1)
  await search(page).press('Enter')

  await expect(page).toHaveURL(/\/projects\/solid-trace-project\/logs\?/)
  await settled(page)
  await expectEditorText(page.getByLabel('Query', { exact: true }), query)
  expect(errors).toEqual([])
})

test('the palette lists Projects\' Logs, pages and actions, and the sidebar opens it', async ({ page }) => {
  const errors = watchErrors(page)
  await page.goto(`/${ORG}/issues`)

  await page.getByRole('button', { name: /^Search…/ }).click()
  await expect(palette(page)).toBeVisible()
  for (const name of ['Issues', 'Projects', 'Settings', 'Toggle theme', 'Copy link', 'Show shortcuts']) {
    await expect(palette(page).getByRole('option', { name: new RegExp(`^${name}`) }).first()).toBeVisible()
  }

  // An action: the theme flips.
  const dark = () => page.evaluate(() => document.documentElement.classList.contains('dark'))
  const before = await dark()
  await search(page).fill('theme')
  await search(page).press('Enter')
  await expect(palette(page)).toBeHidden()
  await expect.poll(dark).toBe(!before)
  await page.keyboard.press('Control+k')
  await search(page).fill('theme')
  await search(page).press('Enter')
  await expect.poll(dark).toBe(before)

  // A Project's Logs.
  await page.keyboard.press('Control+k')
  await search(page).fill('logs solid')
  await palette(page).getByRole('option').first().click()
  await expect(page).toHaveURL(/\/projects\/solid-trace-project\/logs/)
  await settled(page)

  // Cmd+K again closes it, and the next opening starts with an empty search.
  await page.keyboard.press('Control+k')
  await expect(palette(page)).toBeVisible()
  await search(page).fill('theme')
  await page.keyboard.press('Control+k')
  await expect(palette(page)).toBeHidden()
  await page.keyboard.press('Control+k')
  await expect(search(page)).toHaveValue('')
  await expect(palette(page).getByRole('option', { name: /^Issues/ }).first()).toBeVisible()
  expect(errors).toEqual([])
})
