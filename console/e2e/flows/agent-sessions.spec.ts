import { test, expect, type Browser, type Locator, type Page } from '@playwright/test'
import { execFileSync, spawn } from 'node:child_process'
import { existsSync, mkdirSync, readFileSync, rmSync } from 'node:fs'
import { join, resolve } from 'node:path'
import { ADMIN, MEMBER, ORG } from '../support/data'
import { fakeAnthropicKey, longSession, MODEL, shortSession, writeSession, type FixtureSession } from '../support/agentTranscripts'
import { shoot, watchErrors } from '../support/screens'
import { newContext } from '../support/video'

// Laptop Agent sessions, from the real capture to the pages. The admin and
// the member each sign in the real `solidtrace` CLI with the device flow
// (`solidtrace login --device`): the CLI shows a code, the flow types it on
// the device page and approves. Then fixture transcripts in each person's
// Claude projects folder go up through `solidtrace capture`, as the plugin's
// SessionEnd hook runs it, to this stack's EventStore. The flow checks the
// Agent sessions page (rows, totals, the person and repository filters) and
// one Agent session page: a subagent inside its parent, 'Show full' on a cut
// tool output, and a secret shown as '*'.
//
// OAuth needs a Console URL: the flow sets this stack's own, and puts the
// old one back after. The flow stack's ClickHouse keeps the rows of earlier
// runs, so each run uses its own repositories under one captured entry.

const RUN = Date.now().toString(36)
const ENTRY = 'flows.example/flowteam/*'
const APP = `flows.example/flowteam/app-${RUN}`
const TOOLS = `flows.example/flowteam/tools-${RUN}`
const ELSEWHERE = `elsewhere.example/other/notes-${RUN}`
const INGEST_URL = process.env.E2E_INGEST_URL ?? 'http://localhost:4000'
const ADMIN_NAME = 'Admin User'
const MEMBER_NAME = 'Member User'

const consoleDir = resolve(__dirname, '../..')
const work = join(consoleDir, 'tmp', `e2e-agent-sessions-${RUN}`)
const cli = join(work, 'solidtrace')

const rails = (code: string) => execFileSync('mise', ['exec', '--', 'bin/rails', 'runner', code], {
  cwd: consoleDir, stdio: 'pipe', timeout: 120_000,
}).toString().trim()

/** One laptop: a person's CLI config folder and Claude Code config folder. */
type Machine = { config: string; claude: string; env: NodeJS.ProcessEnv }

function machine(name: string): Machine {
  const config = join(work, name, 'solidtrace')
  const claude = join(work, name, 'claude')
  mkdirSync(config, { recursive: true })
  mkdirSync(join(claude, 'projects'), { recursive: true })
  return { config, claude, env: { ...process.env, SOLIDTRACE_CONFIG_DIR: config, CLAUDE_CONFIG_DIR: claude, DISPLAY: '', WAYLAND_DISPLAY: '' } }
}

/** A git repository with an origin remote at the repository, host/workspace/name. */
function gitRepository(repository: string): string {
  const [host, workspace, name] = repository.split('/')
  const dir = join(work, 'repos', name)
  mkdirSync(dir, { recursive: true })
  const git = (...args: string[]) => execFileSync('git', args, { cwd: dir, stdio: 'pipe', env: { ...process.env, GIT_CONFIG_GLOBAL: '/dev/null' } })
  git('init', '-q', '-b', 'main')
  git('remote', 'add', 'origin', `git@${host}:${workspace}/${name}.git`)
  return dir
}

/** The Claude project folder of a working directory, as Claude Code names it. */
function projectDir(m: Machine, cwd: string): string {
  return join(m.claude, 'projects', cwd.replace(/[^A-Za-z0-9]/g, '-'))
}

/** Runs `solidtrace capture` with the SessionEnd hook's input, as the plugin does. */
function capture(m: Machine, session: FixtureSession, cwd: string) {
  const transcript = writeSession(projectDir(m, cwd), session)
  const input = { session_id: session.id, transcript_path: transcript, cwd, hook_event_name: 'SessionEnd' }
  execFileSync(cli, ['capture'], { input: JSON.stringify(input), env: m.env, cwd, stdio: 'pipe', timeout: 60_000 })
}

/** `solidtrace login --device`: the flow types the CLI's code on the device page and approves. */
async function signIn(page: Page, m: Machine, origin: string) {
  const child = spawn(cli, ['login', '--device', '--ingest-url', INGEST_URL, origin], { env: m.env, stdio: ['ignore', 'pipe', 'pipe'] })
  let out = ''
  let err = ''
  child.stdout.on('data', d => { out += d })
  child.stderr.on('data', d => { err += d })
  const exited = new Promise<number | null>(done => child.on('exit', code => done(code)))
  try {
    await expect.poll(() => out, { message: 'the CLI shows a code', timeout: 15_000 }).toMatch(/type this code:\s+\S+/)
    const [, deviceUrl, code] = out.match(/open (\S+) and type this code:\s+(\S+)/)!
    expect(deviceUrl).toBe(`${origin}/device`)

    await page.goto(deviceUrl)
    await page.getByLabel('Code').fill(code)
    await page.getByRole('button', { name: 'Continue' }).click()
    await expect(page.getByText('SolidTrace CLI', { exact: true })).toBeVisible()
    // The engineer's notice: what uploads, before the approval.
    await expect(page.getByRole('note', { name: 'What uploads' })).toContainText(ENTRY)
    await page.getByRole('button', { name: 'Approve' }).click()
    await expect(page.getByRole('status')).toBeVisible()

    expect(await exited, `login's output:\n${out}\n${err}`).toBe(0)
  } finally {
    child.kill()
  }
  expect(out).toContain(`Signed in to ${origin}.`)
  expect(out).toContain(ENTRY)
}

async function signedInPage(browser: Browser, user: { email: string; password: string }): Promise<Page> {
  const context = await newContext(browser, { storageState: { cookies: [], origins: [] } })
  const page = await context.newPage()
  await page.goto('/login')
  await page.fill('input[type=email]', user.email)
  await page.fill('input[type=password]', user.password)
  await page.click('button[type=submit]')
  await page.waitForURL(url => !url.pathname.startsWith('/login'))
  return page
}

/** The ids of the rows on the Agent sessions page. */
async function rowIds(page: Page): Promise<string[]> {
  const ids = await page.getByRole('table', { name: 'Agent sessions' }).locator('tr[data-session]')
    .evaluateAll(rows => rows.map(r => r.getAttribute('data-session')!))
  return ids.sort()
}

function stat(totals: Locator, label: string): Locator {
  return totals.locator(':scope > div').filter({ has: totals.page().getByText(label, { exact: true }) }).locator('div').nth(1)
}

async function expectTotals(page: Page, want: Record<string, string>) {
  const totals = page.getByRole('region', { name: 'Totals of the rows' })
  for (const [label, value] of Object.entries(want)) {
    await expect(stat(totals, label), label).toHaveText(value)
  }
}

async function choose(page: Page, filter: string, option: string) {
  await page.getByRole('combobox', { name: filter }).click()
  await page.getByRole('option', { name: option, exact: true }).click()
}

let oldConsoleUrl = ''

test.beforeAll(({ baseURL }) => {
  test.setTimeout(240_000)
  rmSync(work, { recursive: true, force: true })
  mkdirSync(work, { recursive: true })
  execFileSync('mise', ['exec', '--', 'go', 'build', '-o', cli, './cmd/solidtrace'], {
    cwd: resolve(consoleDir, '../cli'), stdio: 'pipe', timeout: 180_000, env: { ...process.env, CGO_ENABLED: '0' },
  })

  oldConsoleUrl = rails('print InstanceSetting.current.console_url.to_s')
  // Agent sessions on, with the flow's captured entry: the Admin's turn-on
  // or a save that adds the entry, each with the confirm.
  rails(`
    InstanceSetting.current.update!(console_url: ${JSON.stringify(new URL(baseURL!).origin)})
    org = Organization.find_by!(slug: ${JSON.stringify(ORG)})
    admin = User.find_by!(email: ${JSON.stringify(ADMIN.email)})
    project = org.agent_project
    result = if project
      AgentSessionsSettings.save(project: project, user: admin, entries: project.captured_repositories | [${JSON.stringify(ENTRY)}], confirmed: true)
    else
      AgentSessionsSettings.turn_on(organization: org, user: admin, entries: [${JSON.stringify(ENTRY)}], confirmed: true)
    end
    raise result.errors.inspect unless result.done?
  `)
})

test.afterAll(() => {
  rails(`InstanceSetting.current.update!(console_url: ${oldConsoleUrl ? JSON.stringify(oldConsoleUrl) : 'nil'})`)
  rmSync(work, { recursive: true, force: true })
})

test('laptop Agent sessions upload through the real capture and show on the Agent sessions pages', async ({ page, browser, baseURL }) => {
  test.setTimeout(240_000)
  const errors = watchErrors(page)
  const origin = new URL(baseURL!).origin
  const admin = machine('admin')
  const member = machine('member')

  // Each person signs the CLI in with the device flow. Opening the
  // Organization first makes it the one the approval page picks.
  await page.goto(`/${ORG}/agent_sessions`)
  await signIn(page, admin, origin)
  const memberPage = await signedInPage(browser, MEMBER)
  await memberPage.goto(`/${ORG}/agent_sessions`)
  await signIn(memberPage, member, origin)
  await memberPage.context().close()

  // The fixture transcripts, and the capture at each SessionEnd.
  const appDir = gitRepository(APP)
  const toolsDir = gitRepository(TOOLS)
  const elsewhereDir = gitRepository(ELSEWHERE)
  const secret = fakeAnthropicKey()
  const subagentId = `a${RUN}0f1e2d`
  const outputEnd = `END-OF-OUTPUT-${RUN}`
  const long = longSession(appDir, {
    personText: `Find the cart code. My key is ${secret} for the test server.`,
    subagentId, subagentReply: 'It is in cart.go.', outputEnd, secret,
    usage: [
      { input: 11, output: 21, cacheRead: 101, cacheWrite: 31 },
      { input: 12, output: 22, cacheRead: 102, cacheWrite: 32 },
      { input: 13, output: 23, cacheRead: 103, cacheWrite: 33 },
    ],
    subagentUsage: { input: 14, output: 24, cacheRead: 104, cacheWrite: 34 },
  })
  const tools = shortSession(toolsDir, 'Update the build script.', { input: 6, output: 16, cacheRead: 106, cacheWrite: 26 })
  const notCaptured = shortSession(elsewhereDir, 'Write my notes.', { input: 1, output: 1, cacheRead: 1, cacheWrite: 1 })
  const members = shortSession(appDir, 'Fix the cart total.', { input: 5, output: 15, cacheRead: 105, cacheWrite: 25 })
  capture(admin, long, appDir)
  capture(admin, tools, toolsDir)
  capture(admin, notCaptured, elsewhereDir)
  capture(member, members, appDir)

  // The capture log has no failure, only the session outside the list;
  // `capture status` shows what uploaded.
  const captureLog = (m: Machine) => existsSync(join(m.config, 'capture.log')) ? readFileSync(join(m.config, 'capture.log'), 'utf8') : ''
  expect(captureLog(admin).trim().split('\n')).toEqual([
    expect.stringMatching(new RegExp(`^\\S+ ${notCaptured.id}: not in the captured repositories \\(.*notes-${RUN}\\), nothing sent$`)),
  ])
  expect(captureLog(member)).toBe('')
  const status = execFileSync(cli, ['capture', 'status'], { env: admin.env, stdio: 'pipe' }).toString()
  expect(status).toMatch(new RegExp(`${long.id}\\s+uploaded\\s+${APP}`))
  expect(status).toMatch(new RegExp(`${tools.id}\\s+uploaded\\s+${TOOLS}`))
  expect(status).toMatch(new RegExp(`${notCaptured.id}\\s+not in the list`))

  // The member's CLI cannot add to, or read, the admin's Agent session: a
  // transcript with the admin's session id is refused when the CLI asks
  // what the server has, and nothing is sent. The totals below show that
  // the admin's Agent session did not change.
  const other = shortSession(appDir, 'Read the admin session.', { input: 900, output: 900, cacheRead: 900, cacheWrite: 900 })
  const impostor: FixtureSession = { id: long.id, files: { [`${long.id}.jsonl`]: other.files[`${other.id}.jsonl`].replaceAll(other.id, long.id) } }
  capture(member, impostor, appDir)
  expect(captureLog(member).trim().split('\n')).toEqual([
    expect.stringMatching(new RegExp(`^\\S+ capture ${long.id}: ask what the server has: the agent ingest API answered 403: Agent session ${long.id} was uploaded by another person\\.`)),
  ])

  // The Agent sessions page, once EventStore has parsed the uploads.
  await expect(async () => {
    await page.goto(`/${ORG}/agent_sessions`)
    const ids = await rowIds(page)
    expect(ids).toEqual(expect.arrayContaining([long.id, tools.id, members.id]))
  }).toPass({ timeout: 90_000, intervals: [1_000, 2_000, 3_000] })
  expect(await rowIds(page)).not.toContain(notCaptured.id)

  // The repository filter: the two Agent sessions in the app repository.
  await choose(page, 'Repository', APP)
  await expect(page).toHaveURL(new RegExp(`repository=${encodeURIComponent(APP)}`))
  await expect.poll(() => rowIds(page)).toEqual([long.id, members.id].sort())
  await expectTotals(page, {
    'Agent sessions': '2', Tokens: '830', Input: '55', Output: '105', 'Cache read': '515', 'Cache write': '155',
  })
  const table = page.getByRole('table', { name: 'Agent sessions' })
  const longRow = table.locator(`tr[data-session="${long.id}"]`)
  await expect(longRow).toContainText(ADMIN_NAME)
  await expect(longRow).toContainText(APP)
  await expect(longRow).toContainText(MODEL)
  await expect(longRow).toContainText('680')
  await expect(table.locator(`tr[data-session="${members.id}"]`)).toContainText(MEMBER_NAME)

  // The person filter, with the repository filter: one person's rows.
  await choose(page, 'Person', MEMBER_NAME)
  await expect.poll(() => rowIds(page)).toEqual([members.id])
  await expectTotals(page, { 'Agent sessions': '1', Tokens: '150', Input: '5' })
  await choose(page, 'Person', ADMIN_NAME)
  await expect.poll(() => rowIds(page)).toEqual([long.id])
  await expectTotals(page, { 'Agent sessions': '1', Tokens: '680', Input: '50', Output: '90', 'Cache read': '410', 'Cache write': '130' })

  // The other repository.
  await choose(page, 'Repository', TOOLS)
  await expect.poll(() => rowIds(page)).toEqual([tools.id])
  await expectTotals(page, { 'Agent sessions': '1', Tokens: '154' })

  // The Agent session page of the long session.
  await choose(page, 'Repository', APP)
  await expect.poll(() => rowIds(page)).toEqual([long.id])
  await table.locator(`tr[data-session="${long.id}"]`).getByRole('link', { name: ADMIN_NAME }).click()
  await expect(page).toHaveURL(new RegExp(`/${ORG}/agent_sessions/${long.id}`))
  await expect(page.getByText(long.id, { exact: true })).toBeVisible()

  // The header and each person step name the person whose CLI uploaded it.
  const header = page.locator('[data-section="agent-session-header"]')
  await expect(header.locator('div > div').filter({ hasText: /^Person$/ }).locator('xpath=following-sibling::div')).toHaveText(ADMIN_NAME)

  // The secret shows as '*', byte for byte.
  const person = page.locator('[data-step="person"]').first()
  await expect(person).toContainText(ADMIN_NAME)
  await expect(person).toContainText(`My key is ${'*'.repeat(secret.length)} for the test server.`)
  expect(await page.content()).not.toContain(secret)

  // The subagent shows inside the Agent tool call that started it, and its
  // steps load when it is opened.
  const agentCall = page.locator('[data-step="tool"][data-tool="Agent"]')
  const subagent = agentCall.locator(`[data-subagent="${subagentId}"]`)
  await expect(subagent).toBeVisible()
  await subagent.getByRole('button', { name: `Subagent ${subagentId}` }).click()
  await expect(subagent.getByText('It is in cart.go.', { exact: true })).toBeVisible()
  await expect(subagent.getByText(`subagent ${subagentId}`, { exact: true }).first()).toBeVisible()
  // The subagent's prompt is not from a person, so it has no person's name.
  const prompt = subagent.locator('[data-step="not from a person"]')
  await expect(prompt).toContainText('Find the cart code.')
  await expect(prompt).toContainText('Not from a person')
  await expect(prompt).not.toContainText(ADMIN_NAME)
  await expect(subagent.locator('[data-step="person"]')).toHaveCount(0)

  // The Bash output is past the Log limit: cut, and 'Show full' reads the
  // whole raw line, which has the end marker and the second secret redacted.
  const bash = page.locator('[data-step="tool"][data-tool="Bash"]')
  await expect(bash.getByText('cut', { exact: true })).toBeVisible()
  await expect(bash).not.toContainText(outputEnd)
  await bash.getByRole('button', { name: 'Show full' }).click()
  const raw = bash.locator('[data-raw-line]')
  await expect(raw).toContainText(outputEnd)
  await expect(raw).toContainText(`key ${'*'.repeat(secret.length)}`)
  await expect(raw).not.toContainText(secret)
  await shoot(page, 'agent-session-show-full')
  await bash.getByRole('button', { name: 'Show the step' }).click()
  await expect(raw).toHaveCount(0)

  expect(errors).toEqual([])
})
