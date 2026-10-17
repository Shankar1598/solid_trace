import { expect, test, type Page } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import { createServer, type Server } from 'node:http'
import type { AddressInfo } from 'node:net'
import { resolve } from 'node:path'
import { ORG } from '../support/data'
import { last24h, openLogs, runBy } from '../support/logs'
import { shoot, useTheme, watchErrors } from '../support/screens'

// Monitors: a member charts a Query in the explorer, presses Create monitor,
// sets a threshold the count passes and picks a Slack Integration whose
// webhook is a local stub. Run now fires the Monitor and the stub gets the
// message; a higher threshold and Run now resolve it. Then the Monitor is
// turned off and on, and deleted.

const SOURCE = "['solid-trace-project']"

interface Stub {
  server: Server
  url: string
  /** The JSON bodies posted to the webhook, in order. */
  bodies: unknown[]
}

/** A Slack incoming webhook on a free localhost port that records what it is sent. */
function startSlackStub(): Promise<Stub> {
  const bodies: unknown[] = []
  const server = createServer((request, response) => {
    let text = ''
    request.on('data', chunk => { text += chunk })
    request.on('end', () => {
      try { bodies.push(JSON.parse(text)) } catch { bodies.push(text) }
      response.writeHead(200, { 'Content-Type': 'text/plain' })
      response.end('ok')
    })
  })
  return new Promise(done => server.listen(0, '127.0.0.1', () => {
    const { port } = server.address() as AddressInfo
    done({ server, url: `http://127.0.0.1:${port}/services/T000/B000/flow`, bodies })
  }))
}

/** Runs Ruby in the dev Console, with env, and answers what it prints. */
function rails(ruby: string, env: Record<string, string>): string {
  return execFileSync('mise', ['exec', '--', 'bin/rails', 'runner', ruby], {
    cwd: resolve(__dirname, '../..'), env: { ...process.env, ...env }, stdio: 'pipe', timeout: 120_000,
  }).toString().trim()
}

let stub: Stub
const integration = { id: '', name: `Flow Slack ${Date.now()}` }

test.beforeAll(async () => {
  stub = await startSlackStub()
  // Monitor messages only: the Issue rules are off, so new Issues never post to the stub.
  integration.id = rails(
    `print Organization.find_by!(slug: ENV["ORG"]).integrations.create!(provider: "slack", name: ENV["NAME"], settings: {
      "webhook_url" => ENV["URL"], "notify_on_new_issue" => false, "notify_on_event_threshold" => false, "notify_on_assignment" => false
    }).id`,
    { ORG, NAME: integration.name, URL: stub.url },
  )
})

test.afterAll(async () => {
  if (integration.id) {
    rails('Notification.where(integration_id: ENV["ID"]).delete_all; Integration.find_by(id: ENV["ID"])&.destroy!', { ID: integration.id })
  }
  stub?.server.close()
})

const status = (page: Page) => page.locator('[data-monitor-status]').first()

test('a member makes a Threshold monitor from the explorer; Run now fires it into Slack, and a higher threshold resolves it', async ({ page }) => {
  test.setTimeout(150_000)
  await useTheme(page, test.info())
  const errors = watchErrors(page)
  const name = `Flow monitor ${Date.now()}`

  // Create monitor, beside Add to dashboard, starts from the Project, the Query that ran and its range.
  await openLogs(page, last24h('query'))
  await page.getByLabel('Query', { exact: true }).fill(`${SOURCE}\n| summarize count() by bin_auto(time)`)
  await runBy(page, () => page.getByLabel('Query', { exact: true }).press('Control+Enter'))
  await expect(page.getByRole('button', { name: 'Add to dashboard' })).toBeVisible()
  await page.getByRole('link', { name: 'Create monitor' }).click()

  await expect(page.getByRole('heading', { name: 'New monitor' })).toBeVisible()
  await expect(page.getByRole('link', { name: 'Monitors' }).first()).toHaveClass(/bg-secondary/)
  await expect(page.getByRole('link', { name: 'Logs' })).not.toHaveClass(/bg-secondary/)
  await expect(page.getByText(/time bins were taken out/)).toBeVisible()
  await expect(page.getByRole('group', { name: 'Kind' }).getByRole('button', { name: 'Threshold' })).toHaveAttribute('aria-pressed', 'true')
  await expect(page.getByLabel('Over the last (minutes)')).toHaveValue('1440')
  await expect(page.getByRole('combobox', { name: 'Project' }).first()).toHaveText('SolidTrace Project')

  // A save without a threshold comes back with the error beside the field.
  await page.getByLabel('Name').fill(name)
  await page.getByRole('button', { name: 'Create monitor' }).click()
  await expect(page.getByText('Threshold is not a number')).toBeVisible()

  // The preview draws the threshold over 24 hours and says what a run would fire now.
  await page.getByLabel('Run every (minutes)').fill('60')
  await page.getByLabel('Threshold').fill('0')
  await page.getByRole('group', { name: 'Integrations' }).getByRole('checkbox', { name: integration.name }).click()
  await expect(page.locator('[data-monitor-groups]')).toContainText('1 group would fire now.')
  await expect(page.locator('[data-monitor-preview]').getByText('threshold 0')).toBeVisible()
  await shoot(page, 'monitor-new', { fullPage: true })
  await page.getByRole('button', { name: 'Create monitor' }).click()

  // Run now fires it: the page shows the firing group and the run.
  await expect(page.getByRole('heading', { name })).toBeVisible()
  await expect(page.getByText(`Tells ${integration.name} (slack).`)).toBeVisible()
  await page.getByRole('button', { name: 'Run now' }).click()
  await expect(page.getByText('Ran the Monitor: 1 group firing.')).toBeVisible()
  await expect(status(page)).toHaveText('Firing')
  await expect(page.getByRole('table', { name: 'Groups' }).locator('[data-group-status="firing"]')).toHaveCount(1)
  await expect(page.getByRole('table', { name: 'Runs' })).toContainText('1 group firing')

  // The Slack stub gets the firing message, about this Monitor. (The scheduler may run a new Monitor
  // at the same moment as Run now, so this doesn't count them; MonitorEvaluation's tests do.)
  const about = (title: string) => stub.bodies.filter(b => JSON.stringify(b).includes(`${title}: ${name}`))
  await expect.poll(() => about('Firing').length, { timeout: 60_000, intervals: [1000] }).toBeGreaterThan(0)
  await shoot(page, 'monitor-show', { fullPage: true })

  // The list shows it firing; the palette finds it.
  await page.getByRole('link', { name: 'Monitors' }).first().click()
  const row = page.locator(`[data-monitor-row="${name}"]`)
  await expect(row.locator('[data-monitor-status]')).toHaveText('Firing')
  await expect(row).toContainText('Threshold')
  await page.keyboard.press('Control+k')
  const palette = page.getByRole('dialog', { name: 'Command palette' })
  await page.getByLabel('Search the palette').fill(name)
  await expect(palette.getByRole('option', { name: new RegExp(name) })).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(palette).toHaveCount(0)
  await row.getByRole('link', { name }).click()

  // A higher threshold, then Run now: the group resolves.
  await page.getByRole('link', { name: 'Edit' }).click()
  await expect(page.getByRole('heading', { name: `Edit ${name}` })).toBeVisible()
  await page.getByLabel('Threshold').fill('1000000000')
  await expect(page.locator('[data-monitor-groups]')).toContainText('Nothing would fire now.')
  await page.getByRole('button', { name: 'Save monitor' }).click()
  await expect(page.getByRole('heading', { name })).toBeVisible()
  await page.getByRole('button', { name: 'Run now' }).click()
  await expect(page.getByText('Ran the Monitor: nothing firing.')).toBeVisible()
  await expect(status(page)).toHaveText('OK')
  await expect(page.getByRole('table', { name: 'Groups' }).locator('[data-group-status="ok"]')).toHaveCount(1)
  await expect(page.getByRole('table', { name: 'Runs' }).locator('tbody tr').first()).toContainText('Nothing firing')

  // Off and on again; Run now is offered only while it is on.
  await page.getByRole('button', { name: 'Turn off' }).click()
  await expect(status(page)).toHaveText('Off')
  await expect(page.getByRole('button', { name: 'Run now' })).toHaveCount(0)
  await page.getByRole('button', { name: 'Turn on' }).click()
  await expect(status(page)).toHaveText('OK')

  // Delete asks first.
  await page.getByRole('button', { name: 'Delete' }).click()
  await page.getByRole('alertdialog').getByRole('button', { name: 'Delete monitor' }).click()
  await expect(page.getByRole('heading', { name: 'Monitors', exact: true })).toBeVisible()
  await expect(page.locator(`[data-monitor-row="${name}"]`)).toHaveCount(0)

  expect(errors).toEqual([])
})
