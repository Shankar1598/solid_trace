import { test, expect, type Page } from '@playwright/test'
import { ORG, PROJECT } from '../support/data'
import { logCount, openLogs, streamRows } from '../support/logs'
import { field, fixture, ingest, inspector, key, logsSince, postOtlp } from '../support/otlp'
import { watchErrors } from '../support/screens'

// OTLP/HTTP Log ingest. A real Collector (otelcol 0.165.0) sent two requests:
// one as protobuf with gzip, and one as JSON. The flow posts both to
// POST /v1/logs with the Project's key. Each request holds 3 Logs of service
// checkout and environment staging, with Attributes of every type. The Logs
// show in the Logs explorer, with those Fields in the inspector. The
// Overview's Received numbers go up.

const protobuf = fixture('collector.pb')
const json = fixture('collector.json')
const LOGS_PER_REQUEST = 3

/** The checkout Logs, from the fixtures' day to an hour ahead. */
const checkoutLogs = () => logsSince('2026-11-22', `['${PROJECT}']\n| where service == "checkout"`)

/** What the Overview's band says arrived: the Logs of Received, and the requests that brought them. */
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

  // What a Collector sends: protobuf with gzip, then JSON. Each request has
  // the Project's key as a Bearer token.
  await postOtlp(request, '/v1/logs', protobuf, 'application/x-protobuf', true)
  await postOtlp(request, '/v1/logs', json, 'application/json')

  // Log processing stores the Logs about once a second.
  await expect(async () => {
    await openLogs(page, checkoutLogs())
    expect(await logCount(page)).toBeGreaterThanOrEqual(before + 2 * LOGS_PER_REQUEST)
  }).toPass({ timeout: 30_000 })
  await expect(streamRows(page).first()).toContainText('order placed')

  // The newest Log in the inspector shows the Fields of the mapping, from
  // the resource and from the record.
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
  // An int64 that a double cannot hold stays exact.
  await has('order.id', '9007199254740993')
  await has('retried', 'false')
  await has('tags', '["a","b"]')
  // The resource's service.name and deployment.environment.name became
  // built-in Fields. They are not Attributes too.
  await expect(inspector(page).getByText('service.name', { exact: true })).toHaveCount(0)
  await expect(inspector(page).getByText('deployment.environment.name', { exact: true })).toHaveCount(0)
  expect(errors).toEqual([])

  // The Overview counts both requests and their Logs. EventStore writes the
  // Ingest counts about once a second.
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
