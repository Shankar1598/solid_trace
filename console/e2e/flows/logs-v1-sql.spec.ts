import { test, expect, type Locator } from '@playwright/test'
import { holdSqlQueries, last24h, logCount, openLogs, releaseSqlQueries, settled } from '../support/logs'
import { failedLoad, watchErrors } from '../support/screens'

// SQL mode, on an instance with SQL queries on: the Query's readable SQL,
// run only on Run, answering a table; leaving with edited SQL asks first.

test.beforeAll(() => { holdSqlQueries() })
test.afterAll(() => { releaseSqlQueries() })

test('SQL mode opens with the readable SQL, runs on Run, and asks before discarding edits', async ({ page }) => {
  // The SQL with a mistake answers 400.
  const errors = watchErrors(page, [failedLoad(400)])
  await openLogs(page, last24h())
  const all = await logCount(page)

  await page.getByRole('button', { name: 'SQL', exact: true }).click()
  const box = page.getByLabel('SQL', { exact: true })
  await expect(box).toHaveValue(/FROM logs/i)
  await expect(page).toHaveURL(/mode=sql/)
  // Nothing runs until Run.
  await expect(page.getByText('Press Run to run the SQL.')).toBeVisible()

  // While it runs, and once cancelled, the result area speaks of the SQL.
  await page.route(/\/logs\/sql$/, () => {})
  await page.getByRole('button', { name: 'Run', exact: true }).click()
  await expect(page.getByText('Running the SQL…')).toBeVisible()
  await page.getByRole('button', { name: 'Cancel' }).click()
  await expect(page.getByText('SQL cancelled. Run it again.')).toBeVisible()
  await page.unroute(/\/logs\/sql$/)

  await page.getByRole('button', { name: 'Run', exact: true }).click()
  await settled(page)
  await expect(page.getByRole('button', { name: 'Run', exact: true })).toBeVisible()
  const header = page.locator('.sticky.top-0').filter({ hasText: 'message' })
  await expect(header).toBeVisible()

  // Edited SQL, run by Ctrl+Enter: a count per Level adds up to the Logs.
  await box.fill('SELECT level, count(*) AS n FROM logs GROUP BY level ORDER BY n DESC')
  await box.press('Control+Enter')
  await settled(page)
  const table = page.locator('.sticky.top-0').filter({ hasText: /^level\s*n$/ })
  await expect(table).toBeVisible()
  const counts = await table.locator('xpath=following-sibling::div[1]/div/div[2]').allInnerTexts()
  expect(counts.length).toBeGreaterThan(1)
  expect(counts.reduce((sum, n) => sum + Number(n.replace(/,/g, '')), 0)).toBe(all)

  // A mistake in the SQL says what DuckDB said, its caret under the mistake.
  await box.fill('SELECT nope FROM logs')
  await page.getByRole('button', { name: 'Run', exact: true }).click()
  const error = page.getByRole('alert')
  await expect(error).toContainText('Referenced column "nope" not found')
  await expectCaretUnder(error, 'nope')

  // Leaving with edits asks: Stay keeps the SQL, Discard returns to the Builder.
  await page.getByRole('button', { name: 'Builder', exact: true }).click()
  await expect(page.getByText(/SQL can't be turned back into a Query/)).toBeVisible()
  await page.getByRole('button', { name: 'Stay in SQL' }).click()
  await expect(page.getByText(/SQL can't be turned back into a Query/)).toHaveCount(0)
  await expect(box).toHaveValue('SELECT nope FROM logs')

  await page.getByRole('button', { name: 'Builder', exact: true }).click()
  await page.getByRole('button', { name: 'Discard and switch' }).click()
  await settled(page)
  await expect(page).toHaveURL(/mode=builder/)
  expect(await logCount(page)).toBe(all)
  expect(errors).toEqual([])
})

test('leaving SQL mode without edits is silent', async ({ page }) => {
  const errors = watchErrors(page)
  await openLogs(page)
  await page.getByRole('button', { name: 'SQL', exact: true }).click()
  await expect(page.getByLabel('SQL', { exact: true })).toHaveValue(/FROM logs/i)
  await page.getByRole('button', { name: 'Query', exact: true }).click()
  await settled(page)
  await expect(page.getByText(/SQL can't be turned back into a Query/)).toHaveCount(0)
  await expect(page).toHaveURL(/mode=query/)
  // The empty Builder's Query shows as the Project's slug, as Builder to Query writes it.
  await expect(page.getByLabel('Query', { exact: true })).toHaveValue("['solid-trace-project']")
  expect(errors).toEqual([])
})

test('on a phone a SQL error keeps its caret under the mistake', async ({ page }) => {
  const errors = watchErrors(page, [failedLoad(400)])
  await page.setViewportSize({ width: 390, height: 844 })
  await openLogs(page, last24h())
  await page.getByRole('button', { name: 'SQL', exact: true }).click()
  // Longer than a phone is wide.
  await page.getByLabel('SQL', { exact: true }).fill('SELECT service, level, count(*) AS n FROM logs WHERE nope = 1 GROUP BY service, level')
  await page.getByRole('button', { name: 'Run', exact: true }).click()
  const error = page.getByRole('alert')
  await expect(error).toContainText('Referenced column "nope" not found')
  await expectCaretUnder(error, 'nope')
  expect(errors).toEqual([])
})

/** DuckDB's ^ is drawn on the line under its LINE, in the column where `word` starts. */
async function expectCaretUnder(error: Locator, word: string) {
  const at = await error.evaluate((el, word) => {
    const walker = document.createTreeWalker(el, NodeFilter.SHOW_TEXT)
    for (let node = walker.nextNode(); node; node = walker.nextNode()) {
      const text = node.textContent ?? ''
      const line = text.indexOf('LINE ')
      if (line < 0) continue
      const box = (from: number) => {
        const range = document.createRange()
        range.setStart(node, from)
        range.setEnd(node, from + 1)
        return range.getBoundingClientRect()
      }
      const mistake = box(text.indexOf(word, line))
      const caret = box(text.indexOf('^', line))
      return { mistake: { x: mistake.left, y: mistake.top }, caret: { x: caret.left, y: caret.top }, lineHeight: parseFloat(getComputedStyle(node.parentElement!).lineHeight) }
    }
    return null
  }, word)
  expect(at, 'a LINE in the error').not.toBeNull()
  expect(Math.abs(at!.caret.x - at!.mistake.x)).toBeLessThan(2)
  expect(at!.caret.y - at!.mistake.y).toBeCloseTo(at!.lineHeight, 0)
}
