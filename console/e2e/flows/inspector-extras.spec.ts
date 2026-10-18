import { test, expect, type Page } from '@playwright/test'
import { builderRow, openLogs, settled, streamRows } from '../support/logs'
import { watchErrors } from '../support/screens'

// The inspector's ways out to other Logs: Logs around this one, which reads
// the Log's Service a minute either side with the Log highlighted, and Chart
// this field on a number, which charts its average over time.

const inspector = (page: Page) => page.getByRole('complementary', { name: 'Log inspector' })
const heading = (page: Page) => inspector(page).getByRole('heading', { level: 2 })
const selectedRow = (page: Page) => streamRows(page).locator('> div.bg-primary\\/10')
const line = (page: Page, name: string) => inspector(page).locator('div.group', { has: page.getByText(name, { exact: true }) }).first()
/** Where to click a row to open it: near its start, as a cell's right end holds its button. */
const AT_START = { position: { x: 60, y: 15 } }

test('Logs around this one shows its Service a minute either side, with it highlighted', async ({ page }) => {
  const errors = watchErrors(page)
  await openLogs(page)

  const row = streamRows(page).nth(5)
  const message = (await row.locator('> div > div').last().innerText()).trim()
  await row.click(AT_START)
  const opened = await heading(page).innerText()
  const service = (await line(page, 'service').locator('> div').nth(1).innerText()).trim()

  await inspector(page).getByRole('button', { name: 'Logs around this one' }).click()
  await settled(page)

  await expect(builderRow(page, 'Where').getByRole('button', { name: `service == "${service}"`, exact: true })).toBeVisible()
  const search = new URL(page.url()).searchParams
  expect(new Date(search.get('to')!).getTime() - new Date(search.get('from')!).getTime()).toBe(120_000)
  // The inspector stays on the Log, and the stream shades its row in view.
  await expect(heading(page)).toHaveText(opened)
  await expect(selectedRow(page)).toHaveCount(1)
  await expect(selectedRow(page)).toContainText(message)
  await expect(selectedRow(page)).toBeInViewport()
  const services = await streamRows(page).locator('> div > div:nth-child(3)').allInnerTexts()
  expect(new Set(services.map(s => s.trim()))).toEqual(new Set([service]))
  expect(errors).toEqual([])
})

/** A synthetic Service that logs every 10 ms: far more than a page a minute. */
const STEP_MS = 10
const idAt = (ms: number) => `00000000-0000-7000-8000-${ms.toString(16).padStart(12, '0')}`
const logAt = (ms: number) => ({ id: idAt(ms), time: new Date(ms).toISOString(), level: 'info', message: `tick ${ms}`, service: 'busy-api', attributes: {} })

/**
 * Answers the explorer's runs, and the inspector's Log, from the busy Service
 * rather than EventStore: newest first, a page of 200 at a time. `gone` ends
 * every stream at once, as when the Logs left the Retention. Returns the
 * query requests it answered.
 */
async function busyService(page: Page, state: { gone: boolean }) {
  const queries: URL[] = []
  await page.route(url => url.pathname.endsWith('/logs/query'), route => {
    const url = new URL(route.request().url())
    queries.push(url)
    if (state.gone) return route.fulfill({ json: { logs: [], next_cursor: null } })
    const from = Date.parse(url.searchParams.get('from')!)
    const to = Date.parse(url.searchParams.get('to')!)
    let ms = Number(url.searchParams.get('cursor') ?? Math.ceil(to / STEP_MS) * STEP_MS - STEP_MS)
    const logs = []
    for (; ms >= from && logs.length < 200; ms -= STEP_MS) logs.push(logAt(ms))
    return route.fulfill({ json: { logs, next_cursor: ms >= from ? String(ms) : null } })
  })
  await page.route(url => /\/logs\/00000000-0000-7000-8000-[0-9a-f]{12}$/.test(url.pathname), route =>
    route.fulfill({ json: logAt(parseInt(new URL(route.request().url()).pathname.slice(-12), 16)) }))
  return queries
}

test('Logs around this one on a busy Service ends the range sooner so the first page has the Log', async ({ page }) => {
  const errors = watchErrors(page)
  const state = { gone: false }
  const queries = await busyService(page, state)
  await openLogs(page)

  const row = streamRows(page).nth(5)
  const message = (await row.locator('> div > div').last().innerText()).trim()
  const at = Number(message.replace('tick ', ''))
  await row.click(AT_START)
  const before = queries.length
  await inspector(page).getByRole('button', { name: 'Logs around this one' }).click()
  await settled(page)

  // 6,000 Logs fall in the minute after it: a run or two, not a page per 200.
  await expect(selectedRow(page)).toContainText(message)
  await expect(selectedRow(page)).toBeInViewport()
  expect(queries.length - before).toBeLessThanOrEqual(3)
  const search = new URL(page.url()).searchParams
  expect(new Date(search.get('from')!).getTime()).toBe(at - 60_000)
  const to = new Date(search.get('to')!).getTime()
  expect(to).toBeGreaterThan(at)
  expect(to).toBeLessThan(at + 60_000)
  await expect(builderRow(page, 'Where').getByRole('button', { name: 'service == "busy-api"', exact: true })).toBeVisible()
  expect(errors).toEqual([])
})

test('Logs around this one says why when the stream ends without the Log', async ({ page }) => {
  const errors = watchErrors(page)
  const state = { gone: false }
  await busyService(page, state)
  await openLogs(page)

  await streamRows(page).nth(5).click(AT_START)
  const opened = await heading(page).innerText()
  state.gone = true
  await inspector(page).getByRole('button', { name: 'Logs around this one' }).click()
  await settled(page)

  await expect(page.getByText("This Log isn't in the stream any more: it may have left the Project's Retention.")).toBeVisible()
  await expect(heading(page)).toHaveText(opened)
  await expect(selectedRow(page)).toHaveCount(0)
  expect(errors).toEqual([])
})

test('Chart this field charts a number\'s average over time', async ({ page }) => {
  const errors = watchErrors(page)
  await openLogs(page, `?range=24h&q=${encodeURIComponent("['solid-trace-project']\n| where isnotnull(duration_ms)")}`)
  await expect(builderRow(page, 'Where')).toContainText('duration_ms')

  await streamRows(page).nth(0).click(AT_START)
  // Only numbers offer it.
  await line(page, 'service').hover()
  await expect(line(page, 'service').getByRole('button', { name: /^Chart this field/ })).toHaveCount(0)
  await line(page, 'duration_ms').hover()
  await line(page, 'duration_ms').getByRole('button', { name: 'Chart this field: avg(duration_ms) over time' }).click()
  await settled(page)

  await expect(inspector(page)).toHaveCount(0)
  await expect(builderRow(page, 'Summarize')).toContainText('avg(duration_ms)')
  await expect(page.getByRole('figure')).toHaveAttribute('aria-label', 'Line chart of avg_duration_ms over time')
  expect(errors).toEqual([])
})
