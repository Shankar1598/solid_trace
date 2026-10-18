import { test, expect, type Page } from '@playwright/test'
import { addFilter, builderRow, expectEditorText, last24h, logCount, openLogs, runBy, settled, streamRows, summaryLine } from '../support/logs'
import { watchErrors } from '../support/screens'

// Query mode: the Builder's Query as text to edit and run, by Run or
// Ctrl+Enter; a bad Query's mistake with its line and column; and the way
// back to the Builder, silent when the Builder can show the Query.

const SOURCE = "['solid-trace-project']"

async function typeQuery(page: Page, text: string) {
  const box = page.getByLabel('Query', { exact: true })
  await box.fill(text)
  return box
}

test('a Query runs by Run and by Ctrl+Enter, and a bad one shows its line and column', async ({ page }) => {
  const errors = watchErrors(page)
  await openLogs(page, last24h())
  const all = await logCount(page)

  await page.getByRole('button', { name: 'Query', exact: true }).click()
  const box = page.getByLabel('Query', { exact: true })
  await expectEditorText(box, SOURCE)
  await expect(page).toHaveURL(/mode=query/)

  // Run.
  await typeQuery(page, `${SOURCE}\n| where level == "error"`)
  await runBy(page, () => page.getByRole('button', { name: 'Run', exact: true }).click())
  const errorLogs = await logCount(page)
  expect(errorLogs).toBeGreaterThan(0)
  expect(errorLogs).toBeLessThan(all)
  await expect(page).toHaveURL(/q=.*error/)

  // Ctrl+Enter.
  await typeQuery(page, `${SOURCE}\n| where level == "warn"`)
  await runBy(page, () => box.press('Control+Enter'))
  await expect(page).toHaveURL(/q=.*warn/)
  const warnLogs = await logCount(page)
  expect(warnLogs).toBeGreaterThan(0)
  expect(warnLogs).not.toBe(errorLogs)

  // A mistake: its line and column under the box, and the last good result stays.
  await typeQuery(page, `${SOURCE}\n| where level ==`)
  await page.getByRole('button', { name: 'Run', exact: true }).click()
  await expect(page.getByText(/^Line 2, column \d+: /)).toBeVisible()
  await expect(page).toHaveURL(/q=.*warn/)
  expect(await logCount(page)).toBe(warnLogs)

  // Fixing it clears the mistake.
  await typeQuery(page, `${SOURCE}\n| where level == "error"`)
  await runBy(page, () => box.press('Control+Enter'))
  await expect(page.getByText(/^Line 2, column \d+: /)).toHaveCount(0)
  expect(await logCount(page)).toBe(errorLogs)
  expect(errors).toEqual([])
})

test('back to the Builder: silent when it can show the Query, asking when it cannot', async ({ page }) => {
  const errors = watchErrors(page)
  // A fixed range: Discard runs again, and must count the same Logs.
  await openLogs(page, last24h())
  await page.getByRole('button', { name: 'Query', exact: true }).click()

  // The Builder shows a where: no prompt.
  await typeQuery(page, `${SOURCE}\n| where level == "warn"`)
  await runBy(page, () => page.getByRole('button', { name: 'Run', exact: true }).click())
  const warnLogs = await logCount(page)
  await page.getByRole('button', { name: 'Builder', exact: true }).click()
  await settled(page)
  await expect(builderRow(page, 'Where').getByRole('button', { name: 'level == "warn"', exact: true })).toBeVisible()
  await expect(page).toHaveURL(/mode=builder/)
  expect(await logCount(page)).toBe(warnLogs)

  // It can't show a project stage: Stay keeps the Query, Discard returns to the Builder's filters.
  await page.getByRole('button', { name: 'Query', exact: true }).click()
  const unshowable = `${SOURCE}\n| where level == "warn"\n| project time, message`
  await typeQuery(page, unshowable)
  await runBy(page, () => page.getByRole('button', { name: 'Run', exact: true }).click())
  await page.getByRole('button', { name: 'Builder', exact: true }).click()
  await expect(page.getByText(/The Builder can't show/)).toBeVisible()
  await expect(page.getByText(/The Builder can't show/)).toContainText('project time, message')

  await page.getByRole('button', { name: 'Stay in Query' }).click()
  await expect(page.getByText(/The Builder can't show/)).toHaveCount(0)
  await expectEditorText(page.getByLabel('Query', { exact: true }), unshowable)
  await expect(page).toHaveURL(/mode=query/)

  await page.getByRole('button', { name: 'Builder', exact: true }).click()
  await page.getByRole('button', { name: 'Discard and switch' }).click()
  await settled(page)
  await expect(page).toHaveURL(/mode=builder/)
  await expect(builderRow(page, 'Where').getByRole('button', { name: 'level == "warn"', exact: true })).toBeVisible()
  expect(await logCount(page)).toBe(warnLogs)
  expect(errors).toEqual([])
})

test('Discard drops a Query that never ran: Run afterwards runs what the Builder shows', async ({ page }) => {
  const errors = watchErrors(page)
  const unshowable = `${SOURCE}\n| where level == "warn"\n| project time, message`
  const discardTyped = async () => {
    await page.getByRole('button', { name: 'Query', exact: true }).click()
    await typeQuery(page, unshowable)
    await page.getByRole('button', { name: 'Builder', exact: true }).click()
    await page.getByRole('button', { name: 'Discard and switch' }).click()
    await expect(page).toHaveURL(/mode=builder/)
  }
  const run = () => runBy(page, () => page.getByRole('button', { name: 'Run', exact: true }).click())
  const ranQuery = () => new URL(page.url()).searchParams.get('q') ?? ''

  // From an empty Builder.
  await openLogs(page, last24h())
  const all = await logCount(page)
  await discardTyped()
  await run()
  expect(ranQuery()).not.toContain('| project')
  expect(await logCount(page)).toBe(all)
  await expect(builderRow(page, 'Where').getByRole('button', { name: /==/ })).toHaveCount(0)

  // From a Builder with a filter that ran.
  await addFilter(page, 'level', '==', 'error')
  const errorLogs = await logCount(page)
  await discardTyped()
  await run()
  expect(ranQuery()).not.toContain('| project')
  await expect(builderRow(page, 'Where').getByRole('button', { name: 'level == "error"', exact: true })).toBeVisible()
  expect(await logCount(page)).toBe(errorLogs)
  expect(errors).toEqual([])
})

test('an untouched Query is no edit: running it or going back to the Builder adds no history entry', async ({ page }) => {
  const errors = watchErrors(page)
  const entries = () => page.evaluate(() => history.length)

  // A Query-mode link with no Query.
  await openLogs(page, last24h('query'))
  const opened = await entries()
  await runBy(page, () => page.getByRole('button', { name: 'Run', exact: true }).click())
  expect(await entries()).toBe(opened)

  // Back to the Builder replaces the entry, with no run.
  await page.getByRole('button', { name: 'Builder', exact: true }).click()
  await expect(page).toHaveURL(/mode=builder/)
  expect(await entries()).toBe(opened)

  // From the Builder into Query mode and back, the same.
  await page.getByRole('button', { name: 'Query', exact: true }).click()
  await expectEditorText(page.getByLabel('Query', { exact: true }), SOURCE)
  await page.getByRole('button', { name: 'Builder', exact: true }).click()
  await expect(page).toHaveURL(/mode=builder/)
  expect(await entries()).toBe(opened)
  expect(errors).toEqual([])
})

test('in Query mode a Level count and Hide add wheres after the Query, which starts with the Project', async ({ page }) => {
  const errors = watchErrors(page)
  // A Query-mode link with no Query: the box holds the Project's slug, as switching to Query mode writes it.
  await openLogs(page, last24h('query'))
  const box = page.getByLabel('Query', { exact: true })
  await expectEditorText(box, SOURCE)

  const warn = summaryLine(page).getByRole('button', { name: /^warn [\d,]+$/ })
  const warnCount = Number((await warn.innerText()).replace(/[^\d]/g, ''))
  await runBy(page, () => warn.click())
  await expectEditorText(box, `${SOURCE}\n| where level == "warn"`)
  expect(await logCount(page)).toBe(warnCount)

  // Hide a service from the inspector: a where after the last one.
  await streamRows(page).nth(0).click({ position: { x: 60, y: 15 } })
  const inspector = page.getByRole('complementary', { name: 'Log inspector' })
  const line = inspector.locator('div.group', { has: page.getByText('service', { exact: true }) }).first()
  const service = (await line.locator('> div').nth(1).innerText()).trim()
  await line.hover()
  await runBy(page, () => line.getByRole('button', { name: `Hide service == "${service}"` }).click())
  await expectEditorText(box, `${SOURCE}\n| where level == "warn"\n| where service != "${service}"`)
  expect(await logCount(page)).toBeLessThan(warnCount)
  expect(errors).toEqual([])
})
