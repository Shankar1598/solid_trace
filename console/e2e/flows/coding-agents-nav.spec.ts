import { expect, test, type Page } from '@playwright/test'
import { ORG } from '../support/data'
import { watchErrors } from '../support/screens'

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

  await page.goto(`/${ORG}/issues`)
  await page.evaluate(() => localStorage.removeItem('solidtrace.sidebarGroup.coding_agents'))
  await page.reload()
  await expect(heading).toHaveAttribute('aria-expanded', 'false')
  await expect(heading).toHaveAttribute('data-lit', 'false')
  await expect(sessions).toBeHidden()
  await expect(records).toBeHidden()
  await expect(nav.getByRole('link', { name: 'Runs' })).toHaveCount(0)
  if (await isDark(page)) await page.getByRole('button', { name: 'Toggle theme' }).click()
  await expect.poll(() => isDark(page)).toBe(false)
  await shot(page, 'closed-on-issues-light')

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

  await page.reload()
  await expect(heading).toHaveAttribute('aria-expanded', 'true')
  await expect(records).toHaveAttribute('aria-current', 'page')

  await heading.click()
  await expect(heading).toHaveAttribute('aria-expanded', 'false')
  await expect(heading).toHaveAttribute('data-lit', 'true')
  await expect(sessions).toBeHidden()
  await expect(records).toBeHidden()
  await heading.click()
  await expect(heading).toHaveAttribute('aria-expanded', 'true')
  await expect(heading).toHaveAttribute('data-lit', 'false')
  await expect(records).toHaveAttribute('aria-current', 'page')

  await page.getByRole('button', { name: 'Toggle theme' }).click()
  await expect.poll(() => isDark(page)).toBe(true)
  await shot(page, 'open-on-review-records-dark')
  await sessions.click()
  await expect(page).toHaveURL(new RegExp(`/${ORG}/agent_sessions$`))
  await expect(lit).toHaveCount(1)
  await expect(sessions).toHaveAttribute('aria-current', 'page')
  await shot(page, 'open-on-agent-sessions-dark')

  await nav.getByRole('link', { name: 'Issues' }).click()
  await expect(page).toHaveURL(new RegExp(`/${ORG}/issues`))
  await expect(heading).toHaveAttribute('aria-expanded', 'true')
  await heading.click()
  await expect(heading).toHaveAttribute('aria-expanded', 'false')
  await expect(sessions).toBeHidden()
  await page.reload()
  await expect(heading).toHaveAttribute('aria-expanded', 'false')
  await shot(page, 'closed-on-issues-dark')

  await page.goto(`/${ORG}/review_records`)
  await expect(heading).toHaveAttribute('aria-expanded', 'true')
  await expect(records).toHaveAttribute('aria-current', 'page')

  expect(errors).toEqual([])

  await page.evaluate(() => localStorage.setItem('solidtrace.sidebarGroup.coding_agents', 'open'))
  await page.addInitScript(() => {
    Object.defineProperty(window, 'localStorage', { get() { throw new Error('blocked') } })
  })
  await page.goto(`/${ORG}/issues`)
  expect(await page.evaluate(() => {
    try {
      void window.localStorage
      return 'read'
    } catch {
      return 'blocked'
    }
  })).toBe('blocked')
  await expect(heading).toHaveAttribute('aria-expanded', 'false')
  await heading.click()
  await expect(heading).toHaveAttribute('aria-expanded', 'true')
  await records.click()
  await expect(records).toHaveAttribute('aria-current', 'page')
})
