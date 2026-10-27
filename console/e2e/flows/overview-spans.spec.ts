import { test, expect } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import { randomBytes } from 'node:crypto'
import { resolve } from 'node:path'
import { ORG } from '../support/data'
import { watchErrors } from '../support/screens'

// The Overview's Spans: a Transaction of two Spans sent to a Project shows
// in the Spans section as received, and once Span processing stores it, as
// stored, with the Project's trace Retention. Each run makes its own
// Project, with a trace Retention of its own, and deletes it after.

const ingest = process.env.E2E_INGEST_URL ?? 'http://localhost:4000'
const name = `Spans ${Date.now()}`
const slug = name.toLowerCase().replace(' ', '-')

const runner = (code: string) => execFileSync('mise', ['exec', '--', 'bin/rails', 'runner', code], {
  cwd: resolve(__dirname, '../..'), stdio: 'pipe', timeout: 120_000,
}).toString().trim()

let key = ''

test.beforeAll(() => {
  key = runner(`org = Organization.find_by!(slug: ${JSON.stringify(ORG)}); ` +
    `p = Project.build_with_key(organization: org, name: ${JSON.stringify(name)}, slug: ${JSON.stringify(slug)}, trace_retention_days: 3); ` +
    'p.save!; puts p.project_keys.first.public_key').split('\n').at(-1)!
})

test.afterAll(() => {
  runner(`Project.where(slug: ${JSON.stringify(slug)}).destroy_all`)
})

// A request with one query under it, as sentry-ruby frames it.
function envelope(traceId: string): string {
  const start = Date.now() / 1000 - 5
  const transaction = {
    event_id: randomBytes(16).toString('hex'), type: 'transaction', platform: 'ruby',
    transaction: 'OrdersController#show', transaction_info: { source: 'view' },
    start_timestamp: start, timestamp: start + 0.12, environment: 'production',
    contexts: { trace: { trace_id: traceId, span_id: 'e000000000000001', op: 'http.server', status: 'ok' } },
    spans: [{
      trace_id: traceId, span_id: 'e000000000000002', parent_span_id: 'e000000000000001', op: 'db.sql.active_record',
      description: 'SELECT 1', status: 'ok', start_timestamp: start + 0.01, timestamp: start + 0.04,
    }],
  }
  const header = { event_id: transaction.event_id, sent_at: new Date().toISOString(), trace: { trace_id: traceId, sample_rate: '1', public_key: key } }
  return [JSON.stringify(header), JSON.stringify({ type: 'transaction' }), JSON.stringify(transaction)].join('\n')
}

test('a Project\'s Spans show on the Overview, received and stored, with its trace Retention', async ({ page, request }) => {
  const errors = watchErrors(page)
  const sent = await request.post(`${ingest}/api/1/envelope`, {
    headers: { 'X-Sentry-Auth': `Sentry sentry_version=7, sentry_key=${key}`, 'Content-Type': 'application/x-sentry-envelope' },
    data: envelope(randomBytes(16).toString('hex')),
  })
  expect(sent.ok(), await sent.text()).toBe(true)

  // Ingest counts are written about once a second, and Span processing
  // stores the Spans about as often.
  const row = page.locator(`[data-section="spans"] [data-project="${slug}"]`)
  await page.goto(`/${ORG}/overview`)
  await expect(async () => {
    await page.reload()
    await expect(row).toBeVisible({ timeout: 1000 })
    await expect(row.locator('td').nth(1)).toHaveText('2')
    await expect(row.locator('td').nth(3)).toHaveText('2')
  }).toPass({ timeout: 20_000 })
  await expect(row).toContainText(name)
  await expect(row).toContainText('3 days')
  await expect(page.locator('[data-section="spans"] [data-total]')).toContainText('All Projects')

  expect(errors).toEqual([])
})
