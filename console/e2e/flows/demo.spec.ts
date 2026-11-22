import { test, expect } from '@playwright/test'
import { execFileSync, spawn, type ChildProcessWithoutNullStreams } from 'node:child_process'
import { existsSync, mkdirSync, rmSync, writeFileSync } from 'node:fs'
import { join, resolve } from 'node:path'
import { ORG, PROJECT } from '../support/data'
import { watchErrors } from '../support/screens'

// The factory demo (bin/factory-demo): the flow runs `demo up` against this
// stack's Console and EventStore, as a person runs it against the dev
// stack. It checks the Runs list, one Run page, the Stop button, the
// end with Ctrl-C (the settings are put back, the Runs stay), a second
// `demo up`, and `demo down`.

const INGEST_URL = process.env.E2E_INGEST_URL ?? 'http://localhost:4000'

const consoleDir = resolve(__dirname, '../..')
const work = join(consoleDir, 'tmp', `e2e-demo-${Date.now().toString(36)}`)
const demoBin = join(work, 'factory-demo')

const rails = (code: string) => execFileSync('mise', ['exec', '--', 'bin/rails', 'runner', code], {
  cwd: consoleDir, stdio: 'pipe', timeout: 120_000,
}).toString().trim()

/** The settings and tokens that the demo changes, as they are now. */
const demoSettings = (): { console_url: string; dispatcher_url: string; tokens: number } => JSON.parse(rails(`
  settings = InstanceSetting.current
  print({ console_url: settings.console_url.to_s, dispatcher_url: settings.dispatcher_url.to_s,
          tokens: ApiToken.where("name LIKE ?", "Factory demo:%").count }.to_json)
`))

type Demo = { process: ChildProcessWithoutNullStreams; out: () => string; exited: Promise<number | null> }

let origin = ''
let before: ReturnType<typeof demoSettings>
let demo: Demo | undefined

function startDemo(command: 'up' | 'down'): Demo {
  const child = spawn(demoBin, ['demo', command, '--console-dir', consoleDir, '--console-url', origin, '--ingest-url', INGEST_URL])
  let out = ''
  child.stdout.on('data', d => { out += d })
  child.stderr.on('data', d => { out += d })
  const exited = new Promise<number | null>(done => child.on('exit', code => done(code)))
  return { process: child, out: () => out, exited }
}

/** Waits until the demo's Runs run, and gives the ids of its Runs in order. */
async function ready(d: Demo): Promise<string[]> {
  await expect.poll(() => d.out(), { message: () => `the demo is ready:\n${d.out()}`, timeout: 300_000, intervals: [500] })
    .toContain('The demo runs.')
  const runs = d.out().split('The Runs:\n')[1].split('\n\n')[0]
  const ids = [...runs.matchAll(/^ {2}(\S+) {2}\S.*$/gm)].map(m => m[1])
  expect(ids, d.out()).toHaveLength(6)
  return ids
}

test.describe.configure({ mode: 'serial' })

test.beforeAll(async ({ baseURL }) => {
  test.setTimeout(420_000)
  origin = new URL(baseURL!).origin
  before = demoSettings()
  rmSync(work, { recursive: true, force: true })
  mkdirSync(work, { recursive: true })
  execFileSync('mise', ['exec', '--', 'go', 'build', '-o', demoBin, './internal/flowstack'], {
    cwd: resolve(consoleDir, '../factory'), stdio: 'pipe', timeout: 240_000,
  })
})

test.afterAll(async () => {
  // A demo that the flow left running: stop it, then 'demo down' puts back
  // what it did not.
  if (demo && demo.process.exitCode === null) {
    demo.process.kill('SIGTERM')
    await Promise.race([demo.exited, new Promise(done => setTimeout(done, 120_000))])
    demo.process.kill('SIGKILL')
  }
  if (existsSync(join(consoleDir, 'tmp', 'factory-demo.json'))) await startDemo('down').exited
  if (demo) writeFileSync(join(consoleDir, 'tmp', 'e2e-demo.log'), demo.out())
  rmSync(work, { recursive: true, force: true })
})

let runs: string[] = []

test('the demo starts six Runs; the Runs list and a Run page show them', async ({ page }) => {
  test.setTimeout(420_000)
  const errors = watchErrors(page)
  demo = startDemo('up')
  runs = await ready(demo)
  expect(demo.out()).toContain(`${origin}/${ORG}/runs`)
  expect(demo.out()).toContain(`/${ORG}/projects/${PROJECT}`)

  // Each Run has its state in the Runs list, from its run events.
  const states = ['Done', 'Done', 'Waiting on a person', 'Failed', 'Stopped', 'Running']
  await expect(async () => {
    await page.goto(`/${ORG}/runs`)
    for (const [i, id] of runs.entries()) {
      await expect(page.locator(`tr[data-run="${id}"]`)).toContainText(states[i], { timeout: 1_000 })
    }
  }).toPass({ timeout: 90_000, intervals: [1_000, 2_000] })
  await page.screenshot({ path: join(consoleDir, 'tmp', 'demo-runs.png') })

  // The first Run: a PR, two checks that passed, one Evidence file and its Agent session.
  await page.goto(`/${ORG}/runs/${runs[0]}`)
  await expect(page.getByRole('link', { name: /\/pulls?\/|pull-requests/ }).first()).toBeVisible()
  await expect(page.locator('[data-section="report"] [data-check]')).toHaveCount(2)
  await expect(page.locator('[data-section="report"] [data-flag="no-evidence"]')).toHaveCount(0)
  await expect(page.getByRole('link', { name: 'evidence/cart-total.txt' })).toBeVisible()
  await expect(page.locator('[data-section="events"] [data-event="pr_opened"]')).toHaveCount(1)
  await expect(page.locator('[data-section="executions"] [data-execution]')).toHaveCount(1)
  // The demo merged its PR on the fake Forge: the Run is closed.
  await expect(page.getByText('Closed', { exact: true }).first()).toBeVisible()
  await page.screenshot({ path: join(consoleDir, 'tmp', 'demo-run.png'), fullPage: true })
  const session = page.locator('dl a[href*="/agent_sessions/"]').first()
  await expect(session).toBeVisible()
  await session.click()
  await expect(page).toHaveURL(/\/agent_sessions\//)

  // The second Run has no Evidence; the third has its question.
  await page.goto(`/${ORG}/runs/${runs[1]}`)
  await expect(page.locator('[data-flag="no-evidence"]')).toBeVisible()
  await page.goto(`/${ORG}/runs/${runs[2]}`)
  await expect(page.getByText('buyer', { exact: false }).first()).toBeVisible()
  expect(errors).toEqual([])
})

test('the long Run stops with the Stop button in Console', async ({ page }) => {
  test.setTimeout(120_000)
  const errors = watchErrors(page)
  await page.goto(`/${ORG}/runs/${runs[5]}`)
  page.once('dialog', dialog => dialog.accept())
  await page.locator('[data-action="stop-run"]').click()
  await expect(async () => {
    await page.reload()
    await expect(page.locator('[data-section="events"] [data-event="execution_ended"]')).toHaveCount(1, { timeout: 1_000 })
  }).toPass({ timeout: 60_000, intervals: [1_000, 2_000] })
  await expect(page.getByText('Stopped', { exact: true }).first()).toBeVisible()
  expect(errors).toEqual([])
})

test('Ctrl-C puts back the settings and the Runs stay; a second up adds new Runs; down stops it', async ({ page }) => {
  test.setTimeout(600_000)
  const first = demo!
  expect(demoSettings().dispatcher_url).not.toBe(before.dispatcher_url)
  expect(demoSettings().tokens).toBe(before.tokens + 2)
  first.process.kill('SIGINT')
  expect(await first.exited, first.out()).toBe(0)
  expect(first.out()).toContain('The Runs and their Agent sessions stay in SolidTrace.')
  const after = demoSettings()
  expect(after).toEqual(before)

  // The Runs stay in the Runs list.
  await page.goto(`/${ORG}/runs`)
  for (const id of runs) await expect(page.locator(`tr[data-run="${id}"]`)).toBeVisible()

  // The second up does not fail, and adds six Runs.
  demo = startDemo('up')
  const second = await ready(demo)
  expect(second.filter(id => runs.includes(id))).toEqual([])
  await expect(async () => {
    await page.goto(`/${ORG}/runs`)
    for (const id of [...runs, ...second]) await expect(page.locator(`tr[data-run="${id}"]`)).toBeVisible({ timeout: 1_000 })
  }).toPass({ timeout: 60_000, intervals: [1_000, 2_000] })

  // 'demo down' from another process stops the demo and puts back the settings.
  const down = startDemo('down')
  expect(await down.exited, down.out()).toBe(0)
  expect(await demo.exited, demo.out()).toBe(0)
  expect(demoSettings()).toEqual(before)
  await page.goto(`/${ORG}/runs`)
  for (const id of second) await expect(page.locator(`tr[data-run="${id}"]`)).toBeVisible()
})
