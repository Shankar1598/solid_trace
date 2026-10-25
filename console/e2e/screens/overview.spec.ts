import { expect, test } from '@playwright/test'
import { ORG } from '../support/data'
import { shoot, useTheme, watchErrors } from '../support/screens'

// The Overview, for reading by eye at both sizes in both themes: the first
// view, which should read as a dashboard, then the whole page.

test.beforeEach(async ({ page }, info) => { await useTheme(page, info) })

test.describe('Overview', () => {
  for (const range of ['today', '7d']) {
    test(`over ${range}`, async ({ page }) => {
      const errors = watchErrors(page)
      await page.goto(`/${ORG}/overview${range === 'today' ? '' : `?range=${range}`}`)
      await expect(page.getByRole('heading', { name: 'Overview', level: 1 })).toBeVisible()
      // Today refreshes itself, so the network is a poor sign of settling;
      // the panels are.
      await expect(page.locator('[data-section="band"]')).toBeVisible()
      await shoot(page, `overview-${range}`, { idle: false })
      // The page scrolls inside main, not the window, so a full-page shot
      // shows only the first view: scroll main to its end for the rest.
      const main = page.locator('main')
      const { height, view } = await main.evaluate(m => ({ height: m.scrollHeight, view: m.clientHeight }))
      console.log(`overview ${range}: ${height}px tall in a ${view}px view`)
      // On a wide screen only main scrolls: the window stays put, with the
      // sidebar. (On a phone the layout scrolls the window, as every page does.)
      if (test.info().project.name.includes('desktop')) {
        const window_ = await page.evaluate(() => ({ height: document.documentElement.scrollHeight, view: window.innerHeight }))
        expect(window_.height, 'the window scrolls').toBeLessThanOrEqual(window_.view)
      }
      await main.evaluate(m => m.scrollTo(0, m.scrollHeight))
      await shoot(page, `overview-${range}-end`, { idle: false })
      expect(errors).toEqual([])
    })
  }
})
