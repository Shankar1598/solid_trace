import { test, expect, type Page } from '@playwright/test'
import { PROJECT } from '../support/data'
import { logCount, openLogs, streamRows } from '../support/logs'
import { field, fixture, ingest, inspector, key, logsSince, postOtlp } from '../support/otlp'
import { watchErrors } from '../support/screens'

// OTLP/HTTP metrics ingest. Two exports of real Claude Code (scrubbed): one as
// protobuf with gzip and one as JSON. The flow posts both to POST /v1/metrics
// with the Project's key. Each Metric data point is stored as one Log, with
// the metric name as its message. The cost Logs show in the Logs explorer
// with the Fields of a data point in the inspector, and a Query that sums
// metric.value finds the cost of each export.

const protobuf = fixture('claude_code_metrics.pb')
const json = fixture('claude_code_metrics.json')
const MODEL = 'claude-haiku-5-5'

// Each export has one claude_code.cost.usage data point, and a start time of
// its own. The cost is what the export spent, in USD.
const PROTOBUF_COST = { start: '2026-11-24T12:16:53.993Z', total: 0.0051069 }
const JSON_COST = { start: '2026-11-24T12:12:32.426Z', total: 0.00309356 }

const COST = `['${PROJECT}']\n| where message == "claude_code.cost.usage"`

/** The Logs of a Query, from the fixtures' day to an hour ahead. */
const costLogs = (query = COST) => logsSince('2026-11-24', query)

/**
 * The sum of metric.value over the cost Logs of one model, from a Query in
 * the Table view. The cell shows the sum rounded, and its title holds the
 * exact number. Other runs add cost Logs, so the flow reads the sum before
 * and after a post. A row with no title on its sum fails the test.
 */
async function costSum(page: Page, model: string): Promise<number> {
  await openLogs(page, logsSince('2026-11-24', `${COST}\n| summarize sum(metric.value) by model`, 'table'))
  const row = page.locator('[data-table-row]', { hasText: model })
  // A stack with no cost Logs yet has no rows, so the sum is 0.
  await expect(row.or(page.getByText('No rows.'))).toBeVisible()
  if (await row.count() === 0) return 0
  const exact = await row.locator('[title]').last().getAttribute('title')
  if (exact === null) throw new Error(`the sum cell of ${model} has no title`)
  return Number(exact)
}

test('the Metric data points of Claude Code, as protobuf and as JSON, show as Logs in the explorer and add up in a Query', async ({ page, request }) => {
  const errors = watchErrors(page)
  await openLogs(page, costLogs())
  const before = await logCount(page)
  const sumBefore = await costSum(page, MODEL)

  // What a Collector sends: protobuf with gzip, then JSON.
  await postOtlp(request, '/v1/metrics', protobuf, 'application/x-protobuf', true)
  await postOtlp(request, '/v1/metrics', json, 'application/json')

  // Log processing stores the Logs about once a second. Each export has one
  // cost data point.
  await expect(async () => {
    await openLogs(page, costLogs())
    expect(await logCount(page)).toBeGreaterThanOrEqual(before + 2)
  }).toPass({ timeout: 30_000 })

  // The two exports add their own cost to the sum of metric.value.
  await expect(async () => {
    const sum = await costSum(page, MODEL)
    expect(sum - sumBefore).toBeCloseTo(PROTOBUF_COST.total + JSON_COST.total, 8)
  }).toPass({ timeout: 30_000 })

  // A cost Log in the inspector shows the Fields of the mapping, for each export.
  for (const { start, total } of [PROTOBUF_COST, JSON_COST]) {
    await openLogs(page, costLogs(`${COST} and metric.start_time == "${start}"`))
    await streamRows(page).first().click()
    await expect(inspector(page)).toBeVisible()
    const has = (name: string, value: string) => expect(field(page, name)).toContainText(value)
    await has('message', 'claude_code.cost.usage')
    await has('level', 'info')
    await has('service', 'claude-code')
    await has('metric.value', String(total))
    await has('metric.unit', 'USD')
    await has('metric.type', 'sum')
    await has('metric.temporality', 'delta')
    await has('model', MODEL)
  }
  expect(errors).toEqual([])
})

test('a request to /v1/metrics without a key, or with a body of another type, is refused', async ({ request }) => {
  const noKey = await request.post(`${ingest}/v1/metrics`, { headers: { 'Content-Type': 'application/json' }, data: json })
  expect(noKey.status()).toBe(401)
  const wrongType = await request.post(`${ingest}/v1/metrics`, {
    headers: { Authorization: `Bearer ${key}`, 'Content-Type': 'text/plain' }, data: 'claude_code.cost.usage',
  })
  expect(wrongType.status()).toBe(415)
})
