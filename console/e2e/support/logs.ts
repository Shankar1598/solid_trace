import { expect, type Locator, type Page } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import { mkdirSync, readdirSync, rmSync, writeFileSync } from 'node:fs'
import { homedir } from 'node:os'
import { join, resolve } from 'node:path'
import { logsPath, PROJECT } from './data'

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

/**
 * The dummy Logs global setup sends: each has a region. Flows that count
 * Services or open the newest Log read these, so a Log sent to the dev
 * Project by hand (a probe, say) changes nothing they see.
 */
export const DUMMY_LOGS = `['${PROJECT}']\n| where isnotnull(region)`

/** The dummy Logs over the last 24 h as a fixed range (last24h), in a mode. */
export function dummyLogs(mode = 'builder'): string {
  return `${last24h(mode)}&${new URLSearchParams({ q: DUMMY_LOGS })}`
}

/** The time-range picker's button, named "Time range: <the range>". */
export function timeRange(page: Page): Locator {
  return page.getByRole('button', { name: /^Time range: / })
}

/** Picks one of the time-range picker's quick ranges, such as "6 hrs". */
export async function pickQuickRange(page: Page, label: string) {
  await timeRange(page).click()
  await page.getByRole('dialog').getByRole('button', { name: label, exact: true }).click()
}

/** Waits until the run and the histogram have answered: Run is back and nothing is counting. */
export async function settled(page: Page) {
  await expect(page.getByRole('button', { name: 'Run', exact: true })).toBeVisible()
  await expect(page.getByText('Counting Logs…')).toHaveCount(0)
  await expect(page.getByText(/^Running the (Query|SQL)…$/)).toHaveCount(0)
  await expect(page.getByText('Reading the Query…')).toHaveCount(0)
}

/**
 * Runs the explorer by `trigger` (Run, Ctrl+Enter, a Level count, Hide) and
 * waits for the run to answer. In Query mode a run starts only once validate
 * answers, with nothing on screen meanwhile, so settled() straight after the
 * trigger could pass before the run starts and leave the last run's count on
 * screen: this waits for the new run's histogram first.
 */
export async function runBy(page: Page, trigger: () => Promise<unknown>) {
  const counted = page.waitForResponse(response => /\/logs\/histogram\?/.test(response.url()))
  await trigger()
  await counted
  await settled(page)
}

/**
 * The text in a QueryEditor (CodeMirror), found by its name such as
 * getByLabel('Query', { exact: true }): its lines, without the placeholder.
 */
export function editorText(box: Locator): Promise<string> {
  return box.evaluate(el => [...el.querySelectorAll('.cm-line')]
    .map(line => (line.querySelector('.cm-placeholder') ? '' : line.textContent ?? ''))
    .join('\n'))
}

/** Waits until a QueryEditor holds `expected`, or text that matches it. */
export async function expectEditorText(box: Locator, expected: string | RegExp) {
  const text = expect.poll(() => editorText(box))
  if (typeof expected === 'string') await text.toBe(expected)
  else await text.toMatch(expected)
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

/** The summary line over the histogram: "9,172 Logs  error rate 7.6%  debug 1,492 …", or "0 Logs" alone. */
export function summaryLine(page: Page): Locator {
  return page.locator('[data-summary-line]')
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

// SQL queries are an instance setting, shared by every Playwright run
// against the dev stack. Each run that turns them on leaves a file named by
// its process id while it needs them, and the last one out turns them off,
// so one run's afterAll doesn't turn SQL off under another's test.
const SQL_HOLDERS = join(homedir(), '.cache/solid-trace/playwright-sql-holders')

/** Turns SQL queries on for the dev instance, until releaseSqlQueries(). */
export function holdSqlQueries() {
  mkdirSync(SQL_HOLDERS, { recursive: true })
  writeFileSync(join(SQL_HOLDERS, String(process.pid)), '')
  setSqlQueries(true)
}

/** Turns SQL queries off again, unless another live run still holds them. */
export function releaseSqlQueries() {
  rmSync(join(SQL_HOLDERS, String(process.pid)), { force: true })
  const holders = readdirSync(SQL_HOLDERS).filter(pid => {
    if (alive(Number(pid))) return true
    // A run that stopped without releasing them.
    rmSync(join(SQL_HOLDERS, pid), { force: true })
    return false
  })
  if (holders.length === 0) setSqlQueries(false)
}

function alive(pid: number): boolean {
  try {
    process.kill(pid, 0)
    return true
  } catch (error) {
    return (error as NodeJS.ErrnoException).code === 'EPERM'
  }
}

function setSqlQueries(on: boolean) {
  execFileSync('mise', ['exec', '--', 'bin/rails', 'instance_settings:set', `SQL_QUERIES_ENABLED=${on}`], {
    cwd: resolve(__dirname, '../..'), stdio: 'pipe', timeout: 120_000,
  })
}
