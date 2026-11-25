import { expect, test, type Page } from '@playwright/test'
import { ORG } from '../support/data'
import { watchErrors } from '../support/screens'

// The sidebar's Coding agents group: an accordion that holds Agent sessions
// and Review records. It is open on their pages. Elsewhere it stays as the
// viewer last left it, also after a reload.

const isDark = (page: Page) => page.evaluate(() => document.documentElement.classList.contains('dark'))

async function shot(page: Page, name: string) {
  await page.screenshot({ path: test.info().outputPath(`${name}.png`), animations: 'disabled' })
}

test('the Coding agents group opens, closes, keeps the choice, and lights only the child of the page', async ({ page }) => {
  const errors = watchErrors(page)
  const nav = page.getByRole('navigation', { name: 'Main' })
  const heading = nav.getByRole('button', { name: 'Coding agents' })
  const sessions = nav.getByRole('link', { name: 'Agent sessions' })
  const records = nav.getByRole('link', { name: 'Review records' })
  const lit = nav.locator('[aria-current="page"]')

  // From Issues, before the viewer opens it: closed, its children hidden.
  await page.goto(`/${ORG}/issues`)
  await page.evaluate(() => localStorage.removeItem('solidtrace.sidebarGroup.coding_agents'))
  await page.reload()
  await expect(heading).toHaveAttribute('aria-expanded', 'false')
  await expect(sessions).toBeHidden()
  await expect(records).toBeHidden()
  await expect(nav.getByRole('link', { name: 'Runs' })).toHaveCount(0)
  if (await isDark(page)) await page.getByRole('button', { name: 'Toggle theme' }).click()
  await expect.poll(() => isDark(page)).toBe(false)
  await shot(page, 'closed-on-issues-light')

  // Open it, then go to Review records: still open, and Review records alone lit.
  await heading.click()
  await expect(heading).toHaveAttribute('aria-expanded', 'true')
  await records.click()
  await expect(page).toHaveURL(new RegExp(`/${ORG}/review_records$`))
  await expect(page).toHaveTitle('Review records · SolidTrace')
  await expect(heading).toHaveAttribute('aria-expanded', 'true')
  await expect(lit).toHaveCount(1)
  await expect(records).toHaveAttribute('aria-current', 'page')
  await expect(sessions).not.toHaveAttribute('aria-current')
  await expect(page.getByRole('navigation', { name: 'Agent sessions tabs' })).toHaveCount(0)
  await shot(page, 'open-on-review-records-light')

  // A reload keeps it open.
  await page.reload()
  await expect(heading).toHaveAttribute('aria-expanded', 'true')
  await expect(records).toHaveAttribute('aria-current', 'page')

  // Dark mode, then Agent sessions from the group: it alone is lit.
  await page.getByRole('button', { name: 'Toggle theme' }).click()
  await expect.poll(() => isDark(page)).toBe(true)
  await shot(page, 'open-on-review-records-dark')
  await sessions.click()
  await expect(page).toHaveURL(new RegExp(`/${ORG}/agent_sessions$`))
  await expect(lit).toHaveCount(1)
  await expect(sessions).toHaveAttribute('aria-current', 'page')
  await shot(page, 'open-on-agent-sessions-dark')

  // On Issues the group stays as the viewer left it: open. Closed, it stays
  // closed after a reload.
  await nav.getByRole('link', { name: 'Issues' }).click()
  await expect(page).toHaveURL(new RegExp(`/${ORG}/issues`))
  await expect(heading).toHaveAttribute('aria-expanded', 'true')
  await heading.click()
  await expect(heading).toHaveAttribute('aria-expanded', 'false')
  await expect(sessions).toBeHidden()
  await page.reload()
  await expect(heading).toHaveAttribute('aria-expanded', 'false')
  await shot(page, 'closed-on-issues-dark')

  // A child's page opens it, also after the viewer closed it.
  await page.goto(`/${ORG}/review_records`)
  await expect(heading).toHaveAttribute('aria-expanded', 'true')
  await expect(records).toHaveAttribute('aria-current', 'page')

  expect(errors).toEqual([])

  // With no localStorage the group still works: closed elsewhere, open on
  // its children's pages. The layout's theme script throws then, so page
  // errors are not checked past this point.
  await page.addInitScript(() => {
    Object.defineProperty(window, 'localStorage', { get() { throw new Error('blocked') } })
  })
  await page.goto(`/${ORG}/issues`)
  await expect(heading).toHaveAttribute('aria-expanded', 'false')
  await heading.click()
  await expect(heading).toHaveAttribute('aria-expanded', 'true')
  await records.click()
  await expect(records).toHaveAttribute('aria-current', 'page')
})
