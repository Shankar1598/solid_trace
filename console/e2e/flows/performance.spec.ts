import { test, expect } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import { randomBytes } from 'node:crypto'
import { resolve } from 'node:path'
import { ORG } from '../support/data'
import { watchErrors } from '../support/screens'
import { pickQuickRange } from '../support/logs'

// Performance: Transactions sent to a Project of the run's own list by the
// time they consume, sort by another column, and open a Transaction and
// then one of its slowest traces.

const ingest = process.env.E2E_INGEST_URL ?? 'http://localhost:4000'
const name = `Performance ${Date.now()}`
const slug = name.toLowerCase().replace(' ', '-')

const runner = (code: string) => execFileSync('mise', ['exec', '--', 'bin/rails', 'runner', code], {
  cwd: resolve(__dirname, '../..'), stdio: 'pipe', timeout: 120_000,
}).toString().trim()

// An envelope as sentry-ruby frames it: one request taking ms, with a query
// under it, sampled at rate.
function envelope(key: string, traceId: string, transaction: string, ms: number, rate: string): string {
  const start = Date.now() / 1000 - 60
  const event = {
    event_id: randomBytes(16).toString('hex'), type: 'transaction', platform: 'ruby',
    transaction, transaction_info: { source: 'view' },
    start_timestamp: start, timestamp: start + ms / 1000, environment: 'production', release: 'flow@1.0.0',
    contexts: { trace: { trace_id: traceId, span_id: 'e000000000000001', op: 'http.server', status: 'ok' } },
    spans: [{
      trace_id: traceId, span_id: 'e000000000000002', parent_span_id: 'e000000000000001', op: 'db.sql.active_record',
      description: 'SELECT * FROM carts WHERE id = 7', status: null, start_timestamp: start, timestamp: start + ms / 2000,
      tags: {}, data: {},
    }],
  }
  const header = { event_id: event.event_id, sent_at: new Date().toISOString(), trace: { trace_id: traceId, sample_rate: rate, public_key: key } }
  return [JSON.stringify(header), JSON.stringify({ type: 'transaction', content_type: 'application/json' }), JSON.stringify(event)].join('\n')
}

let key = ''

test.beforeAll(() => {
  key = runner(`org = Organization.find_by!(slug: ${JSON.stringify(ORG)}); ` +
    `p = Project.build_with_key(organization: org, name: ${JSON.stringify(name)}, slug: ${JSON.stringify(slug)}); p.save!; ` +
    'puts p.project_keys.first.public_key')
})

test.afterAll(() => {
  runner(`Project.where(slug: ${JSON.stringify(slug)}).destroy_all`)
})

test('Performance lists Transactions by time consumed, sorts, and opens a Transaction and its slowest trace', async ({ page, request }) => {
  const errors = watchErrors(page)
  // The slow one took 400 ms once; the busy one 20 ms, sampled at 1%, so
  // it stands for 100 requests and 2 s.
  const slowTrace = randomBytes(16).toString('hex')
  for (const sent of [
    envelope(key, slowTrace, 'SlowController#show', 400, '1'),
    envelope(key, randomBytes(16).toString('hex'), 'BusyController#index', 20, '0.01'),
  ]) {
    const answer = await request.post(`${ingest}/api/1/envelope`, {
      headers: { 'X-Sentry-Auth': `Sentry sentry_version=7, sentry_key=${key}`, 'Content-Type': 'application/x-sentry-envelope' },
      data: sent,
    })
    expect(answer.ok(), await answer.text()).toBe(true)
  }

  await page.goto(`/${ORG}/issues`)
  await page.getByRole('group', { name: 'Performance' }).getByRole('link', { name }).click()
  await expect(page).toHaveURL(new RegExp(`/${ORG}/projects/${slug}/performance$`))
  await expect(page.getByRole('heading', { name: 'Performance', level: 1 })).toBeVisible()

  // Span processing stores them within a second or so.
  const table = page.getByRole('table', { name: 'Transactions' })
  const names = table.locator('tbody tr td:first-child a')
  await expect(async () => {
    await page.reload()
    await expect(names).toHaveText(['BusyController#index', 'SlowController#show'], { timeout: 1000 })
  }).toPass({ timeout: 20_000 })
  await expect(table.locator('[data-transaction="BusyController#index"] [data-number="throughput_per_minute"]')).toContainText('~')

  await table.getByRole('button', { name: 'p95' }).click()
  await expect(names).toHaveText(['SlowController#show', 'BusyController#index'])

  // The range is kept in the URL, and carried to the Transaction.
  await pickQuickRange(page, '7 days')
  await expect(page).toHaveURL(/range=7d/)
  await table.getByRole('link', { name: 'SlowController#show' }).click()
  await expect(page).toHaveURL(/\/performance\/transaction\?name=SlowController%23show&range=7d/)
  await expect(page.getByRole('heading', { name: 'SlowController#show', level: 1 })).toBeVisible()
  await expect(page.getByRole('table', { name: 'Releases' })).toContainText('flow@1.0.0')
  await expect(page.getByRole('table', { name: 'Self time by op' })).toContainText('db.sql.active_record')

  const slowest = page.getByRole('table', { name: 'Slowest traces' })
  await expect(slowest.locator(`[data-trace="${slowTrace}"]`)).toContainText('400 ms')
  await slowest.getByRole('link', { name: slowTrace }).click()
  await expect(page).toHaveURL(new RegExp(`/${ORG}/traces/${slowTrace}$`))
  await expect(page.getByRole('table', { name: 'Spans' }).locator('[data-span="e000000000000001"]')).toContainText('SlowController#show')
  expect(errors).toEqual([])
})
