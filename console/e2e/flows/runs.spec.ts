import { test, expect, type Browser, type Page } from '@playwright/test'
import { execFileSync, spawn, spawnSync, type ChildProcessWithoutNullStreams } from 'node:child_process'
import { existsSync, mkdirSync, readdirSync, readFileSync, rmSync, statSync, writeFileSync } from 'node:fs'
import { join, relative, resolve } from 'node:path'
import { createInterface } from 'node:readline'
import { randomUUID } from 'node:crypto'
import { ADMIN, MEMBER, ORG, PROJECT } from '../support/data'
import { fakeAnthropicKey } from '../support/agentTranscripts'
import { watchErrors } from '../support/screens'

// Seam 5: the real Dispatcher and the real wrapper against this stack's
// Console, EventStore and ClickHouse. The flow builds the factory's flow
// stack (factory/internal/flowstack): the Dispatcher with the Seam 3
// fakes for the Runner (each Execution is a local wrapper process), the
// Harness (a scripted Claude Code), the Forge and the Tracker. Each call
// that the factory makes to SolidTrace goes to the real API: the token
// checks by introspection with real roles, the child tokens, the run
// events and the uploads of the Agent session.
//
// The admin and the member sign the real `solidtrace` CLI in with the
// device flow. The admin saves the forge connection and the Factory
// settings; only an Admin edits the Factory section. The member starts a
// Run with the CLI, sees it on the Runs page and the Run page, and stops
// it with the Stop button. The Execution's child token reads only its own
// Agent session, its download for a resume is equal byte for byte to the
// redacted upload, and it stops working after the Execution.

const RUN = Date.now().toString(36)
const ENTRY = 'flows.example/flowteam/*'
const INGEST_URL = process.env.E2E_INGEST_URL ?? 'http://localhost:4000'
const REPOSITORY = `bitbucket.org/example/${PROJECT}`
const PROMPT = `Fix the cart total ${RUN}`

const consoleDir = resolve(__dirname, '../..')
const work = join(consoleDir, 'tmp', `e2e-runs-${RUN}`)
const cli = join(work, 'solidtrace')
const flowstackBin = join(work, 'flowstack')

const rails = (code: string) => execFileSync('mise', ['exec', '--', 'bin/rails', 'runner', code], {
  cwd: consoleDir, stdio: 'pipe', timeout: 120_000,
}).toString().trim()

type Machine = { config: string; env: NodeJS.ProcessEnv }

function machine(name: string): Machine {
  const config = join(work, name, 'solidtrace')
  mkdirSync(config, { recursive: true })
  return { config, env: { ...process.env, SOLIDTRACE_CONFIG_DIR: config, DISPLAY: '', WAYLAND_DISPLAY: '' } }
}

/** Runs the person's CLI; the flow checks the exit code itself. */
function solidtrace(m: Machine, args: string[], input = '') {
  const r = spawnSync(cli, args, { env: m.env, input, timeout: 60_000 })
  return { code: r.status, out: r.stdout.toString(), err: r.stderr.toString() }
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
    await page.goto(deviceUrl)
    await page.getByLabel('Code').fill(code)
    await page.getByRole('button', { name: 'Continue' }).click()
    await page.getByRole('button', { name: 'Approve' }).click()
    await expect(page.getByRole('status')).toBeVisible()
    expect(await exited, `login's output:\n${out}\n${err}`).toBe(0)
  } finally {
    child.kill()
  }
}

async function signedInPage(browser: Browser, user: { email: string; password: string }): Promise<Page> {
  const context = await browser.newContext({ storageState: { cookies: [], origins: [] } })
  const page = await context.newPage()
  await page.goto('/login')
  await page.fill('input[type=email]', user.email)
  await page.fill('input[type=password]', user.password)
  await page.click('button[type=submit]')
  await page.waitForURL(url => !url.pathname.startsWith('/login'))
  return page
}

/** Each regular file under dir, by its path in dir. */
function filesIn(dir: string): Record<string, Buffer> {
  const files: Record<string, Buffer> = {}
  const walk = (d: string) => {
    for (const name of readdirSync(d)) {
      const path = join(d, name)
      if (statSync(path).isDirectory()) walk(path)
      else files[relative(dir, path)] = readFileSync(path)
    }
  }
  if (existsSync(dir)) walk(dir)
  return files
}

/** The flow stack: its Dispatcher's URL, its control API and the fake Forge's token. */
type Stack = { dispatcher_url: string; control_url: string; forge_token: string }

let stack: Stack
let flowstack: ChildProcessWithoutNullStreams | undefined
let flowstackLog = ''
let dispatcherTokenId = 0
let oldConsoleUrl = ''
let oldDispatcherUrl = ''
const admin = () => machine('admin')
const member = () => machine('member')

async function control(method: string, path: string, body?: unknown) {
  const resp = await fetch(stack.control_url + path, {
    method, headers: { 'Content-Type': 'application/json' }, body: body === undefined ? undefined : JSON.stringify(body),
  })
  expect(resp.ok, `${method} ${path}: ${resp.status} ${await resp.clone().text()}`).toBe(true)
  return resp.status === 204 ? undefined : resp.json()
}

test.describe.configure({ mode: 'serial' })

test.beforeAll(async ({ baseURL }) => {
  test.setTimeout(420_000)
  rmSync(work, { recursive: true, force: true })
  mkdirSync(work, { recursive: true })
  execFileSync('mise', ['exec', '--', 'go', 'build', '-o', cli, './cmd/solidtrace'], {
    cwd: resolve(consoleDir, '../cli'), stdio: 'pipe', timeout: 180_000, env: { ...process.env, CGO_ENABLED: '0' },
  })
  execFileSync('mise', ['exec', '--', 'go', 'build', '-o', flowstackBin, './internal/flowstack'], {
    cwd: resolve(consoleDir, '../factory'), stdio: 'pipe', timeout: 240_000,
  })

  // Agent sessions on, with a captured entry (the CLI's sign-in reads the
  // list), and the Dispatcher's own token, made by the Admin.
  const origin = new URL(baseURL!).origin
  const made = JSON.parse(rails(`
    settings = InstanceSetting.current
    old = { console_url: settings.console_url.to_s, dispatcher_url: settings.dispatcher_url.to_s }
    settings.update!(console_url: ${JSON.stringify(origin)})
    org = Organization.find_by!(slug: ${JSON.stringify(ORG)})
    admin = User.find_by!(email: ${JSON.stringify(ADMIN.email)})
    project = org.agent_project
    result = if project
      AgentSessionsSettings.save(project: project, user: admin, entries: project.captured_repositories | [${JSON.stringify(ENTRY)}], confirmed: true)
    else
      AgentSessionsSettings.turn_on(organization: org, user: admin, entries: [${JSON.stringify(ENTRY)}], confirmed: true)
    end
    raise result.errors.inspect unless result.done?
    token, secret = ApiToken.issue!(user: admin, name: "Flow Dispatcher ${RUN}", scopes: Oauth::Scopes::DISPATCHER, organization_id: org.id)
    print old.merge(id: token.id, secret: secret).to_json
  `))
  oldConsoleUrl = made.console_url
  oldDispatcherUrl = made.dispatcher_url
  dispatcherTokenId = made.id

  flowstack = spawn(flowstackBin, [], {
    cwd: work,
    env: { ...process.env, FLOWSTACK_CONSOLE_URL: origin, FLOWSTACK_INGEST_URL: INGEST_URL, DISPATCHER_SOLIDTRACE_TOKEN: made.secret },
  })
  flowstack.stderr.on('data', d => { flowstackLog += d })
  const exited = new Promise<never>((_, fail) => flowstack!.on('exit', code => fail(new Error(`flowstack exited ${code}:\n${flowstackLog}`))))
  const line = new Promise<string>(done => createInterface({ input: flowstack!.stdout }).once('line', done))
  stack = JSON.parse(await Promise.race([line, exited]))

  rails(`InstanceSetting.current.update!(dispatcher_url: ${JSON.stringify(stack.dispatcher_url)})`)
  await control('POST', '/repos', { name: `example/${PROJECT}` })
})

test.afterAll(async () => {
  if (flowstack && flowstack.exitCode === null) {
    const gone = new Promise(done => flowstack!.on('exit', done))
    flowstack.stdin.end()
    await Promise.race([gone, new Promise(done => setTimeout(done, 20_000))])
    flowstack.kill('SIGKILL')
  }
  // The Dispatcher's log, to read after a failure.
  writeFileSync(join(consoleDir, 'tmp', 'e2e-runs-flowstack.log'), flowstackLog)
  rails(`
    InstanceSetting.current.update!(console_url: ${oldConsoleUrl ? JSON.stringify(oldConsoleUrl) : 'nil'},
      dispatcher_url: ${oldDispatcherUrl ? JSON.stringify(oldDispatcherUrl) : 'nil'})
    ApiToken.where(id: ${dispatcherTokenId}).destroy_all
  `)
  rmSync(work, { recursive: true, force: true })
})

test('only an Admin edits the Factory section; a member sees it read-only', async ({ page, browser, baseURL }) => {
  test.setTimeout(180_000)
  const errors = watchErrors(page)
  const origin = new URL(baseURL!).origin

  await page.goto(`/${ORG}/runs`)
  await signIn(page, admin(), origin)
  const memberPage = await signedInPage(browser, MEMBER)
  await memberPage.goto(`/${ORG}/runs`)
  await signIn(memberPage, member(), origin)

  // The Admin saves the forge connection and first settings with the CLI.
  const dispatcher = ['--dispatcher', stack.dispatcher_url]
  let r = solidtrace(admin(), ['connections', 'set', 'forge', '--kind', 'bitbucket', ...dispatcher], stack.forge_token)
  expect(r.code, r.out + r.err).toBe(0)
  r = solidtrace(admin(), ['projects', 'set', PROJECT, '--repository', `bitbucket.org/example/old-${RUN}`, '--connection', 'forge', ...dispatcher])
  expect(r.code, r.out + r.err).toBe(0)

  // A member's token is refused for an Admin's change, by its real role.
  r = solidtrace(member(), ['projects', 'set', PROJECT, '--repository', `bitbucket.org/example/member-${RUN}`, ...dispatcher])
  expect(r.code).not.toBe(0)
  expect(r.err).toContain('only an Admin')

  // The Admin changes the repository in the Factory section.
  await page.goto(`/${ORG}/projects/${PROJECT}`)
  const section = page.locator('[data-section="factory"]')
  const repository = section.locator('#factory-repository')
  await expect(repository).toHaveValue(`bitbucket.org/example/old-${RUN}`)
  await expect(repository).not.toHaveAttribute('readonly')
  await repository.fill(REPOSITORY)
  await section.getByRole('button', { name: 'Save' }).click()
  await expect(section.getByRole('button', { name: 'Save' })).toBeEnabled()
  await page.reload()
  await expect(page.locator('[data-section="factory"] #factory-repository')).toHaveValue(REPOSITORY)
  // The save kept the connection that the section does not show.
  r = solidtrace(admin(), ['projects', 'get', PROJECT])
  expect(r.code, r.out + r.err).toBe(0)
  expect(r.out).toContain(REPOSITORY)
  expect(r.out).toContain('forge')

  // The member reads the same values, and cannot change them.
  await memberPage.goto(`/${ORG}/projects/${PROJECT}`)
  const memberSection = memberPage.locator('[data-section="factory"]')
  await expect(memberSection.locator('#factory-repository')).toHaveValue(REPOSITORY)
  await expect(memberSection.locator('#factory-repository')).toHaveAttribute('readonly')
  await expect(memberSection.getByRole('button', { name: 'Save' })).toHaveCount(0)
  await memberPage.context().close()

  expect(errors).toEqual([])
})

test('a member starts a Run with the CLI, sees its events, and stops it; the Execution token reads only its Agent session', async ({ browser, baseURL }) => {
  test.setTimeout(240_000)
  const memberPage = await signedInPage(browser, MEMBER)
  const errors = watchErrors(memberPage)
  const secret = fakeAnthropicKey()
  const kept = join(work, 'kept')

  // The agent replies with a secret in its text, keeps a copy of its
  // Claude project directory (the end deletes the home), and works until
  // the interrupt.
  await control('POST', '/agent', {
    lines: [
      JSON.stringify({ fake: 'await_message' }),
      JSON.stringify({ type: 'system', subtype: 'init', session_id: '', uuid: '', cwd: '', permissionMode: '', model: 'claude-haiku-5-5', tools: ['Bash', 'Read'] }),
      JSON.stringify({
        type: 'assistant', session_id: '', uuid: '', parent_tool_use_id: null,
        message: { id: 'msg_01Flow', type: 'message', role: 'assistant', model: 'claude-haiku-5-5', stop_reason: 'end_turn',
          content: [{ type: 'text', text: `The test server key is ${secret}. I am on the cart code.` }] },
      }),
      JSON.stringify({ fake: 'run', args: ['sh', '-c', `cp -R "$CLAUDE_CONFIG_DIR/projects/$CLAUDE_CODE_PROJECT_DIR_NAME" ${kept}`] }),
      JSON.stringify({ fake: 'busy', ms: 1000, eof_code: 0 }),
    ],
  })

  // The member starts the Run with the CLI.
  const started = solidtrace(member(), ['start', '--project', PROJECT, '--dispatcher', stack.dispatcher_url, PROMPT])
  expect(started.code, started.out + started.err + flowstackLog).toBe(0)
  const runId = started.out.match(/^Run (\S+) is queued\.$/m)![1]
  // Its link is the Run page in Console.
  expect(started.out).toContain(`${new URL(baseURL!).origin}/${ORG}/runs/${runId}`)

  // The Execution's child token, as Console made it for the Dispatcher.
  const token = await (async () => {
    let tokens: { secret: string; scopes: string[]; agent_session_ids: string[] }[] = []
    await expect.poll(async () => {
      tokens = (await control('GET', '/child-tokens')).child_tokens
      return tokens.length
    }, { message: `the Dispatcher makes the Execution's token\n${flowstackLog}`, timeout: 60_000 }).toBeGreaterThan(0)
    return tokens[tokens.length - 1]
  })()
  expect(token.scopes.sort()).toEqual(['sessions:read', 'sessions:write'])
  expect(token.agent_session_ids).toHaveLength(1)
  const session = token.agent_session_ids[0]
  const agent = (path: string, init: RequestInit = {}) => fetch(`${INGEST_URL}/api/agent/v1/agent-sessions/${path}`,
    { ...init, headers: { Authorization: `Bearer ${token.secret}` } })

  // The Runs page and the Run page show the running Run from its events.
  await expect(async () => {
    await memberPage.goto(`/${ORG}/runs`)
    await expect(memberPage.locator(`tr[data-run="${runId}"]`)).toContainText('Running', { timeout: 1_000 })
  }).toPass({ timeout: 60_000, intervals: [1_000, 2_000] })
  await expect(memberPage.locator(`tr[data-run="${runId}"]`)).toContainText(PROMPT)
  await memberPage.locator(`tr[data-run="${runId}"]`).getByRole('link', { name: PROMPT }).click()
  await expect(memberPage).toHaveURL(new RegExp(`/${ORG}/runs/${runId}`))
  await expect(memberPage.getByText(runId, { exact: true })).toBeVisible()
  await expect(memberPage.locator('[data-section="events"] [data-event="execution_started"]')).toHaveCount(1)

  // The download for a resume is equal byte for byte to the redacted
  // upload: the agent's files with the secret as '*'.
  await expect.poll(() => existsSync(join(kept, `${session}.jsonl`)), { message: 'the agent keeps its files', timeout: 60_000 }).toBe(true)
  const uploaded = filesIn(kept)
  const stars = '*'.repeat(secret.length)
  const redacted = Object.fromEntries(Object.entries(uploaded).map(([path, bytes]) => [path, bytes.toString('utf8').replaceAll(secret, stars)]))
  expect(redacted[`${session}.jsonl`]).toContain(`key is ${stars}.`)
  const downloads = join(work, 'download')
  await expect(async () => {
    const resp = await agent(`${session}/download`)
    expect(resp.status).toBe(200)
    rmSync(downloads, { recursive: true, force: true })
    mkdirSync(downloads, { recursive: true })
    execFileSync('tar', ['-x', '--warning=no-unknown-keyword', '-C', downloads], { input: Buffer.from(await resp.arrayBuffer()) })
    const got = Object.fromEntries(Object.entries(filesIn(downloads)).map(([path, bytes]) => [path, bytes.toString('utf8')]))
    expect(got).toEqual(redacted)
  }).toPass({ timeout: 60_000, intervals: [1_000, 2_000] })

  // The token works only for its own Agent session.
  expect((await agent(`${session}/files`)).status).toBe(200)
  const other = randomUUID()
  expect((await agent(`${other}/files`)).status).toBe(403)
  expect((await agent(`${other}/files`, { method: 'POST' })).status).toBe(403)
  expect((await agent(`${other}/download`)).status).toBe(403)

  // The Stop button stops the Run.
  memberPage.once('dialog', dialog => dialog.accept())
  await memberPage.locator('[data-action="stop-run"]').click()
  await expect(async () => {
    await memberPage.reload()
    await expect(memberPage.locator('[data-section="events"] [data-event="execution_ended"]')).toHaveCount(1, { timeout: 1_000 })
  }).toPass({ timeout: 60_000, intervals: [1_000, 2_000] })
  await expect(memberPage.getByText('Stopped', { exact: true }).first()).toBeVisible()
  await expect(memberPage.locator('[data-section="events"] [data-event="stop"]')).toHaveCount(1)
  // The stop is an intervention by the member who pressed the button.
  await expect(memberPage.locator('[data-section="interventions"] [data-event="stop"]')).toContainText(`by ${MEMBER.email}`)
  await expect(memberPage.locator('[data-action="stop-run"]')).toHaveCount(0)

  // After the Execution, its token stops working.
  await expect.poll(async () => (await agent(`${session}/download`)).status,
    { message: 'the Dispatcher revokes the token in the end step', timeout: 30_000 }).toBe(401)

  await memberPage.goto(`/${ORG}/runs`)
  await expect(memberPage.locator(`tr[data-run="${runId}"]`)).toContainText('Stopped')
  await memberPage.context().close()
  expect(errors).toEqual([])
})
