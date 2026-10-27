import { test, expect } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import { randomBytes } from 'node:crypto'
import { resolve } from 'node:path'
import { ORG, PROJECT } from '../support/data'
import { watchErrors } from '../support/screens'

// A Transaction sent as a Sentry SDK sends it shows as a trace: its id
// pasted into the command palette opens the waterfall of its Spans.

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
