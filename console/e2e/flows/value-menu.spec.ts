import { expect, test, type Page } from '@playwright/test'
import { expectEditorText, last24h, openLogs, runBy, streamRows, summaryLine } from '../support/logs'
import { watchErrors } from '../support/screens'

// The value menu: one menu for every value in the stream, a table, a chart's
// legend and the inspector, with Show only, Hide, Group by, Chart this, Add
// column and Copy. Group by and Chart this make a Summarize that charts.

const SOURCE = "['solid-trace-project']"

/** The chart over the table, by its accessible name: "Line chart of count_ over time". */
const chart = (page: Page) => page.getByRole('figure')

/** The Query that ran, from the URL. */
const ranQuery = (page: Page) => new URL(page.url()).searchParams.get('q') ?? ''

/** Opens the value menu of the first stream row's cell in `column`, from the button its hover shows. */
async function openCellMenu(page: Page, column: string) {
  const button = streamRows(page).first().locator(`[aria-label="Menu for ${column}"]`)
  await button.locator('..').hover()
  await button.click()
}

async function pick(page: Page, item: string) {
  await runBy(page, () => page.getByRole('menuitem', { name: item, exact: true }).click())
}

test('Group by from a stream value counts Logs by its Field over time, as a chart', async ({ page }) => {
  const errors = watchErrors(page)
  await openLogs(page, last24h())
  // The run line: the Logs counted, and how long EventStore took.
  await expect(summaryLine(page)).toContainText(/[\d,]+ Logs? · [\d.]+ m?s/)

  await openCellMenu(page, 'service')
  for (const item of ['Show only this', 'Hide this', 'Group by', 'Chart this', 'Copy value', 'Remove column']) {
    await expect(page.getByRole('menuitem', { name: item })).toBeVisible()
  }
  await pick(page, 'Group by')

  await expect.poll(() => ranQuery(page)).toContain('summarize count() by bin_auto(time), service')
  await expect(chart(page)).toHaveAttribute('aria-label', /^Line chart of .+ over time$/)
  await expect(page.locator('[data-run-line]')).toHaveText(/^[\d,]+ rows? · [\d.]+ m?s$/)

  // A legend entry is a value of service: its menu filters on it.
  const entry = page.getByRole('button', { name: /^Menu for service = / }).first()
  const name = (await entry.getAttribute('aria-label'))!.replace('Menu for service = ', '')
  await entry.click()
  await pick(page, 'Show only this')
  await expect.poll(() => ranQuery(page)).toContain(`where service == "${name}"`)
  await expect(chart(page)).toBeVisible()
  expect(errors).toEqual([])
})

test('Chart this charts a number\'s average, or counts by any other value, over time', async ({ page }) => {
  const errors = watchErrors(page)
  const search = new URLSearchParams(last24h().slice(1))
  search.set('q', `${SOURCE}\n| where duration_ms > 0`)
  for (const col of ['time', 'duration_ms', 'service', 'message']) search.append('col', col)
  await openLogs(page, `?${search}`)

  await openCellMenu(page, 'duration_ms')
  await pick(page, 'Chart this')
  await expect.poll(() => ranQuery(page)).toBe(`${SOURCE}\n| where duration_ms > 0\n| summarize avg(duration_ms) by bin_auto(time)`)
  await expect(chart(page)).toHaveAttribute('aria-label', 'Line chart of avg_duration_ms over time')

  // A string: Logs counted by its Field's values, a series each.
  await openLogs(page, `?${search}`)
  await openCellMenu(page, 'service')
  await pick(page, 'Chart this')
  await expect.poll(() => ranQuery(page)).toBe(`${SOURCE}\n| where duration_ms > 0\n| summarize count() by service, bin_auto(time)`)
  await expect(chart(page)).toHaveAttribute('aria-label', /^Line chart of .+ over time$/)
  await expect(page.getByRole('button', { name: /^Menu for service = / }).first()).toBeVisible()
  expect(errors).toEqual([])
})

test('Group by in Query mode changes the Query the Builder would', async ({ page }) => {
  const errors = watchErrors(page)
  await openLogs(page, last24h('query'))

  await openCellMenu(page, 'service')
  await pick(page, 'Group by')
  await expectEditorText(page.getByLabel('Query', { exact: true }), `${SOURCE}\n| summarize count() by bin_auto(time), service`)
  await expect(chart(page)).toHaveAttribute('aria-label', /^Line chart of .+ over time$/)
  expect(errors).toEqual([])
})
