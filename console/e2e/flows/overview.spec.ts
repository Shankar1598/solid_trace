import { test, expect } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import { resolve } from 'node:path'
import { ORG, PROJECT } from '../support/data'
import { watchErrors } from '../support/screens'

// The Overview: Logs sent to two Projects show in the band, the rates, the
// daily Charts and the Projects table. Each run makes its own second
// Project, and a quiet one whose last Log arrived two hours ago, and deletes
// them after.

const ingest = process.env.E2E_INGEST_URL ?? 'http://localhost:4000'
// The stack's ClickHouse, over HTTP, where the quiet Project's Ingest count
// is written back-dated: ingest counts what arrives by its own clock.
const clickhouse = process.env.E2E_CLICKHOUSE_URL ?? 'http://127.0.0.1:4201'
const clickhouseDatabase = process.env.E2E_CLICKHOUSE_DATABASE ?? 'solidtrace'
const second = `Second ${Date.now()}`
const secondSlug = second.toLowerCase().replace(' ', '-')
const quiet = `Quiet ${Date.now()}`
const quietSlug = quiet.toLowerCase().replace(' ', '-')

const runner = (code: string) => execFileSync('mise', ['exec', '--', 'bin/rails', 'runner', code], {
  cwd: resolve(__dirname, '../..'), stdio: 'pipe', timeout: 120_000,
}).toString().trim()

const keyOf = (slug: string) => runner(`puts Project.find_by!(slug: ${JSON.stringify(slug)}).project_keys.first.public_key`)

test.beforeAll(async ({ request }) => {
  const made = runner(`org = Organization.find_by!(slug: ${JSON.stringify(ORG)}); ` +
    [[second, secondSlug], [quiet, quietSlug]].map(([name, slug]) =>
      `p = Project.build_with_key(organization: org, name: ${JSON.stringify(name)}, slug: ${JSON.stringify(slug)}); p.save!; puts p.id`).join('; '))
  const quietId = Number(made.split('\n').at(-1))
  const twoHoursAgo = new Date(Date.now() - 2 * 3600_000).toISOString().slice(0, 19).replace('T', ' ')
  const written = await request.post(`${clickhouse}/?user=solidtrace&database=${clickhouseDatabase}`, {
    data: 'INSERT INTO log_ingest_counts (project_id, second, logs_received, bytes_received, bytes_on_wire, requests) ' +
      `VALUES (${quietId}, '${twoHoursAgo}', 1, 100, 100, 1)`,
  })
  expect(written.ok(), await written.text()).toBe(true)
})

test.afterAll(() => {
  runner(`Project.where(slug: ${JSON.stringify([secondSlug, quietSlug])}).destroy_all`)
})

test('Logs sent to two Projects show in the band and the rates, over Today and 7 days', async ({ page, request }) => {
  const errors = watchErrors(page)
  const now = new Date().toISOString()
  for (const [slug, n] of [[PROJECT, 3], [secondSlug, 2]] as const) {
    const logs = Array.from({ length: n }, (_, i) => ({ message: `overview flow ${i}`, time: now, service: 'overview-flow' }))
    const sent = await request.post(`${ingest}/api/logs`, { headers: { Authorization: `Bearer ${keyOf(slug)}` }, data: logs })
    expect(sent.ok()).toBe(true)
  }

  await page.goto(`/${ORG}/issues`)
  await page.getByRole('navigation', { name: 'Main' }).getByRole('link', { name: 'Overview' }).click()
  await expect(page).toHaveURL(new RegExp(`/${ORG}/overview$`))
  await expect(page.getByRole('heading', { name: 'Overview', level: 1 })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Today' })).toHaveAttribute('aria-pressed', 'true')

  // Ingest counts are written about once a second, and Log processing
  // stores the Logs about as often.
  const band = page.locator('[data-section="band"]')
  await expect(async () => {
    await page.reload()
    await expect(band.locator('[data-step="received"]')).toContainText('Logs')
    await expect(band.locator('[data-step="stored"]')).not.toContainText(/^Stored\s*0 B/)
  }).toPass({ timeout: 20_000 })
  await expect(band.getByText('Received', { exact: true })).toBeVisible()
  await expect(band.getByText('Stored', { exact: true })).toBeVisible()
  await expect(band.getByText(/× smaller/)).toBeVisible()
  await expect(band.getByText('Turned away as overloaded')).toBeVisible()

  const rates = page.locator('[data-section="rates"]')
  await expect(rates.locator('[data-rate="Logs per second"]')).toContainText(/peak at .* UTC/)
  await expect(rates.locator('[data-rate="Bytes per second"]')).toContainText(/\/s average/)
  await expect(rates.locator('[data-rate="Requests per second"]')).toContainText(/average/)
  await expect(rates.locator('[data-rate="last-five-minutes"]')).toContainText(/Logs per second/)

  // The daily Charts, each with its table, and the Projects: both that
  // sent Logs, and the quiet one marked as over an hour since its last.
  const daily = page.locator('[data-section="daily"]')
  for (const chart of ['logs', 'bytes', 'rates']) {
    await expect(daily.locator(`[data-chart="${chart}"] [data-chart-table] tbody tr`)).toHaveCount(1)
  }
  await expect(daily.locator('[data-chart="logs"] [data-chart-legend]')).toContainText(second)
  const projects = page.locator('[data-section="projects"]')
  for (const slug of [PROJECT, secondSlug]) {
    await expect(projects.locator(`[data-project="${slug}"]`)).toContainText(/ago/)
    await expect(projects.locator(`[data-project="${slug}"] [data-stale]`)).toHaveCount(0)
  }
  await expect(projects.locator(`[data-project="${quietSlug}"] [data-stale]`)).toContainText(/about 2 hours ago/)

  // Health: both Projects' processing; a read of the Logs explorer shows in
  // its Project's read times once ClickHouse writes its query_log (every
  // few seconds).
  const health = page.locator('[data-section="health"]')
  await expect(health.locator(`[data-health-project="${secondSlug}"]`)).toContainText(second)
  await expect(health.locator('[data-health="monitors"]')).toContainText('Monitors')
  await expect(health.locator('[data-health="issues"]')).toContainText(/Events? in the range/)
  await page.goto(`/${ORG}/projects/${PROJECT}/logs`)
  await expect(page.getByText('overview flow 0').first()).toBeVisible()
  await page.goto(`/${ORG}/overview`)
  await expect(async () => {
    await page.reload()
    await expect(health.locator(`[data-health-project="${PROJECT}"]`)).not.toContainText('No reads')
  }).toPass({ timeout: 30_000 })
  await expect(health.locator(`[data-health-project="${PROJECT}"]`)).toContainText(/Keeping up|Behind/)
  await expect(health.locator('[data-health="slowest-reads"]')).toContainText(/in .*UTC/)

  await page.getByRole('button', { name: '7 days' }).click()
  await expect(page).toHaveURL(new RegExp(`/${ORG}/overview\\?range=7d$`))
  await expect(page.getByRole('button', { name: '7 days' })).toHaveAttribute('aria-pressed', 'true')
  await expect(band.locator('[data-step="received"]')).toContainText('Logs')
  await expect(rates.locator('[data-rate="last-five-minutes"]')).toHaveCount(0)
  await expect(daily.locator('[data-chart="logs"] [data-chart-table] tbody tr')).toHaveCount(7)

  // A row opens its Project's Logs.
  await projects.locator(`[data-project="${secondSlug}"] td`).nth(1).click()
  await expect(page).toHaveURL(new RegExp(`/${ORG}/projects/${secondSlug}/logs`))
  expect(errors).toEqual([])
})

test('the command palette opens the Overview', async ({ page }) => {
  await page.goto(`/${ORG}/issues`)
  await page.keyboard.press('ControlOrMeta+k')
  const palette = page.getByRole('dialog', { name: 'Command palette' })
  await page.getByLabel('Search the palette').fill('Overview')
  await palette.getByRole('option', { name: /^Overview/ }).first().click()
  await expect(page).toHaveURL(new RegExp(`/${ORG}/overview$`))
})
