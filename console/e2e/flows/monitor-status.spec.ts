import { expect, test, type Page } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import { writeFileSync } from 'node:fs'
import { createServer, type Server } from 'node:http'
import type { AddressInfo } from 'node:net'
import { resolve } from 'node:path'
import { ORG } from '../support/data'
import { watchErrors } from '../support/screens'

// Monitor status: an admin chooses a Slack Integration, whose webhook is a
// local stub, as the Organization's Monitor status Integration. Two
// Monitors run. Then Console cannot reach EventStore's query port for 3
// minutes. The Monitors list shows "Not running since", and the stub gets
// one "not running" message that names both Monitors. When the port is
// back and the runs compare again, it gets one "running again" message.
// The Monitors' own Integration gets nothing.
//
// The stack must reach EventStore's query port through a switch that the
// flow can turn off and on: E2E_QUERY_PORT_SWITCH is a program that takes
// "off" or "on". Without it, the flow is skipped.

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
function rails(ruby: string, env: Record<string, string>): string {
  return execFileSync('mise', ['exec', '--', 'bin/rails', 'runner', ruby], {
    cwd: resolve(__dirname, '../..'), env: { ...process.env, ...env }, stdio: 'pipe', timeout: 120_000,
  }).toString().trim()
}

function queryPort(state: 'off' | 'on') {
  execFileSync(SWITCH!, [state], { stdio: 'pipe', timeout: 30_000 })
}

const SLACK_SETTINGS = '"notify_on_new_issue" => false, "notify_on_event_threshold" => false, "notify_on_assignment" => false'

let status: Stub
let own: Stub
const made = { status: '', own: '', monitors: '' }
const stamp = Date.now()
const names = [`Status A ${stamp}`, `Status B ${stamp}`]

test.beforeAll(async () => {
  if (!SWITCH) return
  status = await startSlackStub()
  own = await startSlackStub()
  made.status = rails(
    `print Organization.find_by!(slug: ENV["ORG"]).integrations.create!(provider: "slack", name: ENV["NAME"],
      settings: { "webhook_url" => ENV["URL"], ${SLACK_SETTINGS} }).id`,
    { ORG, NAME: `Status channel ${stamp}`, URL: status.url },
  )
  made.own = rails(
    `print Organization.find_by!(slug: ENV["ORG"]).integrations.create!(provider: "slack", name: ENV["NAME"],
      settings: { "webhook_url" => ENV["URL"], ${SLACK_SETTINGS} }).id`,
    { ORG, NAME: `Own channel ${stamp}`, URL: own.url },
  )
  // Two Monitors that run each minute and never fire.
  made.monitors = rails(
    `org = Organization.find_by!(slug: ENV["ORG"])
     project = org.projects.find_by!(slug: "solid-trace-project")
     ids = ENV["NAMES"].split("|").map do |name|
       m = org.monitors.new(project: project, kind: "threshold")
       m.save_with({ name: name, query: "['solid-trace-project']\\n| summarize count()", interval_minutes: 1, range_minutes: 5,
         operator: ">", threshold: 1e15 }, integration_ids: [ ENV["OWN"] ]) or abort(m.errors.full_messages.join(", "))
       m.id
     end
     print ids.join(",")`,
    { ORG, NAMES: names.join('|'), OWN: made.own },
  )
})

test.afterAll(async () => {
  if (!SWITCH) return
  queryPort('on')
  rails(
    `org = Organization.find_by!(slug: ENV["ORG"])
     org.update!(monitor_status_integration: nil) if org.monitor_status_integration_id.to_s == ENV["STATUS"]
     LogMonitor.where(id: ENV["MONITORS"].split(",")).destroy_all
     ids = [ ENV["STATUS"], ENV["OWN"] ].compact_blank
     Notification.where(integration_id: ids).delete_all
     Integration.where(id: ids).destroy_all`,
    { ORG, STATUS: made.status, OWN: made.own, MONITORS: made.monitors },
  )
  status?.server.close()
  own?.server.close()
})

const said = (stub: Stub, words: RegExp) => stub.bodies.filter(body => words.test(JSON.parse(body).text))

async function openMonitors(page: Page) {
  await page.goto(`/${ORG}/monitors`)
  await expect(page.getByRole('heading', { name: 'Monitors' })).toBeVisible()
}

test('the Monitor status Integration gets one "not running" and one "running again" message when EventStore\'s query port stops', async ({ page }, info) => {
  test.skip(!SWITCH, 'needs E2E_QUERY_PORT_SWITCH: a program that turns the stack\'s EventStore query port off and on')
  test.setTimeout(15 * 60_000)
  // While the query port is off, pages that read EventStore answer errors.
  const errors = watchErrors(page, [/status of 50\d/])

  // An admin chooses the Monitor status Integration.
  await page.goto(`/${ORG}/settings/organization`)
  const card = page.getByRole('region', { name: 'Monitor status' })
  const select = card.locator('[data-monitor-status-integration]')
  await select.click()
  await expect(page.getByRole('option', { name: 'None' })).toBeVisible()
  await expect(page.getByRole('option', { name: /Fixer|Factory/ })).toHaveCount(0)
  await page.getByRole('option', { name: `Status channel ${stamp} (Slack)` }).click()
  await expect(page.getByText('Organization settings updated')).toBeVisible()
  await expect(select).toHaveText(`Status channel ${stamp} (Slack)`)
  await page.screenshot({ path: info.outputPath('settings-monitor-status.png') })
  expect(rails('print Organization.find_by!(slug: ENV["ORG"]).monitor_status_integration_id', { ORG })).toBe(made.status)

  // Both Monitors run and compare first.
  const ran = () => rails('print LogMonitor.where(id: ENV["IDS"].split(",")).where.not(last_run_at: nil).where(last_error: nil).count',
    { IDS: made.monitors })
  await expect.poll(ran, { timeout: 150_000, intervals: [5000] }).toBe('2')

  // Console cannot reach EventStore's query port: each run fails.
  queryPort('off')
  const cutAt = Date.now()
  await expect.poll(async () => {
    await openMonitors(page)
    return page.locator('[data-monitor-not-running]').count()
  }, { timeout: 150_000, intervals: [10_000] }).toBeGreaterThanOrEqual(2)
  for (const name of names) {
    await expect(page.locator(`[data-monitor-row="${name}"] [data-monitor-not-running]`)).toHaveText(/^Not running since /)
  }
  await page.screenshot({ path: info.outputPath('monitors-not-running.png'), fullPage: true })

  await expect.poll(() => said(status, /not running/).length, { timeout: CUT_MS, intervals: [5000] }).toBe(1)
  const notRunning = said(status, /not running/)[0]
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
  for (const name of names) {
    await expect(page.locator(`[data-monitor-row="${name}"] [data-monitor-not-running]`)).toHaveCount(0)
  }
  expect(errors).toEqual([])
})
