import { test, expect, type Page } from '@playwright/test'
import { editorText, expectEditorText, holdSqlQueries, openLogs, releaseSqlQueries, runBy, summaryLine } from '../support/logs'
import { failedLoad, watchErrors } from '../support/screens'

// The QueryEditor in Query mode: completions for stages, Fields, operators
// and values, validate's mistakes as squiggles with their message on hover,
// Ctrl+Enter to run, and Esc to leave the editor. In SQL mode, SQL's own
// highlighting.

const SOURCE = "['solid-trace-project']"
const completions = (page: Page) => page.locator('.cm-tooltip-autocomplete')
const option = (page: Page, label: string) =>
  completions(page).getByRole('option', { name: new RegExp(`^${label.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')}(?![\\w])`) })

/**
 * Enter on the list once it has been open a moment: the editor ignores keys
 * in its first 75 ms, so a key meant for the text isn't taken as a pick.
 */
async function pick(page: Page) {
  await expect(completions(page)).toBeVisible()
  await page.waitForTimeout(100)
  await page.keyboard.press('Enter')
}

async function queryMode(page: Page) {
  await openLogs(page)
  await page.getByRole('button', { name: 'Query', exact: true }).click()
  const box = page.getByLabel('Query', { exact: true })
  await expectEditorText(box, SOURCE)
  return box
}

test('completions write a where: a stage after |, a Field, an operator and a value', async ({ page }) => {
  const errors = watchErrors(page)
  const box = await queryMode(page)
  // The box has focus, its cursor at the end.
  await expect(box).toBeFocused()

  await page.keyboard.press('Enter')
  await page.keyboard.type('| ')
  await expect(option(page, 'where')).toBeVisible()
  await expect(option(page, 'summarize')).toBeVisible()
  await page.keyboard.type('wh')
  await pick(page)
  await expectEditorText(box, `${SOURCE}\n| where `)

  // Fields follow at once, with their types.
  await expect(option(page, 'level')).toContainText('string')
  await page.keyboard.type('lev')
  await pick(page)
  await expectEditorText(box, `${SOURCE}\n| where level `)

  // Then the operators, then the values recent Logs have, which depend on
  // the newest Logs: the flow takes the last one listed.
  await expect(option(page, '==')).toBeVisible()
  await pick(page)
  const values = completions(page).getByRole('option')
  await expect(values.first()).toContainText(/^"\w+"/)
  const listed = await values.locator('.cm-completionLabel').allInnerTexts()
  const level = listed.at(-1)!
  await values.last().click()
  const where = `${SOURCE}\n| where level == ${level}`
  await expectEditorText(box, where)

  // Ctrl+Enter runs it; only that level shows.
  await runBy(page, () => box.press('Control+Enter'))
  expect(new URL(page.url()).searchParams.get('q')).toBe(where)
  const levelCount = (value: string) => new RegExp(`\\b${JSON.parse(value)}\\s*[\\d,]+`)
  await expect(summaryLine(page)).toContainText(levelCount(level))
  for (const other of listed.filter(v => v !== level)) await expect(summaryLine(page)).not.toContainText(levelCount(other))
  expect(errors).toEqual([])
})

test('summarize offers aggregates, then Fields inside them, then by and time bins', async ({ page }) => {
  const errors = watchErrors(page)
  const box = await queryMode(page)
  await page.keyboard.type('\n| summarize ')

  await expect(option(page, 'count')).toContainText('count()')
  await page.keyboard.type('av')
  await pick(page)
  await expectEditorText(box, `${SOURCE}\n| summarize avg()`)
  // Inside the parentheses: Fields.
  await expect(option(page, 'level')).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(completions(page)).toHaveCount(0)
  // Escape closed the list; the editor still has focus.
  await expect(box).toBeFocused()

  // After the aggregate: by, then the time bins first.
  await page.keyboard.type('level')
  await page.keyboard.press('End')
  await page.keyboard.type(' ')
  await expect(option(page, 'by')).toBeVisible()
  await pick(page)
  await expect(completions(page).getByRole('option').first()).toContainText('bin_auto(time)')
  await pick(page)
  await expectEditorText(box, `${SOURCE}\n| summarize avg(level) by bin_auto(time)`)
  expect(errors).toEqual([])
})

test('a mistake is squiggled where validate puts it, with its message on hover and under the box', async ({ page }) => {
  const errors = watchErrors(page)
  const box = await queryMode(page)
  await box.fill(`${SOURCE}\n| where nope(level) == 1`)
  await page.getByRole('button', { name: 'Run', exact: true }).click()

  const squiggle = page.locator('.cm-lintRange-error')
  await expect(squiggle).toHaveText('nope')
  const line = page.getByText(/^Line 2, column 9: unknown function "nope"$/)
  await expect(line).toBeVisible()
  await squiggle.hover()
  await expect(page.locator('.cm-tooltip-lint')).toContainText('unknown function "nope"')
  // The box says where its mistakes are listed.
  await expect(box).toHaveAttribute('aria-describedby', 'query-problems')

  // Fixed and run, both go.
  await box.fill(`${SOURCE}\n| where tolower(level) == "error"`)
  await runBy(page, () => box.press('Control+Enter'))
  await expect(squiggle).toHaveCount(0)
  await expect(line).toHaveCount(0)
  expect(errors).toEqual([])
})

test('on a Mac, both Ctrl+Enter, as the hint says, and Cmd+Enter run the Query', async ({ page }) => {
  // CodeMirror reads the platform once, when it loads.
  await page.addInitScript(() => Object.defineProperty(Navigator.prototype, 'platform', { get: () => 'MacIntel' }))
  const errors = watchErrors(page)
  const box = await queryMode(page)
  await expect(page.getByText('Ctrl+Enter runs the Query.')).toBeVisible()

  await page.keyboard.type('\n| take 5')
  await runBy(page, () => box.press('Control+Enter'))
  expect(new URL(page.url()).searchParams.get('q')).toBe(`${SOURCE}\n| take 5`)

  await page.keyboard.type('0')
  await runBy(page, () => box.press('Meta+Enter'))
  expect(new URL(page.url()).searchParams.get('q')).toBe(`${SOURCE}\n| take 50`)
  // Neither key wrote a line break.
  expect(await editorText(box)).toBe(`${SOURCE}\n| take 50`)
  expect(errors).toEqual([])
})

test('Escape leaves the editor, and typing never runs the Query', async ({ page }) => {
  const errors = watchErrors(page)
  const box = await queryMode(page)
  const before = page.url()
  await page.keyboard.type('\n| take 5')
  // Past the editor's wait before it asks for completions, which a first Escape would close.
  await page.waitForTimeout(200)
  await page.keyboard.press('Escape')
  await expect(box).not.toBeFocused()
  expect(page.url()).toBe(before)
  expect(await editorText(box)).toBe(`${SOURCE}\n| take 5`)
  expect(errors).toEqual([])
})

test.describe('SQL mode', () => {
  test.beforeAll(() => { holdSqlQueries() })
  test.afterAll(() => { releaseSqlQueries() })

  test('highlights SQL and runs it with Ctrl+Enter', async ({ page }) => {
    const errors = watchErrors(page, [failedLoad(400)])
    await openLogs(page)
    await page.getByRole('button', { name: 'SQL', exact: true }).click()
    const box = page.getByLabel('SQL', { exact: true })
    await expectEditorText(box, /FROM logs/i)
    await box.fill('SELECT service, count(*) AS total FROM logs GROUP BY service')
    // Keywords are coloured apart from the plain text of names.
    const keyword = box.getByText('SELECT', { exact: true })
    const line = box.locator('.cm-line').first()
    const color = (l: typeof keyword) => l.evaluate(el => getComputedStyle(el).color)
    // The SQL is parsed when the browser is idle, so the colour can come a moment later.
    await expect.poll(async () => (await color(keyword)) !== (await color(line))).toBe(true)

    await box.press('Control+Enter')
    await expect(page.locator('.sticky.top-0').filter({ hasText: /^service\s*total$/ })).toBeVisible()
    expect(errors).toEqual([])
  })
})
