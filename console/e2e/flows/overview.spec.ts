import { test, expect } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import { resolve } from 'node:path'
import { ORG, PROJECT } from '../support/data'
import { watchErrors } from '../support/screens'

// The Overview: Logs sent to two Projects show in the band and the rates.
// Each run makes its own second Project, and deletes it after.

const ingest = process.env.E2E_INGEST_URL ?? 'http://localhost:4000'
const second = `Second ${Date.now()}`
const secondSlug = second.toLowerCase().replace(' ', '-')

const runner = (code: string) => execFileSync('mise', ['exec', '--', 'bin/rails', 'runner', code], {
  cwd: resolve(__dirname, '../..'), stdio: 'pipe', timeout: 120_000,
}).toString().trim()

const keyOf = (slug: string) => runner(`puts Project.find_by!(slug: ${JSON.stringify(slug)}).project_keys.first.public_key`)

test.beforeAll(() => {
  runner(`Project.build_with_key(organization: Organization.find_by!(slug: ${JSON.stringify(ORG)}), ` +
    `name: ${JSON.stringify(second)}, slug: ${JSON.stringify(secondSlug)}).save!`)
})

test.afterAll(() => {
  runner(`Project.find_by(slug: ${JSON.stringify(secondSlug)})&.destroy`)
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

  await page.getByRole('button', { name: '7 days' }).click()
  await expect(page).toHaveURL(new RegExp(`/${ORG}/overview\\?range=7d$`))
  await expect(page.getByRole('button', { name: '7 days' })).toHaveAttribute('aria-pressed', 'true')
  await expect(band.locator('[data-step="received"]')).toContainText('Logs')
  await expect(rates.locator('[data-rate="last-five-minutes"]')).toHaveCount(0)
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
