import { chromium, type Browser, type FullConfig } from '@playwright/test'
import { execFile, execFileSync } from 'node:child_process'
import { mkdirSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { promisify } from 'node:util'
import { identity, type Identity } from './data'

const consoleDir = resolve(__dirname, '../..')

// The records of workers 1 and up (data.ts), made as db/seeds.rb makes
// worker 0's: an Organization and a Project with a key, an admin (also an
// Instance admin) and a member.
const WORKER_RECORDS = `
  JSON.parse(ENV.fetch("E2E_IDENTITIES")).each do |w|
    org = Organization.find_or_create_by!(slug: w["org"]) { |o| o.name = "SolidTrace Org" }
    project = org.projects.find_or_create_by!(slug: w["project"]) { |p| p.name = "SolidTrace Project" }
    project.project_keys.first_or_create!
    admin, member = %w[admin member].map do |role|
      User.find_or_create_by!(email: w[role]["email"]) do |u|
        u.name = "#{role.capitalize} User"
        u.password = u.password_confirmation = w[role]["password"]
      end
    end
    admin.update!(instance_admin: true)
    OrganizationUser.find_or_create_by!(organization: org, user: admin, role: :admin)
    OrganizationUser.find_or_create_by!(organization: org, user: member, role: :member)
  end
`

// Makes sure the dev database has the records of each of the run's workers,
// signs each worker's admin in once and saves the session for its tests, and
// makes sure each worker's Project has Logs across the last 24 hours, sending
// dummy Logs through ingest when it has too few or an hour has none.
export default async function globalSetup(config: FullConfig) {
  const project = config.projects[0]
  const env = (more: Record<string, string> = {}) => ({ ...process.env, ...more })
  const rails = (args: string[], more: Record<string, string> = {}) =>
    execFileSync('mise', ['exec', '--', 'bin/rails', ...args], { cwd: consoleDir, env: env(more), stdio: 'pipe', timeout: 300_000 }).toString()

  const workers = Array.from({ length: config.workers }, (_, n) => identity(n))
  rails(['db:seed'])
  if (workers.length > 1) rails(['runner', WORKER_RECORDS], { E2E_IDENTITIES: JSON.stringify(workers.slice(1)) })

  const browser = await chromium.launch(project.use.launchOptions)
  const empty: string[] = []
  for (const worker of workers) {
    if (await signInAndCheckLogs(browser, project.use.baseURL!, worker)) empty.push(worker.project)
  }
  await browser.close()

  // dev:logs spends most of its time in Rails' boot and in ingest, so the Projects fill at once.
  await Promise.all(empty.map(slug => promisify(execFile)('mise', ['exec', '--', 'bin/rails', 'dev:logs'], {
    cwd: consoleDir, env: env({ PROJECT: slug, COUNT: '20000', HOURS: '24', SEED: '42' }), timeout: 300_000,
  })))
}

/** Signs the worker's admin in and saves the session. True when the worker's Project needs dummy Logs. */
async function signInAndCheckLogs(browser: Browser, baseURL: string, worker: Identity): Promise<boolean> {
  const statePath = resolve(consoleDir, worker.adminState)
  mkdirSync(dirname(statePath), { recursive: true })
  const page = await browser.newPage({ baseURL })
  await page.goto('/login')
  await page.fill('input[type=email]', worker.admin.email)
  await page.fill('input[type=password]', worker.admin.password)
  await page.click('button[type=submit]')
  await page.waitForURL(url => !url.pathname.startsWith('/login'), { timeout: 30_000 })

  // Flows chart the last 24 hours and zoom into its middle, so every hour
  // needs Logs, not only enough of them in all. The newest two hours may be
  // empty, so a run soon after a top-up doesn't send more.
  const hour = 3600 * 1000
  const to = new Date()
  const from = new Date(to.getTime() - 24 * hour)
  const params = new URLSearchParams({ query: '', from: from.toISOString(), to: to.toISOString(), bucket_seconds: '3600' })
  const histogram = await page.request.get(`/${worker.org}/projects/${worker.project}/logs/histogram?${params}`)
  const { count = 0, buckets = [] }: { count?: number, buckets?: { time: string, count: number }[] } =
    histogram.ok() ? await histogram.json() : {}
  const emptyHour = buckets.some(b => b.count === 0 && Date.parse(b.time) < to.getTime() - 2 * hour)

  await page.context().storageState({ path: statePath })
  await page.context().close()
  return count < 5_000 || emptyHour
}
