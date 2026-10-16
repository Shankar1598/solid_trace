import { expect, test, type Page } from '@playwright/test'
import { openLogs } from '../support/logs'
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
