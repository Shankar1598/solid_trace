import { test, expect, type Page } from '@playwright/test'
import { execFileSync, spawn, type ChildProcessWithoutNullStreams } from 'node:child_process'
import { existsSync, mkdirSync, rmSync, writeFileSync } from 'node:fs'
import { homedir } from 'node:os'
import { join, resolve } from 'node:path'
import { ADMIN, ADMIN_STATE, ORG, PROJECT } from '../support/data'

// A screenshot pass over the factory pages, to look at by eye (the UI
// screenshot pass of the deferred work). It starts the factory demo (six
// Runs: done, done with no Evidence, waiting on a person, failed, stopped,
// running) against this stack, adds two Runs from run events that the demo
// cannot make (a taken-over Run, and a Run with a lost Execution), and
// shoots each factory page at desktop (1440 px) and phone (390 px) width, in
// light and dark. The screenshots go outside the repo, to
// UI_PASS_DIR (default ~/.cache/solid-trace/factory-demo/screens/ui-pass).
// Beside each shot, the flow checks the page for sideways overflow, boxes
// that scroll inside on a phone, and raw values ("undefined", "null",
// "None") in the text, and writes what it finds to problems.txt there.
// A full `bun run e2e` leaves it out (playwright.config.ts, onRequest). Run it
// alone: bun run e2e e2e/flows/ui-pass.spec.ts (through run-flow.sh).

const INGEST_URL = process.env.E2E_INGEST_URL ?? 'http://localhost:4000'
const OUT = process.env.UI_PASS_DIR ?? join(homedir(), '.cache/solid-trace/factory-demo/screens/ui-pass')
const consoleDir = resolve(__dirname, '../..')
const RUN = Date.now().toString(36)
const work = join(consoleDir, 'tmp', `e2e-ui-pass-${RUN}`)
const demoBin = join(work, 'factory-demo')

const rails = (code: string) => execFileSync('mise', ['exec', '--', 'bin/rails', 'runner', code], {
  cwd: consoleDir, stdio: 'pipe', timeout: 120_000,
}).toString().trim()

type Demo = { process: ChildProcessWithoutNullStreams; out: () => string; exited: Promise<number | null> }

function startDemo(command: 'up' | 'down', origin: string): Demo {
  const child = spawn(demoBin, ['demo', command, '--console-dir', consoleDir, '--console-url', origin, '--ingest-url', INGEST_URL])
  let out = ''
  child.stdout.on('data', d => { out += d })
  child.stderr.on('data', d => { out += d })
  const exited = new Promise<number | null>(done => child.on('exit', code => done(code)))
  return { process: child, out: () => out, exited }
}

const SIZES = [
  { name: 'desktop', viewport: { width: 1440, height: 900 } },
  { name: 'phone', viewport: { width: 390, height: 844 } },
]
const THEMES = ['light', 'dark'] as const

type Screen = { name: string; go: (page: Page) => Promise<void>; element?: string }

let origin = ''
let demo: Demo | undefined
let runs: string[] = []
let takenOverRun = ''
let lostRun = ''
let eventToken: { id: number; secret: string } | undefined
const problems: string[] = []

/** One run event, as the Dispatcher sends it (see factory/dispatcher/events.go recordEvent). */
function runEvent(id: string, seq: number, at: Date, kind: string, state: string, details: Record<string, unknown> = {}) {
  return {
    time: at.toISOString(), level: 'info', message: `Run ${id}: ${kind}`,
    'run.id': id, 'run.seq': seq, 'event.id': `${id}-${seq}`, 'event.kind': kind,
    'run.state': state, 'run.reason': '', 'run.trigger': 'person', 'run.title': '', 'run.executions': 1,
    'run.pr.url': '', 'run.pr.outcome': '', 'run.flags': [], 'run.base_branch': 'main',
    'factory.project': PROJECT, repository: `bitbucket.org/example/${PROJECT}`, folder: '',
    'actor.kind': 'person', 'actor.id': '7', 'actor.name': 'Ana Admin',
    ...details,
    // The Run's starter, whoever caused this event.
    'run.started_by.kind': 'person', 'run.started_by.id': '7', 'run.started_by.name': 'Ana Admin',
  }
}

async function sendEvents(events: object[]) {
  const resp = await fetch(`${INGEST_URL}/api/agent/v1/run-events`, {
    method: 'POST', headers: { Authorization: `Bearer ${eventToken!.secret}`, 'Content-Type': 'application/json' },
    body: JSON.stringify(events),
  })
  expect(resp.status, await resp.clone().text()).toBe(200)
}

test.describe.configure({ mode: 'serial' })

test.beforeAll(async ({ baseURL }) => {
  test.setTimeout(420_000)
  origin = new URL(baseURL!).origin
  rmSync(work, { recursive: true, force: true })
  mkdirSync(work, { recursive: true })
  mkdirSync(OUT, { recursive: true })
  execFileSync('mise', ['exec', '--', 'go', 'build', '-o', demoBin, './internal/flowstack'], {
    cwd: resolve(consoleDir, '../factory'), stdio: 'pipe', timeout: 240_000,
  })
})

test.afterAll(async () => {
  if (demo && demo.process.exitCode === null) {
    demo.process.kill('SIGINT')
    await Promise.race([demo.exited, new Promise(done => setTimeout(done, 120_000))])
    demo.process.kill('SIGKILL')
  }
  if (existsSync(join(consoleDir, 'tmp', 'factory-demo.json'))) await startDemo('down', origin).exited
  if (eventToken) rails(`ApiToken.where(id: ${eventToken.id}).destroy_all`)
  rmSync(work, { recursive: true, force: true })
  mkdirSync(OUT, { recursive: true })
  writeFileSync(join(OUT, 'problems.txt'), problems.join('\n') + '\n')
})

test('the demo and two more Runs are up', async () => {
  test.setTimeout(420_000)
  demo = startDemo('up', origin)
  await expect.poll(() => demo!.out(), { message: () => `the demo is ready:\n${demo!.out()}`, timeout: 300_000, intervals: [500] })
    .toContain('The demo runs.')
  const list = demo.out().split('The Runs:\n')[1].split('\n\n')[0]
  runs = [...list.matchAll(/^ {2}(\S+) {2}\S.*$/gm)].map(m => m[1])
  expect(runs, demo.out()).toHaveLength(6)

  // The two Runs that the demo does not make, from run events.
  const made = JSON.parse(rails(`
    org = Organization.find_by!(slug: ${JSON.stringify(ORG)})
    admin = User.find_by!(email: ${JSON.stringify(ADMIN.email)})
    token, secret = ApiToken.issue!(user: admin, name: "UI pass events ${RUN}", scopes: Oauth::Scopes::DISPATCHER, organization_id: org.id)
    print({ id: token.id, secret: secret }.to_json)
  `))
  eventToken = made
  const ago = (minutes: number) => new Date(Date.now() - minutes * 60_000)
  const dee = { 'actor.kind': 'person', 'actor.id': '12', 'actor.name': 'Dee Member' }
  const dispatcher = { 'actor.kind': 'dispatcher', 'actor.id': '', 'actor.name': 'Dispatcher' }
  const vm = 'agent-machine-1'
  const attach = 'docker exec -i -u engineer factory-local-agent-machine-1 sudo -u agent hc connect'

  takenOverRun = `uipass-taken-${RUN}`
  const t = takenOverRun
  const title = { 'run.title': 'Fix the shipping rate for the island zones' }
  await sendEvents([
    runEvent(t, 1, ago(40), 'created', 'queued', title),
    runEvent(t, 2, ago(40), 'queued', 'queued', title),
    runEvent(t, 3, ago(39), 'execution_started', 'running', { ...title, 'execution.id': 'x1', 'execution.account': 'team-2', 'execution.vm': vm }),
    runEvent(t, 4, ago(20), 'execution_ended', 'running', { ...title, 'execution.id': 'x1', 'execution.vm': vm, 'execution.reason': 'taken over', 'execution.exit_code': 130 }),
    runEvent(t, 5, ago(20), 'take_over', 'taken_over', { ...title, ...dee, 'execution.id': 'x2', 'take_over.from_execution': 'x1', 'execution.vm': vm, 'execution.attach': attach }),
    runEvent(t, 6, ago(19), 'execution_started', 'taken_over', { ...title, ...dee, 'execution.id': 'x2', 'execution.form': 'interactive', 'execution.account': 'team-2', 'execution.vm': vm, 'execution.attach': attach }),
  ])

  lostRun = `uipass-lost-${RUN}`
  const l = lostRun
  const lost = { 'run.title': 'Move the order history to the new table', 'run.executions': 2 }
  await sendEvents([
    runEvent(l, 1, ago(30), 'created', 'queued', lost),
    runEvent(l, 2, ago(30), 'queued', 'queued', lost),
    runEvent(l, 3, ago(29), 'execution_started', 'running', { ...lost, 'execution.id': 'x1', 'execution.account': 'team-1', 'execution.vm': 'agent-machine-3' }),
    runEvent(l, 4, ago(20), 'machine_lost', 'queued', { ...lost, ...dispatcher, 'execution.id': 'x1', 'machine_lost.reason': 'machine lost', 'machine_lost.in_a_row': 1, 'machine_lost.restart': true }),
    runEvent(l, 5, ago(19), 'queued', 'queued', { ...lost, ...dispatcher }),
    runEvent(l, 6, ago(18), 'execution_started', 'running', { ...lost, 'execution.id': 'x2', 'execution.account': 'team-1', 'execution.vm': 'agent-machine-4' }),
  ])
})

test('every factory page at both sizes and themes', async ({ browser, baseURL }) => {
  test.setTimeout(900_000)
  // Sign in once for the contexts below.
  const login = await browser.newContext({ baseURL })
  const lp = await login.newPage()
  await lp.goto('/login')
  await lp.fill('input[type=email]', ADMIN.email)
  await lp.fill('input[type=password]', ADMIN.password)
  await lp.click('button[type=submit]')
  await lp.waitForURL(u => !u.pathname.startsWith('/login'), { timeout: 30_000 })
  const state = await login.storageState()
  await login.close()

  const run = (id: string) => `/${ORG}/runs/${id}`
  const screens: Screen[] = [
    { name: 'runs-list', go: async p => { await p.goto(`/${ORG}/runs`); await expect(p.locator('tr[data-run]').first()).toBeVisible() } },
    { name: 'run-done', go: async p => { await p.goto(run(runs[0])); await expect(p.locator('[data-section="report"]')).toBeVisible() } },
    { name: 'run-done-no-evidence', go: async p => { await p.goto(run(runs[1])); await expect(p.locator('[data-flag="no-evidence"]')).toBeVisible() } },
    { name: 'run-waiting', go: async p => { await p.goto(run(runs[2])); await expect(p.locator('[data-section="events"]')).toBeVisible() } },
    { name: 'run-failed', go: async p => { await p.goto(run(runs[3])); await expect(p.locator('[data-section="events"]')).toBeVisible() } },
    { name: 'run-taken-over', go: async p => { await p.goto(run(takenOverRun)); await expect(p.locator('[data-take-over="vm"]')).toBeVisible() } },
    { name: 'run-lost-execution', go: async p => { await p.goto(run(lostRun)); await expect(p.locator('[data-section="events"]')).toBeVisible() } },
    {
      name: 'project-factory-section', element: '[data-section="factory"]',
      go: async p => {
        await p.goto(`/${ORG}/projects/${PROJECT}`)
        await expect(p.locator('[data-section="factory"] #factory-repository')).toBeVisible()
      },
    },
    { name: 'settings-instance', go: async p => { await p.goto(`/${ORG}/settings/instance`) } },
    { name: 'settings-agent-sessions', go: async p => { await p.goto(`/${ORG}/settings/agent_sessions`) } },
    { name: 'settings-capture-setup', go: async p => { await p.goto(`/${ORG}/settings/agent_sessions/setup`) } },
    { name: 'agent-sessions-list', go: async p => { await p.goto(`/${ORG}/agent_sessions`) } },
    {
      name: 'agent-session',
      go: async p => {
        await p.goto(run(runs[0]))
        const href = await p.locator('a[href*="/agent_sessions/"]').first().getAttribute('href')
        await p.goto(href!)
      },
    },
    {
      name: 'factory-dashboard',
      go: async p => {
        await p.goto(`/${ORG}/dashboards`)
        await p.getByRole('link', { name: 'Factory', exact: true }).first().click()
        await expect(p.getByRole('heading', { name: 'Factory', exact: true })).toBeVisible()
        // The Charts run their Queries. The dev Console answers 'busy' to a Chart that
        // starts while others run: Refresh until none says so.
        for (let i = 0; i < 6; i++) {
          for (let j = 0; j < 30; j++) {
            if (await p.getByRole('status', { name: "Running the chart's Query" }).count() === 0) break
            await p.waitForTimeout(1000)
          }
          if (await p.getByText('Console is busy running other charts').count() === 0) break
          await p.getByRole('button', { name: 'Refresh', exact: true }).click()
          await p.waitForTimeout(1500)
        }
      },
    },
    { name: 'api-tokens', go: async p => { await p.goto(`/${ORG}/settings/api_tokens`) } },
  ]

  for (const size of SIZES) {
    for (const theme of THEMES) {
      const ctx = await browser.newContext({ baseURL, viewport: size.viewport, colorScheme: theme, storageState: state })
      await ctx.addInitScript(t => { try { localStorage.setItem('theme', t) } catch { /* private mode */ } }, theme)
      for (const s of screens) {
        const where = `${s.name} ${size.name}-${theme}`
        const page = await ctx.newPage()
        const errors: string[] = []
        page.on('pageerror', e => errors.push(`pageerror: ${e.message}`))
        page.on('console', m => {
          // The dev Console may answer 503 (busy) to the explorer's first run.
          if (m.type() === 'error' && !/status of 503/.test(m.text())) errors.push(`console: ${m.text()}`)
        })
        try {
          await s.go(page)
          await page.waitForLoadState('networkidle', { timeout: 20_000 }).catch(() => {})
          await page.waitForTimeout(600)
          // The app scrolls an inner box, not the document: grow the viewport to the content.
          const extra = await page.evaluate(() => {
            let most = 0
            for (const el of document.querySelectorAll('*')) {
              const st = getComputedStyle(el)
              if ((st.overflowY === 'auto' || st.overflowY === 'scroll') && el.scrollHeight > el.clientHeight + 1) most = Math.max(most, el.scrollHeight - el.clientHeight)
            }
            return most
          })
          if (extra > 0) {
            await page.setViewportSize({ width: size.viewport.width, height: Math.min(size.viewport.height + extra, 12000) })
            await page.waitForTimeout(500)
          }
          const file = join(OUT, `${s.name}--${size.name}-${theme}.png`)
          await page.screenshot({ path: file, animations: 'disabled' })
          if (s.element) await page.locator(s.element).screenshot({ path: join(OUT, `${s.name}-only--${size.name}-${theme}.png`), animations: 'disabled' })

          const found = await page.evaluate(() => {
            const out: string[] = []
            const across = document.documentElement.scrollWidth - document.documentElement.clientWidth
            if (across > 1) out.push(`the page overflows across by ${across}px`)
            for (const el of document.querySelectorAll('*')) {
              const st = getComputedStyle(el)
              if ((st.overflowX === 'auto' || st.overflowX === 'scroll') && el.scrollWidth > el.clientWidth + 1 && el.clientWidth > 0) {
                const name = el.tagName.toLowerCase() + (el.className && typeof el.className === 'string' ? '.' + el.className.split(' ').slice(0, 3).join('.') : '')
                out.push(`${name} scrolls inside by ${el.scrollWidth - el.clientWidth}px`)
              }
            }
            const text = document.body.innerText
            for (const raw of [/\bundefined\b/, /\bnull\b/, /\bNone\b/, /\[object /, /\bNaN\b/]) {
              const m = text.match(raw)
              if (m) out.push(`the text has "${m[0]}"`)
            }
            return out
          })
          for (const f of found) problems.push(`${where}: ${f}`)
        } catch (e) {
          problems.push(`${where}: ${(e as Error).message.split('\n')[0]}`)
        }
        for (const e of errors) problems.push(`${where}: ${e}`)
        await page.close()
      }
      await ctx.close()
    }
  }
})

// The confirm checkbox of Settings -> Agent sessions: with a new entry, the
// box and its notice show, and a click on the box ticks it (nothing covers it).
for (const width of [1280, 1440, 390]) {
  test(`the confirm checkbox of the captured repositories works at ${width} px`, async ({ browser, baseURL }) => {
    const ctx = await browser.newContext({ baseURL, viewport: { width, height: 800 }, storageState: join(consoleDir, ADMIN_STATE) })
    const page = await ctx.newPage()
    await page.goto(`/${ORG}/settings/agent_sessions`)
    const entries = page.locator('#captured_repositories')
    await entries.fill(`${await entries.inputValue()}\nflows.example/ui-pass-${RUN}/*`)
    const box = page.locator('#confirmed')
    await expect(box).toBeVisible()
    await box.scrollIntoViewIfNeeded()
    mkdirSync(OUT, { recursive: true })
    await page.screenshot({ path: join(OUT, `settings-agent-sessions-confirm--${width}.png`), animations: 'disabled' })
    // The id is on the visible box (not on a hidden input at the window's top left), and
    // nothing covers it: the element at its centre is the box.
    await expect(box).toHaveAttribute('role', 'checkbox')
    const covered = await box.evaluate(el => {
      const r = el.getBoundingClientRect()
      const at = document.elementFromPoint(r.x + r.width / 2, r.y + r.height / 2)
      return !at || !el.contains(at)
    })
    expect(covered, 'something covers the confirm checkbox').toBe(false)
    await box.click({ timeout: 5_000 })
    await expect(box).toBeChecked()
    await ctx.close()
  })
}
