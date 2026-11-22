import { expect, type Browser, type Page } from '@playwright/test'
import { execFileSync, spawn, spawnSync, type ChildProcessWithoutNullStreams } from 'node:child_process'
import { existsSync, mkdirSync, readdirSync, readFileSync, rmSync, statSync, writeFileSync } from 'node:fs'
import { join, relative, resolve } from 'node:path'
import { createInterface } from 'node:readline'
import { ADMIN, ORG, PROJECT } from './data'
import { newContext } from './video'

// The factory's Seam 5 flows (runs.spec.ts, factory-integration.spec.ts):
// the real Dispatcher and the real wrapper against this stack's Console,
// EventStore and ClickHouse. This file builds the factory's flow stack
// (factory/internal/flowstack: the Dispatcher with the Seam 3 fakes for the
// Runner, the Harness, the Forge and the Tracker), starts it, and gives the
// flow the real `solidtrace` CLI, signed in with the device flow, and the
// flow stack's control API.

export const INGEST_URL = process.env.E2E_INGEST_URL ?? 'http://localhost:4000'
export const consoleDir = resolve(__dirname, '../..')

/** Runs Ruby in the dev Console and answers what it prints. */
export const rails = (code: string) => execFileSync('mise', ['exec', '--', 'bin/rails', 'runner', code], {
  cwd: consoleDir, stdio: 'pipe', timeout: 120_000,
}).toString().trim()

/** The flow stack: its Dispatcher's URL, its control API and the fake Forge's token. */
export type Stack = { dispatcher_url: string; control_url: string; forge_token: string }

/** One person's computer: the CLI's config folder and its environment. */
export type Machine = { config: string; env: NodeJS.ProcessEnv }

/** One child token that Console made for the Dispatcher (GET /child-tokens of the flow stack). */
export type ChildToken = { secret: string; scopes: string[]; agent_session_ids: string[]; project_id: string }

/** Each regular file under dir, by its path in dir. */
export function filesIn(dir: string): Record<string, Buffer> {
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

/** A page of a new browser context, signed in as the user. */
export async function signedInPage(browser: Browser, user: { email: string; password: string }): Promise<Page> {
  const context = await newContext(browser, { storageState: { cookies: [], origins: [] } })
  const page = await context.newPage()
  await page.goto('/login')
  await page.fill('input[type=email]', user.email)
  await page.fill('input[type=password]', user.password)
  await page.click('button[type=submit]')
  await page.waitForURL(url => !url.pathname.startsWith('/login'))
  return page
}

/**
 * One factory flow: `name` makes its work folder and its log file, and
 * `entry` is the captured-repository entry that the CLI's sign-in reads.
 * Call start() in beforeAll and stop() in afterAll.
 */
export function factoryFlow(name: string, entry: string) {
  const run = Date.now().toString(36)
  const work = join(consoleDir, 'tmp', `e2e-${name}-${run}`)
  const cli = join(work, 'solidtrace')
  const flowstackBin = join(work, 'flowstack')

  let stack: Stack | undefined
  let flowstack: ChildProcessWithoutNullStreams | undefined
  let flowstackLog = ''
  let dispatcherTokenId = 0
  /** The dev box's settings before the flow changes them; unset until start() reads them. */
  let oldSettings: { console_url: string; dispatcher_url: string } | undefined

  const self = {
    run,
    work,
    cli,
    /** The Dispatcher's log so far, to show with a failure. */
    log: () => flowstackLog,
    stack: () => stack!,

    machine(who: string): Machine {
      const config = join(work, who, 'solidtrace')
      mkdirSync(config, { recursive: true })
      return { config, env: { ...process.env, SOLIDTRACE_CONFIG_DIR: config, DISPLAY: '', WAYLAND_DISPLAY: '' } }
    },

    /** Runs the person's CLI; the flow checks the exit code itself. */
    solidtrace(m: Machine, args: string[], input = '') {
      const r = spawnSync(cli, args, { env: m.env, input, timeout: 60_000 })
      return { code: r.status, out: r.stdout.toString(), err: r.stderr.toString() }
    },

    /** `solidtrace login --device`: the flow types the CLI's code on the device page and approves. */
    async signIn(page: Page, m: Machine, origin: string) {
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
    },

    /** The flow stack's control API (loopback, for the flow only). */
    async control(method: string, path: string, body?: unknown) {
      const resp = await fetch(stack!.control_url + path, {
        method, headers: { 'Content-Type': 'application/json' }, body: body === undefined ? undefined : JSON.stringify(body),
      })
      expect(resp.ok, `${method} ${path}: ${resp.status} ${await resp.clone().text()}`).toBe(true)
      return resp.status === 204 ? undefined : resp.json()
    },

    /** Each child token that Console made for the Dispatcher so far, oldest first. */
    async childTokens(): Promise<ChildToken[]> {
      return (await self.control('GET', '/child-tokens')).child_tokens
    },

    /**
     * Builds the CLI and the flow stack, turns Agent sessions on, makes the
     * Dispatcher's token, starts the flow stack, and sets the Dispatcher URL.
     * Call it first in beforeAll, with a 420 s timeout.
     */
    async start(baseURL: string) {
      // Read the dev box's settings first, before any step that can fail, so
      // that stop() always puts back the real values and never clears them.
      oldSettings = JSON.parse(rails(`
        settings = InstanceSetting.current
        print({ console_url: settings.console_url.to_s, dispatcher_url: settings.dispatcher_url.to_s }.to_json)
      `))
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
      const origin = new URL(baseURL).origin
      const made = JSON.parse(rails(`
        InstanceSetting.current.update!(console_url: ${JSON.stringify(origin)})
        org = Organization.find_by!(slug: ${JSON.stringify(ORG)})
        admin = User.find_by!(email: ${JSON.stringify(ADMIN.email)})
        project = org.agent_project
        result = if project
          AgentSessionsSettings.save(project: project, user: admin, entries: project.captured_repositories | [${JSON.stringify(entry)}], confirmed: true)
        else
          AgentSessionsSettings.turn_on(organization: org, user: admin, entries: [${JSON.stringify(entry)}], confirmed: true)
        end
        raise result.errors.inspect unless result.done?
        token, secret = ApiToken.issue!(user: admin, name: "Flow Dispatcher ${run}", scopes: Oauth::Scopes::DISPATCHER, organization_id: org.id)
        print({ id: token.id, secret: secret }.to_json)
      `))
      dispatcherTokenId = made.id

      flowstack = spawn(flowstackBin, [], {
        cwd: work,
        env: { ...process.env, FLOWSTACK_CONSOLE_URL: origin, FLOWSTACK_INGEST_URL: INGEST_URL, DISPATCHER_SOLIDTRACE_TOKEN: made.secret },
      })
      flowstack.stderr.on('data', d => { flowstackLog += d })
      const exited = new Promise<never>((_, fail) => flowstack!.on('exit', code => fail(new Error(`flowstack exited ${code}:\n${flowstackLog}`))))
      const line = new Promise<string>(done => createInterface({ input: flowstack!.stdout }).once('line', done))
      stack = JSON.parse(await Promise.race([line, exited]))

      rails(`InstanceSetting.current.update!(dispatcher_url: ${JSON.stringify(stack!.dispatcher_url)})`)
      await self.control('POST', '/repos', { name: `example/${PROJECT}` })
    },

    /** Stops the flow stack, and puts back what start() read. Call it in afterAll. */
    async stop() {
      if (flowstack && flowstack.exitCode === null) {
        const gone = new Promise(done => flowstack!.on('exit', done))
        flowstack.stdin.end()
        await Promise.race([gone, new Promise(done => setTimeout(done, 20_000))])
        flowstack.kill('SIGKILL')
      }
      // The Dispatcher's log, to read after a failure.
      writeFileSync(join(consoleDir, 'tmp', `e2e-${name}-flowstack.log`), flowstackLog)
      // Put back only what start() read: a failed read changed nothing.
      const restore: string[] = []
      if (oldSettings) {
        const value = (v: string) => (v ? JSON.stringify(v) : 'nil')
        restore.push(`InstanceSetting.current.update!(console_url: ${value(oldSettings.console_url)}, dispatcher_url: ${value(oldSettings.dispatcher_url)})`)
      }
      if (dispatcherTokenId) restore.push(`ApiToken.where(id: ${dispatcherTokenId}).destroy_all`)
      if (restore.length) rails(restore.join('\n'))
      rmSync(work, { recursive: true, force: true })
    },
  }
  return self
}
