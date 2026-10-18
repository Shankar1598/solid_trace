import { test, expect, type Page } from '@playwright/test'
import { expectEditorText, holdSqlQueries, openLogs, releaseSqlQueries, runBy } from '../support/logs'
import { failedLoad, shoot, useTheme, watchErrors } from '../support/screens'

// The QueryEditor: a coloured Query, completions open for stages and for
// Fields, a mistake squiggled with its message, SQL coloured, and the box
// shown while CodeMirror loads.

test.beforeEach(async ({ page }, info) => { await useTheme(page, info) })

const SOURCE = "['solid-trace-project']"
const completions = (page: Page) => page.locator('.cm-tooltip-autocomplete')

async function queryMode(page: Page) {
  await openLogs(page)
  await page.getByRole('button', { name: 'Query', exact: true }).click()
  const box = page.getByLabel('Query', { exact: true })
  await expectEditorText(box, SOURCE)
  return box
}

test('a coloured Query', async ({ page }) => {
  const errors = watchErrors(page)
  const box = await queryMode(page)
  await box.fill(`${SOURCE}\n| where level in ("error", "fatal") and duration_ms > 250 // slow failures\n| summarize count(), avg(duration_ms) by service, bin(time, 1h)\n| sort by count_ desc`)
  await runBy(page, () => box.press('Control+Enter'))
  await shoot(page, 'editor-query')
  expect(errors).toEqual([])
})

test('completions: stages after a |', async ({ page }) => {
  const errors = watchErrors(page)
  await queryMode(page)
  await page.keyboard.type('\n| ')
  await expect(completions(page)).toBeVisible()
  await shoot(page, 'editor-completions-stages')
  expect(errors).toEqual([])
})

test('completions: Fields and functions in a where', async ({ page }) => {
  const errors = watchErrors(page)
  await queryMode(page)
  await page.keyboard.type('\n| where ')
  await expect(completions(page).getByRole('option').first()).toBeVisible()
  await shoot(page, 'editor-completions-fields')
  expect(errors).toEqual([])
})

test('a mistake squiggled, its message on hover', async ({ page }) => {
  const errors = watchErrors(page)
  const box = await queryMode(page)
  await box.fill(`${SOURCE}\n| where nope(level) == 1`)
  await page.getByRole('button', { name: 'Run', exact: true }).click()
  const squiggle = page.locator('.cm-lintRange-error')
  await expect(squiggle).toBeVisible()
  await squiggle.hover()
  await expect(page.locator('.cm-tooltip-lint')).toBeVisible()
  await shoot(page, 'editor-error')
  expect(errors).toEqual([])
})

test('loading: the text in a box of the same size', async ({ page }) => {
  const errors = watchErrors(page)
  await openLogs(page)
  // Hold CodeMirror's module so the box stays as it shows while loading.
  await page.route(/editor\/CodeEditor/, () => {})
  await page.getByRole('button', { name: 'Query', exact: true }).click()
  await expect(page.getByText(SOURCE, { exact: true })).toBeVisible()
  await shoot(page, 'editor-loading', { idle: false })
  expect(errors).toEqual([])
})

test.describe('SQL mode', () => {
  test.beforeAll(() => { holdSqlQueries() })
  test.afterAll(() => { releaseSqlQueries() })

  test('coloured SQL', async ({ page }) => {
    const errors = watchErrors(page, [failedLoad(400)])
    await openLogs(page)
    await page.getByRole('button', { name: 'SQL', exact: true }).click()
    const box = page.getByLabel('SQL', { exact: true })
    await expectEditorText(box, /FROM logs/i)
    await box.fill("SELECT service, count(*) AS total\nFROM logs\nWHERE level = 'error' -- failures only\nGROUP BY service\nORDER BY total DESC")
    await box.press('Control+Enter')
    await expect(page.locator('.sticky.top-0').filter({ hasText: /^service\s*total$/ })).toBeVisible()
    await shoot(page, 'editor-sql')
    expect(errors).toEqual([])
  })
})
