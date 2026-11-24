import { test, expect, type Page } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { gzipSync } from 'node:zlib'
import { ORG, PROJECT } from '../support/data'
import { logCount, openLogs, streamRows } from '../support/logs'
import { watchErrors } from '../support/screens'

// OTLP/HTTP Log ingest: the two requests a real Collector (otelcol 0.165.0)
// sent, one as protobuf with gzip and one as JSON, go to POST /v1/logs with
// the Project's key. Each holds 3 Logs of service checkout, environment
// staging, with Attributes of every type. The Logs show in the Logs
// explorer with those Fields in the inspector, and the Overview's Received
// numbers go up.

const ingest = process.env.E2E_INGEST_URL ?? 'http://localhost:4000'
const fixtures = resolve(__dirname, '../../../event_store/ingest/normalise/testdata/otlp')
const protobuf = readFileSync(resolve(fixtures, 'collector.pb'))
const json = readFileSync(resolve(fixtures, 'collector.json'))
const LOGS_PER_REQUEST = 3

const key = execFileSync('mise', ['exec', '--', 'bin/rails', 'runner',
  `puts Project.find_by!(slug: ${JSON.stringify(PROJECT)}).project_keys.first.public_key`], {
  cwd: resolve(__dirname, '../..'), stdio: 'pipe', timeout: 120_000,
}).toString().trim()

/**
 * The checkout Logs from the fixtures' day to an hour ahead. The Logs keep
 * the fixtures' own time until a Project's Retention passes it, then take
 * the time they arrived, and this range holds both.
 */
function checkoutLogs(): string {
  const from = new Date(Date.parse('2026-11-22T00:00:00Z')).toISOString()
  const to = new Date(Date.now() + 3600_000).toISOString()
  const q = `['${PROJECT}']\n| where service == "checkout"`
  return `?${new URLSearchParams({ from, to, mode: 'builder', q })}`
}

const inspector = (page: Page) => page.getByRole('complementary', { name: 'Log inspector' })
/** An inspector row by its Field name, its value in the second cell. */
const field = (page: Page, name: string) =>
  inspector(page).locator('div.group', { has: page.getByText(name, { exact: true }) }).first()

/** What the Overview's band says arrived: Received's Logs, and the requests that brought them. */
async function received(page: Page): Promise<{ logs: number, requests: number }> {
  await page.goto(`/${ORG}/overview?range=7d`)
  const band = page.locator('[data-section="band"]')
  await expect(band.locator('[data-step="received"]')).toContainText('Logs')
  const logs = await band.locator('[data-step="received"] [title$=" Logs"]').getAttribute('title')
  const requests = /in ([\d,.]+) requests?/.exec((await band.innerText()).replace(/\s+/g, ' '))
  expect(requests, 'the band names the requests that Received counts').not.toBeNull()
  return { logs: Number(logs!.replace(/[^\d]/g, '')), requests: Number(requests![1].replace(/,/g, '')) }
}

test('the Logs of a Collector, as protobuf and as JSON, show in the explorer, the inspector and the Overview', async ({ page, request }) => {
  const errors = watchErrors(page)
  await openLogs(page, checkoutLogs())
  const before = await logCount(page)
  const arrived = await received(page)

  // What a Collector sends: protobuf with gzip, then JSON, each with the
  // Project's key as a Bearer token.
  const protobufSent = await request.post(`${ingest}/v1/logs`, {
    headers: { Authorization: `Bearer ${key}`, 'Content-Type': 'application/x-protobuf', 'Content-Encoding': 'gzip' },
    data: gzipSync(protobuf),
  })
  expect(protobufSent.status(), await protobufSent.text()).toBe(200)
  const jsonSent = await request.post(`${ingest}/v1/logs`, {
    headers: { Authorization: `Bearer ${key}`, 'Content-Type': 'application/json' },
    data: json,
  })
  expect(jsonSent.status(), await jsonSent.text()).toBe(200)

  // Log processing stores the Logs about once a second.
  await expect(async () => {
    await openLogs(page, checkoutLogs())
    expect(await logCount(page)).toBe(before + 2 * LOGS_PER_REQUEST)
  }).toPass({ timeout: 30_000 })
  await expect(streamRows(page).first()).toContainText('order placed')

  // The newest Log in the inspector: the Fields of the mapping, from the
  // resource and from the record.
  await streamRows(page).first().click({ position: { x: 60, y: 15 } })
  await expect(inspector(page)).toBeVisible()
  const has = (name: string, value: string) => expect(field(page, name)).toContainText(value)
  await has('level', 'warn')
  await has('service', 'checkout')
  await has('environment', 'staging')
  await has('message', 'order placed')
  await has('trace_id', '5b8efff798038103d269b633813fc60c')
  await has('span_id', 'b0e6f15b45c36b12')
  await expect(inspector(page).getByText('Attributes · 6')).toBeVisible()
  await has('host.name', 'web-1')
  await has('app', 'server')
  await has('http.request.method', 'POST')
  // An int64 that a double can't hold stays exact.
  await has('order.id', '9007199254740993')
  await has('retried', 'false')
  await has('tags', '["a","b"]')
  // The resource's service.name and deployment.environment.name became the
  // built-in Fields: they are not Attributes too.
  await expect(inspector(page).getByText('service.name', { exact: true })).toHaveCount(0)
  await expect(inspector(page).getByText('deployment.environment.name', { exact: true })).toHaveCount(0)
  expect(errors).toEqual([])

  // The Overview counts both requests and their Logs. Ingest counts are
  // written about once a second.
  await expect(async () => {
    const now = await received(page)
    expect(now.logs - arrived.logs).toBeGreaterThanOrEqual(2 * LOGS_PER_REQUEST)
    expect(now.requests - arrived.requests).toBeGreaterThanOrEqual(2)
  }).toPass({ timeout: 30_000 })
})

test('a request without a key, or with a body of another type, is refused', async ({ request }) => {
  const noKey = await request.post(`${ingest}/v1/logs`, { headers: { 'Content-Type': 'application/json' }, data: json })
  expect(noKey.status()).toBe(401)
  const wrongType = await request.post(`${ingest}/v1/logs`, {
    headers: { Authorization: `Bearer ${key}`, 'Content-Type': 'text/plain' }, data: 'order placed',
  })
  expect(wrongType.status()).toBe(415)
})
