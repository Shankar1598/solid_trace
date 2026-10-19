import { chromium, type FullConfig } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import { mkdirSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { ADMIN, ORG, PROJECT } from './data'

// Signs the seeded admin in once and saves the session for every test. Makes
// sure the dev database has the seed data and the seed Project has Logs across
// the last 24 hours, sending dummy Logs through ingest when it has too few or
// an hour has none.
export default async function globalSetup(config: FullConfig) {
  const project = config.projects[0]
  const baseURL = project.use.baseURL!
  const statePath = resolve(project.use.storageState as string)
  mkdirSync(dirname(statePath), { recursive: true })

  const consoleDir = resolve(__dirname, '../..')
  const rails = (args: string[], env: Record<string, string> = {}) =>
    execFileSync('mise', ['exec', '--', 'bin/rails', ...args], {
      cwd: consoleDir, env: { ...process.env, ...env }, stdio: 'pipe', timeout: 300_000,
    }).toString()

  rails(['db:seed'])

  const browser = await chromium.launch(project.use.launchOptions)
  const page = await browser.newPage({ baseURL })
  await page.goto('/login')
  await page.fill('input[type=email]', ADMIN.email)
  await page.fill('input[type=password]', ADMIN.password)
  await page.click('button[type=submit]')
  await page.waitForURL(url => !url.pathname.startsWith('/login'), { timeout: 30_000 })

  // Flows chart the last 24 hours and zoom into its middle, so every hour
  // needs Logs, not only enough of them in all. The newest two hours may be
  // empty, so a run soon after a top-up doesn't send more.
  const hour = 3600 * 1000
  const to = new Date()
  const from = new Date(to.getTime() - 24 * hour)
  const params = new URLSearchParams({ query: '', from: from.toISOString(), to: to.toISOString(), bucket_seconds: '3600' })
  const histogram = await page.request.get(`/${ORG}/projects/${PROJECT}/logs/histogram?${params}`)
  const { count = 0, buckets = [] }: { count?: number, buckets?: { time: string, count: number }[] } =
    histogram.ok() ? await histogram.json() : {}
  const emptyHour = buckets.some(b => b.count === 0 && Date.parse(b.time) < to.getTime() - 2 * hour)
  if (count < 5_000 || emptyHour) {
    rails(['dev:logs'], { PROJECT, COUNT: '20000', HOURS: '24', SEED: '42' })
  }

  await page.context().storageState({ path: statePath })
  await browser.close()
}
