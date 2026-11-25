import { test, expect, type APIRequestContext, type Locator, type Page } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import { resolve } from 'node:path'
import { gzipSync } from 'node:zlib'
import { ADMIN, ORG } from '../support/data'
import { logCount, settled, streamRows } from '../support/logs'
import { field, fixture, ingest, inspector, logsSince } from '../support/otlp'
import { failedLoad, watchErrors } from '../support/screens'

// Claude Code telemetry into the Agent Project. An Admin makes the Agent
// Project's one key on its page. The flow sends OTLP Logs and Metric data
// points with that key. Some are of the Captured repository, and one of them
// has a forged file and run.id. Others are of another repository or of no
// repository, or have the repository on the resource only. EventStore keeps
// only the records of the Captured repository, each in the file otel, and
// counts the others as filtered out. The key gets 403 on routes that are not
// OTLP. Run events sent with the Dispatcher's API token are stored as before.

const RUN = Date.now().toString(36)
const ENTRY = 'github.com/Example-Team/App'
const CAPTURED = 'https://github.com/example-team/app'
const ANOTHER = 'https://github.com/example-team/other'
const FORGED_RUN = `x1${RUN}`
const DISPATCHER_RUN = `cco05${RUN}`
const TOKEN_NAME = `Flow Dispatcher ${RUN}`

const rails = (code: string) => execFileSync('mise', ['exec', '--', 'bin/rails', 'runner', code], {
  cwd: resolve(__dirname, '../..'), stdio: 'pipe', timeout: 120_000,
}).toString().trim()

type Setup = { id: number; slug: string; entriesBefore: string[]; dispatcherSecret: string }
let setup: Setup

test.beforeAll(() => {
  setup = JSON.parse(rails(`
    org = Organization.find_by!(slug: ${JSON.stringify(ORG)})
    admin = User.find_by!(email: ${JSON.stringify(ADMIN.email)})
    project = org.agent_project
    before = project&.captured_repositories || []
    result = if project
      AgentSessionsSettings.save(project: project, user: admin, entries: before | [${JSON.stringify(ENTRY)}], confirmed: true)
    else
      AgentSessionsSettings.turn_on(organization: org, user: admin, entries: [${JSON.stringify(ENTRY)}], confirmed: true)
    end
    raise result.errors.inspect unless result.done?
    project = result.project
    project.project_keys.destroy_all
    _, secret = ApiToken.issue!(user: admin, name: ${JSON.stringify(TOKEN_NAME)}, scopes: Oauth::Scopes::DISPATCHER, organization_id: org.id)
    print({ id: project.id, slug: project.slug, entriesBefore: before, dispatcherSecret: secret }.to_json)
  `))
})

test.afterAll(() => {
  if (!setup) return
  rails(`
    admin = User.find_by!(email: ${JSON.stringify(ADMIN.email)})
    project = Project.find(${setup.id})
    project.project_keys.destroy_all
    AgentSessionsSettings.save(project: project, user: admin, entries: ${JSON.stringify(setup.entriesBefore)}, confirmed: true) if ${JSON.stringify(setup.entriesBefore)}.any?
    ApiToken.where(name: ${JSON.stringify(TOKEN_NAME)}).delete_all
  `)
})

type Attributes = Record<string, string | number>

const otlpAttributes = (attributes: Attributes) => Object.entries(attributes).map(([key, value]) => ({
  key, value: typeof value === 'number' ? { intValue: String(value) } : { stringValue: value },
}))

const resource = (attributes: Attributes = {}) => ({ attributes: otlpAttributes({ 'service.name': 'claude-code', ...attributes }) })

const own = (repository?: string, more: Attributes = {}): Attributes =>
  ({ ...(repository ? { 'vcs.repository.url.full': repository } : {}), 'flow.run': RUN, ...more })

const nowNano = () => `${Date.now()}000000`

function otlpLogs(records: { message: string; attributes: Attributes; resource?: Attributes }[]) {
  return {
    resourceLogs: records.map(r => ({
      resource: resource(r.resource),
      scopeLogs: [{
        scope: { name: 'com.anthropic.claude_code.events' },
        logRecords: [{ timeUnixNano: nowNano(), body: { stringValue: r.message }, attributes: otlpAttributes(r.attributes) }],
      }],
    })),
  }
}

function otlpMetrics(points: { name: string; attributes: Attributes }[]) {
  return {
    resourceMetrics: [{
      resource: resource(),
      scopeMetrics: [{
        scope: { name: 'com.anthropic.claude_code' },
        metrics: points.map(p => ({
          name: p.name, unit: 'count',
          sum: {
            aggregationTemporality: 1, isMonotonic: true,
            dataPoints: [{ startTimeUnixNano: nowNano(), timeUnixNano: nowNano(), asDouble: 1, attributes: otlpAttributes(p.attributes) }],
          },
        })),
      }],
    }],
  }
}

const message = (what: string) => `cco-05 ${RUN} ${what}`

const KEPT_LOG = message('captured')
const FORGED_LOG = message('forged run')
const KEPT_METRIC = `cco05.captured.${RUN}`
const LOGS = otlpLogs([
  { message: KEPT_LOG, attributes: own(CAPTURED) },
  { message: FORGED_LOG, attributes: own(CAPTURED, { file: `runs/${FORGED_RUN}`, 'run.id': FORGED_RUN, 'run.seq': 1 }) },
  { message: message('another repository'), attributes: own(ANOTHER) },
  { message: message('no repository'), attributes: own() },
  { message: message('repository on the resource only'), attributes: own(), resource: { 'vcs.repository.url.full': CAPTURED } },
])
const METRICS = otlpMetrics([
  { name: KEPT_METRIC, attributes: own(CAPTURED) },
  { name: `cco05.another.${RUN}`, attributes: own(ANOTHER) },
  { name: `cco05.none.${RUN}`, attributes: own() },
])
const KEPT = [KEPT_LOG, FORGED_LOG, KEPT_METRIC]

const UNTAGGED_PROTOBUF = [
  { route: '/v1/logs', body: fixture('collector.pb'), records: 3, where: 'service == "checkout"' },
  { route: '/v1/metrics', body: fixture('claude_code_metrics.pb'), records: 12, where: 'metric.start_time == "2026-11-24T12:16:53.993Z"' },
]
const FIXTURES_DAY = '2026-11-22'
const FILTERED = 3 + 2 + UNTAGGED_PROTOBUF.reduce((sum, p) => sum + p.records, 0)

async function postWithKey(request: APIRequestContext, key: string, route: string, body: Buffer | object, protobuf = false) {
  const response = await request.post(`${ingest}${route}`, {
    headers: {
      Authorization: `Bearer ${key}`,
      'Content-Type': protobuf ? 'application/x-protobuf' : 'application/json',
      ...(protobuf ? { 'Content-Encoding': 'gzip' } : {}),
    },
    data: protobuf ? gzipSync(body as Buffer) : body,
  })
  expect(response.status(), await response.text()).toBe(200)
  if (!protobuf) expect(JSON.stringify(await response.json())).not.toMatch(/rejected/i)
}

async function openAgentLogs(page: Page, day: string, where: string) {
  await page.goto(`/${ORG}/projects/${setup.slug}/logs${logsSince(day, `['${setup.slug}']\n| where ${where}`)}`)
  await settled(page)
}

const today = () => new Date().toISOString().slice(0, 10)
const TEST_TIME = 180_000
const msToUtcMidnight = () => {
  const now = new Date()
  return Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate() + 1) - now.getTime()
}
const ofThisRun = `flow.run == "${RUN}"`

async function exactLogs(number: Locator) {
  if (await number.count() === 0) return 0
  return Number((await number.getAttribute('title'))!.replace(/\D/g, ''))
}

async function agentOverview(page: Page): Promise<{ received: number; filtered: number }> {
  await page.goto(`/${ORG}/overview?range=7d`)
  await expect(page.getByRole('heading', { name: 'Overview', exact: true })).toBeVisible()
  await expect(page.getByRole('alert'), "EventStore's numbers are unavailable").toHaveCount(0)
  const received = page.locator(`[data-section="projects"] tr[data-project="${setup.slug}"] td:not([data-empty])`).nth(1)
  const filtered = page.locator('[data-section="band"] dt:has-text("Filtered out as not of a Captured repository") + dd')
  return { received: await exactLogs(received), filtered: await exactLogs(filtered) }
}

test("Claude Code's OpenTelemetry with the Agent Project's key keeps only the Captured repository's records, in the file otel", async ({ page, request }) => {
  // The Logs search and the Overview's 7d range count whole UTC days, so the test runs inside one.
  const wait = msToUtcMidnight() < TEST_TIME ? msToUtcMidnight() + 1_000 : 0
  test.setTimeout(TEST_TIME + wait)
  if (wait) await page.waitForTimeout(wait)
  const day = today()
  const errors = watchErrors(page, [failedLoad(404)])

  await page.goto(`/${ORG}/projects/${setup.slug}`)
  await page.getByRole('button', { name: 'Generate a key' }).click()
  await expect(page.getByText('Key created successfully')).toBeVisible()
  const keyBox = page.locator('input[readonly]')
  await expect(keyBox).toHaveValue(/^[0-9a-f]{32}$/)
  const key = await keyBox.inputValue()

  let before = { received: 0, filtered: 0 }
  await expect(async () => { before = await agentOverview(page) }).toPass({ timeout: 30_000 })

  for (const p of UNTAGGED_PROTOBUF) await postWithKey(request, key, p.route, p.body, true)
  await postWithKey(request, key, '/v1/logs', LOGS)
  await postWithKey(request, key, '/v1/metrics', METRICS)

  const runEvent = {
    time: new Date().toISOString(), level: 'info', message: `Run ${DISPATCHER_RUN}: created`,
    run: { id: DISPATCHER_RUN, seq: 1, state: 'queued', trigger: 'cli', title: message('Dispatcher run'), executions: 0 },
    event: { id: `${DISPATCHER_RUN}-1`, kind: 'created' },
    repository: 'github.com/example-team/app',
  }
  const sent = await request.post(`${ingest}/api/agent/v1/run-events`, {
    headers: { Authorization: `Bearer ${setup.dispatcherSecret}`, 'Content-Type': 'application/json' }, data: [runEvent],
  })
  expect(sent.status(), await sent.text()).toBe(200)
  expect(await sent.json()).toMatchObject({ accepted: 1, dropped: 0 })

  for (const route of [`/api/${setup.id}/store`, `/api/${setup.id}/envelope`, `/api/${setup.id}/logs`, '/api/logs']) {
    const refused = await request.post(`${ingest}${route}`, {
      headers: { Authorization: `Bearer ${key}`, 'X-Sentry-Auth': `Sentry sentry_key=${key}`, 'Content-Type': 'application/json' },
      data: { message: message(`refused on ${route}`) },
    })
    expect(refused.status(), `${route}: ${await refused.text()}`).toBe(403)
  }

  await expect(async () => {
    await openAgentLogs(page, day, ofThisRun)
    expect(await logCount(page)).toBe(KEPT.length)
  }).toPass({ timeout: 30_000 })
  const rows = streamRows(page)
  for (const kept of KEPT) await expect(rows.filter({ hasText: kept })).toHaveCount(1)
  await openAgentLogs(page, day, `${ofThisRun} and file == "otel"`)
  expect(await logCount(page)).toBe(KEPT.length)

  await openAgentLogs(page, day, `${ofThisRun} and run.id == "${FORGED_RUN}"`)
  await streamRows(page).first().click()
  await expect(inspector(page)).toBeVisible()
  await expect(field(page, 'message')).toContainText(FORGED_LOG)
  await expect(field(page, 'original.file')).toContainText(`runs/${FORGED_RUN}`)

  for (const p of UNTAGGED_PROTOBUF) {
    await openAgentLogs(page, FIXTURES_DAY, p.where)
    expect(await logCount(page), p.where).toBe(0)
  }

  await expect(async () => {
    await page.goto(`/${ORG}/runs`)
    await expect(page.locator(`tr[data-run="${DISPATCHER_RUN}"]`)).toBeVisible({ timeout: 1_000 })
  }).toPass({ timeout: 30_000 })
  await expect(page.locator(`tr[data-run="${FORGED_RUN}"]`)).toHaveCount(0)
  const forgedRun = await page.goto(`/${ORG}/runs/${FORGED_RUN}`)
  expect(forgedRun?.status()).toBe(404)

  await expect(async () => {
    const now = await agentOverview(page)
    expect(now.filtered - before.filtered).toBe(FILTERED)
    expect(now.received - before.received).toBeGreaterThanOrEqual(FILTERED + KEPT.length)
  }).toPass({ timeout: 30_000 })

  expect(errors).toEqual([])
})
