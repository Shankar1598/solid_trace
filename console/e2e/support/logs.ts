import { expect, type Locator, type Page } from '@playwright/test'
import { logsPath } from './data'

// The Logs explorer as a user sees it: its bar, its Builder and the summary
// line over the stream, for the flows and screens that click through it.

/** Opens the explorer with the URL's search (default: the last 24 h in the Builder) and waits for the run. */
export async function openLogs(page: Page, search = '?range=24h') {
  await page.goto(logsPath(search))
  await settled(page)
}

/**
 * The last 24 h as a fixed range, so runs a few seconds apart count the same
 * Logs: a relative range moves with the clock, and the dummy Logs are spread
 * evenly enough that a couple fall out of its start every few seconds.
 */
export function last24h(mode = 'builder'): string {
  const to = new Date(Math.floor(Date.now() / 60_000) * 60_000)
  const from = new Date(to.getTime() - 24 * 3600_000)
  return `?${new URLSearchParams({ from: from.toISOString(), to: to.toISOString(), mode })}`
}

/** Waits until the run and the histogram have answered: Run is back and nothing is counting. */
export async function settled(page: Page) {
  await expect(page.getByRole('button', { name: 'Run', exact: true })).toBeVisible()
  await expect(page.getByText('Counting Logs…')).toHaveCount(0)
  await expect(page.getByText(/^Running the (Query|SQL)…$/)).toHaveCount(0)
  await expect(page.getByText('Reading the Query…')).toHaveCount(0)
}

/** The summary line's Log count, as a number. */
export async function logCount(page: Page): Promise<number> {
  const line = summaryLine(page)
  await expect(line).toBeVisible()
  const text = (await line.innerText()).replace(/\s+/g, ' ')
  const match = /([\d,]+) Logs?\b/.exec(text)
  if (!match) throw new Error(`no Log count in the summary line: ${text}`)
  return Number(match[1].replace(/,/g, ''))
}

/** The summary line over the histogram: "9,172 Logs  error rate 7.6%  debug 1,492 …". */
export function summaryLine(page: Page): Locator {
  return page.getByText(/error rate/).locator('..')
}

/** The stream's rows, the Logs of the run. */
export function streamRows(page: Page): Locator {
  return page.locator('[data-stream-header] + div > div')
}

/** The row of a Builder clause, by its label: Project, Where, Summarize, Search, Sort by, Limit or More. */
export function builderRow(page: Page, label: string): Locator {
  return page.locator('div.relative.flex', { has: page.getByText(label, { exact: true }) }).first()
}

/** Adds a Where filter through the + steps: the Field, then the operator and value. */
export async function addFilter(page: Page, field: string, op: string, value: string) {
  await page.getByRole('button', { name: 'Add a filter' }).click()
  await page.getByLabel('Filter on a Field…').fill(field)
  await page.getByRole('option', { name: new RegExp(`^${escape(field)}\\b`) }).first().click()
  await page.getByLabel('Operator').selectOption(op)
  await page.getByLabel('Value', { exact: true }).fill(value)
  await page.getByRole('button', { name: 'Apply' }).click()
  await settled(page)
}

function escape(text: string) {
  return text.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}
