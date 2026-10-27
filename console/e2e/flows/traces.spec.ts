import { test, expect, type APIRequestContext, type Page } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import { randomBytes } from 'node:crypto'
import { resolve } from 'node:path'
import { ORG, PROJECT } from '../support/data'
import { openLogs, streamRows } from '../support/logs'
import { watchErrors } from '../support/screens'

// A Transaction sent as a Sentry SDK sends it shows as a trace: its id
// pasted into the command palette opens the waterfall of its Spans. An
// Issue's Event and a Log of the trace open it too, with its Issues and
// Logs, even when its SDK didn't sample it.

const ingest = process.env.E2E_INGEST_URL ?? 'http://localhost:4000'

const runner = (code: string) => execFileSync('mise', ['exec', '--', 'bin/rails', 'runner', code], {
  cwd: resolve(__dirname, '../..'), stdio: 'pipe', timeout: 120_000,
}).toString().trim()

// An envelope as sentry-ruby frames it: a request with a query under it,
// at a 25% sample rate.
function envelope(traceId: string, key: string): string {
  const start = Date.now() / 1000 - 5
  const span = (spanId: string, parent: string | null, op: string, description: string, from: number, to: number) => ({
    trace_id: traceId, span_id: spanId, parent_span_id: parent, op, description, status: 'ok',
    start_timestamp: start + from, timestamp: start + to, tags: {}, data: {},
  })
  const transaction = {
    event_id: randomBytes(16).toString('hex'), type: 'transaction', platform: 'ruby',
    transaction: 'OrdersController#show', transaction_info: { source: 'view' },
    start_timestamp: start, timestamp: start + 0.12, environment: 'production', release: 'flow@1.0.0',
    contexts: { trace: { trace_id: traceId, span_id: 'f000000000000001', op: 'http.server', status: 'ok' } },
    spans: [span('f000000000000002', 'f000000000000001', 'db.sql.active_record', 'SELECT * FROM orders WHERE id = 42', 0.01, 0.04)],
  }
  const header = { event_id: transaction.event_id, sent_at: new Date().toISOString(), trace: { trace_id: traceId, sample_rate: '0.25', public_key: key } }
  return [JSON.stringify(header), JSON.stringify({ type: 'transaction', content_type: 'application/json' }), JSON.stringify(transaction)].join('\n')
}

test('a Transaction sent to a Project opens as a trace from the palette', async ({ page, request }) => {
  const errors = watchErrors(page)
  const traceId = randomBytes(16).toString('hex')
  const key = runner(`puts Project.find_by!(slug: ${JSON.stringify(PROJECT)}).project_keys.first.public_key`)
  const sent = await request.post(`${ingest}/api/1/envelope`, {
    headers: { 'X-Sentry-Auth': `Sentry sentry_version=7, sentry_key=${key}`, 'Content-Type': 'application/x-sentry-envelope' },
    data: envelope(traceId, key),
  })
  expect(sent.ok(), await sent.text()).toBe(true)

  await page.goto(`/${ORG}/issues`)
  await page.keyboard.press('Meta+k')
  const palette = page.getByRole('dialog', { name: 'Command palette' })
  await expect(palette).toBeVisible()
  await page.getByLabel('Search the palette').fill(traceId.toUpperCase())
  await palette.getByRole('option', { name: `Open trace ${traceId}` }).click()
  await expect(page).toHaveURL(new RegExp(`/${ORG}/traces/${traceId}$`))
  await expect(page.getByRole('heading', { name: 'Trace', level: 1 })).toBeVisible()

  // Span processing stores it within a second or so.
  const spans = page.getByRole('table', { name: 'Spans' })
  await expect(async () => {
    await page.reload()
    await expect(spans.locator('[data-span="f000000000000001"]')).toBeVisible({ timeout: 1000 })
  }).toPass({ timeout: 20_000 })
  const segment = spans.locator('[data-span="f000000000000001"]')
  await expect(segment).toContainText('http.server')
  await expect(segment).toContainText('OrdersController#show')
  await expect(segment).toContainText('120 ms')
  await expect(segment).toContainText('90 ms')
  const query = spans.locator('[data-span="f000000000000002"]')
  await expect(query).toContainText('SELECT * FROM orders WHERE id = 42')
  await expect(query).toContainText('30 ms')
  expect(errors).toEqual([])
})

const keyOf = () => runner(`puts Project.find_by!(slug: ${JSON.stringify(PROJECT)}).project_keys.first.public_key`)

// An Event raised in a trace, as an envelope's event item.
function eventItem(traceId: string, message: string): string {
  const event = {
    event_id: randomBytes(16).toString('hex'), platform: 'ruby', level: 'error', message,
    timestamp: Date.now() / 1000 - 4, environment: 'production',
    contexts: { trace: { trace_id: traceId, span_id: 'f000000000000002' } },
  }
  return [JSON.stringify({ type: 'event' }), JSON.stringify(event)].join('\n')
}

async function send(request: APIRequestContext, key: string, traceId: string, message: string, sampled: boolean) {
  const items = sampled ? `${envelope(traceId, key)}\n${eventItem(traceId, message)}` :
    `${JSON.stringify({ sent_at: new Date().toISOString() })}\n${eventItem(traceId, message)}`
  const sent = await request.post(`${ingest}/api/1/envelope`, {
    headers: { 'X-Sentry-Auth': `Sentry sentry_version=7, sentry_key=${key}`, 'Content-Type': 'application/x-sentry-envelope' },
    data: items,
  })
  expect(sent.ok(), await sent.text()).toBe(true)
  const logged = await request.post(`${ingest}/api/logs`, {
    headers: { Authorization: `Bearer ${key}` },
    data: [{ message: `Charging card for ${message}`, time: new Date(Date.now() - 4500).toISOString(), trace_id: traceId }],
  })
  expect(logged.ok(), await logged.text()).toBe(true)
}

// The Issue page of the Event's Issue, once Event processing has made it.
async function openIssue(page: Page, message: string) {
  let number = ''
  await expect(async () => {
    number = runner(`print Project.find_by!(slug: ${JSON.stringify(PROJECT)}).issues.where("title LIKE ?", ${JSON.stringify(`%${message}%`)}).pick(:number)`)
    expect(number).not.toBe('')
  }).toPass({ timeout: 30_000 })
  await page.goto(`/${ORG}/projects/${PROJECT}/issues/${number}`)
}

// The trace's Logs in the explorer, the first opened in the inspector.
async function inspectLog(page: Page, traceId: string) {
  const search = `?${new URLSearchParams({ range: '1h', mode: 'query', q: `where trace_id == "${traceId}"` })}`
  await expect(async () => {
    await openLogs(page, search)
    await expect(streamRows(page).first()).toBeVisible({ timeout: 1000 })
  }).toPass({ timeout: 20_000 })
  await streamRows(page).first().click({ position: { x: 60, y: 15 } })
  const inspector = page.getByRole('complementary', { name: 'Log inspector' })
  await expect(inspector).toBeVisible()
  return inspector
}

test("an Issue's Event and a Log open their trace, with its Spans, Issues and Logs", async ({ page, request }) => {
  const errors = watchErrors(page)
  const traceId = randomBytes(16).toString('hex')
  const message = `Payment declined ${traceId.slice(0, 8)}`
  await send(request, keyOf(), traceId, message, true)

  await openIssue(page, message)
  await page.getByRole('link', { name: 'Open trace' }).click()
  await expect(page).toHaveURL(new RegExp(`/${ORG}/traces/${traceId}\\?time=`))
  const spans = page.getByRole('table', { name: 'Spans' })
  await expect(async () => {
    await page.reload()
    await expect(spans.locator('[data-span="f000000000000001"]')).toBeVisible({ timeout: 1000 })
  }).toPass({ timeout: 20_000 })
  await expect(spans.locator('[data-span="f000000000000001"]')).toHaveAttribute('data-critical', 'true')
  await expect(page.getByRole('list', { name: 'Issues' })).toContainText(message)
  await expect(page.getByRole('list', { name: 'Logs' })).toContainText(`Charging card for ${message}`)

  // The Issue links back to its Event.
  await page.getByRole('list', { name: 'Issues' }).getByRole('link', { name: message }).click()
  await expect(page).toHaveURL(/\/issues\/\d+\?event_id=/)

  const inspector = await inspectLog(page, traceId)
  await inspector.getByRole('link', { name: 'Open trace' }).click()
  await expect(page).toHaveURL(new RegExp(`/${ORG}/traces/${traceId}\\?time=`))
  await expect(spans.locator('[data-span="f000000000000001"]')).toBeVisible()
  expect(errors).toEqual([])
})

test("an unsampled trace opens from its Log and its Issue, with a note, its Logs and its Issue", async ({ page, request }) => {
  const errors = watchErrors(page)
  const traceId = randomBytes(16).toString('hex')
  const message = `Card expired ${traceId.slice(0, 8)}`
  await send(request, keyOf(), traceId, message, false)

  const expectUnsampled = async () => {
    await expect(page.locator('[data-notice="unsampled"]')).toContainText("wasn't sampled")
    await expect(page.getByRole('table', { name: 'Spans' })).toHaveCount(0)
    await expect(page.getByRole('list', { name: 'Issues' })).toContainText(message)
    await expect(page.getByRole('list', { name: 'Logs' })).toContainText(`Charging card for ${message}`)
  }

  const inspector = await inspectLog(page, traceId)
  await inspector.getByRole('link', { name: 'Open trace' }).click()
  await expect(page).toHaveURL(new RegExp(`/${ORG}/traces/${traceId}\\?time=`))
  await expect(async () => {
    await page.reload()
    await expect(page.getByRole('list', { name: 'Issues' })).toBeVisible({ timeout: 1000 })
  }).toPass({ timeout: 30_000 })
  await expectUnsampled()

  await openIssue(page, message)
  await page.getByRole('link', { name: 'Open trace' }).click()
  await expectUnsampled()
  expect(errors).toEqual([])
})
