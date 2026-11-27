import { expect, test, type Page } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import { writeFileSync } from 'node:fs'
import { createServer, type Server } from 'node:http'
import type { AddressInfo } from 'node:net'
import { resolve } from 'node:path'
import { ADMIN } from '../support/data'
import { watchErrors } from '../support/screens'

// Monitor status, in an Organization of its own, so that no other Monitor
// is in its messages. An admin chooses a Slack Integration, whose webhook is
// a local stub, as the Organization's Monitor status Integration.
//
// The first test runs in the usual suite. A Monitor that does not run shows
// "Not running since" on the Monitors list, and the stub gets one "not
// running" message about it.
//
// The second test cuts EventStore's query port. Two Monitors run. Then
// Console cannot reach EventStore's query port for 3 minutes. The Monitors
// list shows "Not running since", and the stub gets one "not running"
// message that names both Monitors. When the port is back and the runs
// compare again, it gets one "running again" message. The Monitors' own
// Integration gets nothing. The stack must reach EventStore's query port
// through a switch that the flow can turn off and on: E2E_QUERY_PORT_SWITCH
// is a program that takes "off" or "on", such as e2e/query-port-switch
// (docs/agents/testing.md). Without it, that test is skipped.

const SWITCH = process.env.E2E_QUERY_PORT_SWITCH
const CUT_MS = 3 * 60_000

interface Stub {
  server: Server
  url: string
  bodies: string[]
}

/** A Slack incoming webhook on a free localhost port that records what it is sent. */
function startSlackStub(): Promise<Stub> {
  const bodies: string[] = []
  const server = createServer((request, response) => {
    let text = ''
    request.on('data', chunk => { text += chunk })
    request.on('end', () => {
      bodies.push(text)
      response.writeHead(200, { 'Content-Type': 'text/plain' })
      response.end('ok')
    })
  })
  return new Promise(done => server.listen(0, '127.0.0.1', () => {
    const { port } = server.address() as AddressInfo
    done({ server, url: `http://127.0.0.1:${port}/services/T000/B000/status`, bodies })
  }))
}

/** Runs Ruby in the dev Console, with env, and answers what it prints. */
function rails(ruby: string, env: Record<string, string> = {}): string {
  return execFileSync('mise', ['exec', '--', 'bin/rails', 'runner', ruby], {
    cwd: resolve(__dirname, '../..'), env: { ...process.env, ...env }, stdio: 'pipe', timeout: 120_000,
  }).toString().trim()
}

function queryPort(state: 'off' | 'on') {
  execFileSync(SWITCH!, [state], { stdio: 'pipe', timeout: 30_000 })
}

const SLACK_SETTINGS = '"notify_on_new_issue" => false, "notify_on_event_threshold" => false, "notify_on_assignment" => false'
const stamp = Date.now()
const ORG = `monitor-status-${stamp}`
const PROJECT = `status-shop-${stamp}`

let status: Stub
let own: Stub
const made = { status: '', own: '' }

/** Makes Monitors that run each minute and never fire, telling the own Integration. Answers their ids. */
function makeMonitors(names: string[]): string {
  return rails(
    `org = Organization.find_by!(slug: ENV["ORG"])
     project = org.projects.find_by!(slug: ENV["PROJECT"])
     ids = ENV["NAMES"].split("|").map do |name|
       m = org.monitors.new(project: project, kind: "threshold")
       m.save_with({ name: name, query: "['#{project.slug}']\\n| summarize count()", interval_minutes: 1, range_minutes: 5,
         operator: ">", threshold: 1e15 }, integration_ids: [ ENV["OWN"] ]) or abort(m.errors.full_messages.join(", "))
       m.id
     end
     print ids.join(",")`,
    { ORG, PROJECT, NAMES: names.join('|'), OWN: made.own },
  )
}

test.beforeAll(async () => {
  status = await startSlackStub()
  own = await startSlackStub()
  const ids = rails(
    `user = User.find_by!(email: ENV["ADMIN"])
     org = Organization.create!(name: "Monitor status #{ENV["ORG"]}", slug: ENV["ORG"])
     org.add_admin(user)
     org.projects.create!(name: "Status shop", slug: ENV["PROJECT"])
     ids = [ ENV["STATUS_URL"], ENV["OWN_URL"] ].zip([ "Status channel", "Own channel" ]).map do |url, name|
       org.integrations.create!(provider: "slack", name: name, settings: { "webhook_url" => url, ${SLACK_SETTINGS} }).id
     end
     print ids.join(",")`,
    { ADMIN: ADMIN.email, ORG, PROJECT, STATUS_URL: status.url, OWN_URL: own.url },
  )
  ;[made.status, made.own] = ids.split(',')
})

test.afterAll(async () => {
  if (SWITCH) queryPort('on')
  rails(
    `Organization.where(slug: ENV["ORG"]).each do |org|
       org.update!(monitor_status_integration: nil)
       Notification.where(integration_id: org.integrations.select(:id)).delete_all
       OrganizationUser.where(organization: org).delete_all
       org.destroy!
     end`,
    { ORG },
  )
  status?.server.close()
  own?.server.close()
})

const texts = (stub: Stub) => stub.bodies.map(body => JSON.parse(body).text as string)
const said = (stub: Stub, words: RegExp) => stub.bodies.filter(body => words.test(JSON.parse(body).text))

async function openMonitors(page: Page) {
  await page.goto(`/${ORG}/monitors`)
  await expect(page.getByRole('heading', { name: 'Monitors' })).toBeVisible()
}

/** An admin chooses the stub's Slack Integration as the Monitor status Integration, and it is saved. */
async function chooseStatusIntegration(page: Page) {
  await page.goto(`/${ORG}/settings/organization`)
  const card = page.getByRole('region', { name: 'Monitor status' })
  await expect(card).toContainText('do not run for 2 minutes')
  const select = card.locator('[data-monitor-status-integration]')
  await select.click()
  await expect(page.getByRole('option', { name: 'None' })).toBeVisible()
  await expect(page.getByRole('option', { name: /Factory/ })).toHaveCount(0)
  await page.getByRole('option', { name: 'Status channel (Slack)' }).click()
  await expect(page.getByText('Organization settings updated')).toBeVisible()
  await expect(select).toHaveText('Status channel (Slack)')
  expect(rails('print Organization.find_by!(slug: ENV["ORG"]).monitor_status_integration_id', { ORG })).toBe(made.status)
  await page.reload()
  await expect(page.getByRole('region', { name: 'Monitor status' }).locator('[data-monitor-status-integration]'))
    .toHaveText('Status channel (Slack)')
}

test('an admin chooses the Monitor status Integration, and a Monitor that does not run shows "Not running since" and is told', async ({ page }, info) => {
  // The check runs at second 30 of each minute, and the tick 10 s after it.
  test.setTimeout(3 * 60_000)
  await chooseStatusIntegration(page)
  await page.screenshot({ path: info.outputPath('settings-monitor-status.png') })

  // A Monitor whose runs failed 3 minutes ago and that the scheduler does not run again in this test.
  const name = `Stopped ${stamp}`
  const id = makeMonitors([name])
  rails(
    `LogMonitor.find(ENV["ID"]).update_columns(next_run_at: 1.day.from_now, last_run_at: 3.minutes.ago,
       not_running_since: 3.minutes.ago, last_error: "EventStore connection error: flow")`,
    { ID: id },
  )

  await openMonitors(page)
  await expect(page.locator(`[data-monitor-row="${name}"] [data-monitor-not-running]`)).toHaveText(/^Not running since /)
  await page.screenshot({ path: info.outputPath('monitors-not-running.png'), fullPage: true })

  await expect.poll(() => said(status, /is not running/).length, { timeout: 120_000, intervals: [5000] }).toBe(1)
  expect(texts(status)).toEqual([`🚨 ${name} is not running`])
  expect(status.bodies[0]).toContain('EventStore connection error: flow')
  expect(status.bodies[0]).toContain('Open the Monitors')
  expect(own.bodies).toEqual([])
  // With None chosen first, the deleted Monitor sends nothing more.
  rails('Organization.find_by!(slug: ENV["ORG"]).update!(monitor_status_integration: nil); LogMonitor.find(ENV["ID"]).destroy!',
    { ORG, ID: id })
})

test('the Monitor status Integration gets one "not running" and one "running again" message when EventStore\'s query port stops', async ({ page }, info) => {
  test.skip(!SWITCH, 'needs E2E_QUERY_PORT_SWITCH: a program that turns the stack\'s EventStore query port off and on')
  test.setTimeout(15 * 60_000)
  // While the query port is off, pages that read EventStore answer errors.
  const errors = watchErrors(page, [/status of 50\d/])
  await chooseStatusIntegration(page)
  status.bodies.length = 0

  const names = [`Status A ${stamp}`, `Status B ${stamp}`]
  const ids = makeMonitors(names)

  // Both Monitors run and compare first.
  const ran = () => rails('print LogMonitor.where(id: ENV["IDS"].split(",")).where.not(last_run_at: nil).where(last_error: nil).count',
    { IDS: ids })
  await expect.poll(ran, { timeout: 150_000, intervals: [5000] }).toBe('2')

  // Console cannot reach EventStore's query port: each run fails.
  queryPort('off')
  const cutAt = Date.now()
  const notRunningRows = page.locator(names.map(name => `[data-monitor-row="${name}"] [data-monitor-not-running]`).join(', '))
  await expect.poll(async () => {
    await openMonitors(page)
    return notRunningRows.count()
  }, { timeout: 150_000, intervals: [10_000] }).toBe(2)
  for (const name of names) {
    await expect(page.locator(`[data-monitor-row="${name}"] [data-monitor-not-running]`)).toHaveText(/^Not running since /)
  }
  await page.screenshot({ path: info.outputPath('monitors-not-running.png'), fullPage: true })

  await expect.poll(() => said(status, /not running/).length, { timeout: CUT_MS, intervals: [5000] }).toBe(1)
  const notRunning = said(status, /not running/)[0]
  expect(JSON.parse(notRunning).text).toBe('🚨 All 2 Monitors are not running')
  for (const name of names) expect(notRunning).toContain(name)
  expect(notRunning).toContain('EventStore connection error')

  // The port comes back after 3 minutes. The runs compare again.
  await page.waitForTimeout(Math.max(0, CUT_MS - (Date.now() - cutAt)))
  queryPort('on')
  await expect.poll(() => said(status, /running again/).length, { timeout: 240_000, intervals: [5000] }).toBe(1)
  for (const name of names) expect(said(status, /running again/)[0]).toContain(name)

  // A later tick sends nothing more, and the Monitors' own Integration got nothing.
  await page.waitForTimeout(75_000)
  writeFileSync(info.outputPath('status-messages.json'), JSON.stringify(status.bodies.map(body => JSON.parse(body)), null, 2))
  expect(status.bodies.length).toBe(2)
  expect(own.bodies).toEqual([])
  await openMonitors(page)
  await expect(notRunningRows).toHaveCount(0)
  expect(errors).toEqual([])
})
