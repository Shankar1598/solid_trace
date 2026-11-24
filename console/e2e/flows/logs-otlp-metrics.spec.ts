import { test, expect, type APIRequestContext, type Page } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { gzipSync } from 'node:zlib'
import { PROJECT } from '../support/data'
import { logCount, openLogs, streamRows } from '../support/logs'
import { watchErrors } from '../support/screens'

// OTLP/HTTP metrics ingest. Two exports of real Claude Code (scrubbed): one as
// protobuf with gzip and one as JSON. The flow posts both to POST /v1/metrics
// with the Project's key. Each Metric data point is stored as one Log, with
// the metric name as its message. The cost Logs show in the Logs explorer
// with the Fields of a data point in the inspector, and a Query that sums
// metric.value finds the cost of each export.

const ingest = process.env.E2E_INGEST_URL ?? 'http://localhost:4000'
const fixtures = resolve(__dirname, '../../../event_store/ingest/normalise/testdata/otlp')
const protobuf = readFileSync(resolve(fixtures, 'claude_code_metrics.pb'))
const json = readFileSync(resolve(fixtures, 'claude_code_metrics.json'))

// Each export has one claude_code.cost.usage data point, and a start time of
// its own. The cost is what the export spent, in USD.
const PROTOBUF_COST = { start: '2026-11-24T12:16:53.993Z', total: 0.0051069 }
const JSON_COST = { start: '2026-11-24T12:12:32.426Z', total: 0.00309356 }

const key = execFileSync('mise', ['exec', '--', 'bin/rails', 'runner',
  `puts Project.find_by!(slug: ${JSON.stringify(PROJECT)}).project_keys.first.public_key`], {
  cwd: resolve(__dirname, '../..'), stdio: 'pipe', timeout: 120_000,
}).toString().trim()

/**
 * The cost Logs, from the fixtures' day to an hour ahead. The Logs keep the
 * fixtures' own time until the Project's Retention passes it. Then they take
 * the time they arrived. This range holds both.
 */
function search(query: string, extra = ''): string {
  const from = new Date(Date.parse('2026-11-24T00:00:00Z')).toISOString()
  const to = new Date(Date.now() + 3600_000).toISOString()
  return `?${new URLSearchParams({ from, to, mode: extra ? 'query' : 'builder', q: query })}${extra}`
}

const COST = `['${PROJECT}']\n| where message == "claude_code.cost.usage"`

const inspector = (page: Page) => page.getByRole('complementary', { name: 'Log inspector' })
/** An inspector row by its Field name, its value in the second cell. */
const field = (page: Page, name: string) =>
  inspector(page).locator('div.group', { has: page.getByText(name, { exact: true }) }).first()

/**
 * The sum of metric.value over the cost Logs, from a Query in the Table view.
 * The cell shows the sum rounded, and its title holds the exact number. Other
 * runs add cost Logs, so the flow reads the sum before and after a post.
 */
async function costSum(page: Page): Promise<number> {
  await openLogs(page, search(`${COST}\n| summarize sum(metric.value)`, '&viz=table'))
  const row = page.locator('[data-table-row]').first()
  await expect(row).toBeVisible()
  const exact = await row.locator('[title]').first().getAttribute('title')
  return exact === null ? 0 : Number(exact)
}

async function post(request: APIRequestContext, body: Buffer, type: string, gzip: boolean) {
  const response = await request.post(`${ingest}/v1/metrics`, {
    headers: { Authorization: `Bearer ${key}`, 'Content-Type': type, ...(gzip ? { 'Content-Encoding': 'gzip' } : {}) },
    data: gzip ? gzipSync(body) : body,
  })
  expect(response.status(), await response.text()).toBe(200)
}

test('the Metric data points of Claude Code, as protobuf and as JSON, show as Logs in the explorer and add up in a Query', async ({ page, request }) => {
  const errors = watchErrors(page)
  await openLogs(page, search(COST))
  const before = await logCount(page)
  const sumBefore = await costSum(page)

  // What a Collector sends: protobuf with gzip, then JSON.
  await post(request, protobuf, 'application/x-protobuf', true)
  await post(request, json, 'application/json', false)

  // Log processing stores the Logs about once a second. Each export has one
  // cost data point.
  await expect(async () => {
    await openLogs(page, search(COST))
    expect(await logCount(page)).toBeGreaterThanOrEqual(before + 2)
  }).toPass({ timeout: 30_000 })

  // The two exports add their own cost to the sum of metric.value.
  await expect(async () => {
    const sum = await costSum(page)
    expect(sum - sumBefore).toBeCloseTo(PROTOBUF_COST.total + JSON_COST.total, 8)
  }).toPass({ timeout: 30_000 })

  // A cost Log in the inspector shows the Fields of the mapping, for each export.
  for (const { start, total } of [PROTOBUF_COST, JSON_COST]) {
    await openLogs(page, search(`${COST} and metric.start_time == "${start}"`))
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
    await has('model', 'claude-haiku-5-5')
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
