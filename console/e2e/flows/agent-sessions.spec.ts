import { test, expect, type Browser, type Locator, type Page } from '@playwright/test'
import { execFileSync, spawn } from 'node:child_process'
import { randomUUID } from 'node:crypto'
import { existsSync, mkdirSync, readFileSync, rmSync } from 'node:fs'
import { join, resolve } from 'node:path'
import { ADMIN, MEMBER, ORG } from '../support/data'
import { EFFORT, fakeAnthropicKey, longSession, MODEL, openPrSession, shortSession, SUBAGENT_EFFORT, SUBAGENT_MODEL, writeSession, type FixtureSession } from '../support/agentTranscripts'
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
// tool output, and a secret shown as '*'. Each row and the page show the
// Agent session's title: the person's /rename, else the start of the first
// person message.
//
// The second test captures two open-pr Agent sessions of one PR, and checks
// its Review record on the Review records pages and over MCP.
//
// OAuth needs a Console URL: the flow sets this stack's own, and puts the
// old one back after. The flow stack's ClickHouse keeps the rows of earlier
// runs, so each run uses its own repositories under one captured entry.

const RUN = Date.now().toString(36)
const ENTRY = 'flows.example/flowteam/*'
const APP = `flows.example/flowteam/app-${RUN}`
const TOOLS = `flows.example/flowteam/tools-${RUN}`
const ELSEWHERE = `elsewhere.example/other/notes-${RUN}`
const SHOP = `flows.example/flowteam/shop-${RUN}`
const TOKEN_NAME = `e2e review records ${RUN}`
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
  rails(`ApiToken.where(name: ${JSON.stringify(TOKEN_NAME)}).delete_all`)
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
  const longTitle = `Cart code hunt, with a title that is too long to show in one line of the Agent sessions list ${RUN}`
  const long = longSession(appDir, {
    personText: `Find the cart code. My key is ${secret} for the test server.`, title: longTitle,
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
  const status = execFileSync(cli, ['capture', 'status', '--all'], { env: admin.env, stdio: 'pipe' }).toString()
  expect(status).toMatch(new RegExp(`${long.id}\\s+uploaded\\s+${APP}`))
  expect(status).toMatch(new RegExp(`${tools.id}\\s+uploaded\\s+${TOOLS}`))
  expect(status).toMatch(new RegExp(`${notCaptured.id}\\s+not captured`))

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
  // The Model column has the model and the effort of the latest reply.
  await expect(longRow).toContainText(`${MODEL} · ${EFFORT}`)
  await expect(longRow).toContainText('680')
  await expect(table.locator(`tr[data-session="${members.id}"]`)).toContainText(MEMBER_NAME)

  // The title comes first: the person's /rename, on one line cut with an
  // ellipsis, with all of it as the hover text; else the start of the
  // first person message.
  const longTitleLink = longRow.locator('td[data-cell="title"] a')
  await expect(longTitleLink).toHaveText(longTitle)
  await expect(longTitleLink).toHaveAttribute('title', longTitle)
  await expect(longTitleLink).toHaveCSS('text-overflow', 'ellipsis')
  await expect.poll(() => longTitleLink.evaluate(a => a.scrollWidth > a.clientWidth), { message: 'the long title is cut' }).toBe(true)
  await expect(table.locator(`tr[data-session="${members.id}"] td[data-cell="title"] a`)).toHaveText('Fix the cart total.')

  // A sortable heading sorts the list at EventStore, before its limit: the
  // most tokens first, then the fewest. The list opens with the latest
  // activity first. The order is in the URL, and a filter keeps it.
  const orderedIds = () => table.locator('tr[data-session]').evaluateAll(rows => rows.map(r => r.getAttribute('data-session')!))
  await expect(table.getByRole('columnheader', { name: 'Last activity' })).toHaveAttribute('aria-sort', 'descending')
  // The last activity reads as how long ago, with the time in its hover text.
  const last = table.locator(`tr[data-session="${members.id}"] td[data-cell="last"] time`)
  await expect(last).toHaveText(/ ago$/)
  await expect(last).toHaveAttribute('title', /^[A-Z][a-z]{2} \d{1,2}, \d{2}:\d{2}:\d{2}$/)
  const tokensHead = table.getByRole('columnheader', { name: 'Tokens' })
  await expect(tokensHead).not.toHaveAttribute('aria-sort')
  await tokensHead.getByRole('button', { name: 'Tokens' }).click()
  await expect(page).toHaveURL(/[?&]sort=tokens&direction=desc(&|$)/)
  await expect(tokensHead).toHaveAttribute('aria-sort', 'descending')
  await expect.poll(orderedIds).toEqual([long.id, members.id])
  await tokensHead.getByRole('button', { name: 'Tokens' }).click()
  await expect(page).toHaveURL(/[?&]sort=tokens&direction=asc(&|$)/)
  await expect(tokensHead).toHaveAttribute('aria-sort', 'ascending')
  await expect(table.getByRole('columnheader', { name: 'Last activity' })).not.toHaveAttribute('aria-sort')
  await expect.poll(orderedIds).toEqual([members.id, long.id])

  // The person filter, with the repository filter: one person's rows.
  await choose(page, 'Person', MEMBER_NAME)
  await expect.poll(() => rowIds(page)).toEqual([members.id])
  await expect(page).toHaveURL(/[?&]sort=tokens&direction=asc(&|$)/)
  await expectTotals(page, { 'Agent sessions': '1', Tokens: '150', Input: '5' })
  await choose(page, 'Person', ADMIN_NAME)
  await expect.poll(() => rowIds(page)).toEqual([long.id])
  await expectTotals(page, { 'Agent sessions': '1', Tokens: '680', Input: '50', Output: '90', 'Cache read': '410', 'Cache write': '130' })

  // The other repository.
  await choose(page, 'Repository', TOOLS)
  await expect.poll(() => rowIds(page)).toEqual([tools.id])
  await expect(table.locator(`tr[data-session="${tools.id}"] td[data-cell="title"] a`)).toHaveText('Update the build script.')
  await expectTotals(page, { 'Agent sessions': '1', Tokens: '154' })

  // The Agent session page of the long session. A click anywhere on the
  // row opens it, not only on the name: here on the start time.
  await choose(page, 'Repository', APP)
  await expect.poll(() => rowIds(page)).toEqual([long.id])
  // The person's cell is plain text, not a link of its own: the row opens
  // the Agent session, and the title is the row's link.
  const personCell = table.locator(`tr[data-session="${long.id}"] td`).nth(3)
  await expect(personCell).toHaveText(ADMIN_NAME)
  await expect(personCell.getByRole('link')).toHaveCount(0)
  await table.locator(`tr[data-session="${long.id}"] td`).nth(1).click()
  await expect(page).toHaveURL(new RegExp(`/${ORG}/agent_sessions/${long.id}`))
  await expect(page.getByText(long.id, { exact: true })).toBeVisible()
  await expect(page.getByRole('heading', { level: 1 })).toHaveText(longTitle)

  // The header and each person step name the person whose CLI uploaded it.
  const header = page.locator('[data-section="agent-session-header"]')
  const fact = (label: string) => header.locator('div > div').filter({ hasText: new RegExp(`^${label}$`) }).locator('xpath=following-sibling::div')
  await expect(fact('Person')).toHaveText(ADMIN_NAME)
  // The model and the effort of the main transcript's latest reply, not
  // the subagent's.
  await expect(fact('Model')).toHaveText(MODEL)
  await expect(fact('Effort')).toHaveText(EFFORT)

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
  // It is named by the description that the main agent gave it.
  await subagent.getByRole('button', { name: /^Subagent: Find the cart code/ }).click()
  await expect(subagent.getByText('It is in cart.go.', { exact: true })).toBeVisible()
  await expect(subagent.locator('[data-step="agent"] [data-model]')).toHaveText(`${SUBAGENT_MODEL} · ${SUBAGENT_EFFORT}`)
  // Inside the subagent its steps need no badge that names it again.
  await expect(subagent.getByText(`subagent ${subagentId}`, { exact: true })).toHaveCount(0)
  // The subagent's prompt is not from a person, so it has no person's name.
  const prompt = subagent.locator('[data-step="not from a person"]')
  await expect(prompt).toContainText('Find the cart code.')
  await expect(prompt).toContainText('Not from a person')
  await expect(prompt).not.toContainText(ADMIN_NAME)
  await expect(subagent.locator('[data-step="person"]')).toHaveCount(0)

  // The /compact shows once, as the person typed it, with Claude Code's
  // output under it and no colour codes of the terminal. The caveat for
  // the model does not show. The summary is one folded line, and its tags
  // show as headings.
  const transcript = page.locator('[data-transcript="main"]')
  const command = transcript.locator('[data-step="person"][data-command]')
  await expect(command).toHaveCount(1)
  await expect(command).toContainText('/compact')
  await expect(command.locator('[data-command-output]')).toHaveText(/^⎿\s*Compacted \(ctrl\+o to see full summary\)$/)
  await expect(transcript.locator('[data-step="person"]').filter({ hasText: '/compact' })).toHaveCount(1)
  await expect(transcript).not.toContainText('[2m')
  await expect(transcript).not.toContainText('Caveat')
  const summary = transcript.locator('[data-compaction-summary]')
  await expect(summary).toHaveCount(1)
  await expect(summary).toContainText('Compaction summary')
  await expect(summary).not.toContainText('The person asked where the cart code is.')
  await summary.getByRole('button', { name: /Compaction summary/ }).click()
  await expect(summary).toContainText('The person asked where the cart code is.')
  await expect(summary.locator('strong').first()).toHaveText('Tools used before summary')
  await expect(summary).not.toContainText('<tools-used-before-summary>')

  // The Bash output is past the Log limit: cut, and 'Show full' reads the
  // whole raw line, which has the end marker and the second secret redacted.
  // A tool call shows as one line, its description; it opens on a click.
  const bash = page.locator('[data-step="tool"][data-tool="Bash"]')
  await expect(bash).toContainText('Print many lines')
  await expect(bash.getByText('Output', { exact: true })).toHaveCount(0)
  await bash.getByRole('button', { name: /Print many lines/ }).click()
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

  // With tool calls turned off, the tool calls go, and the subagent that
  // the Agent tool call started stays, with its name. The choice stays
  // after a reload. The two choices are switches: a click on the label
  // turns one, and Space turns the one with the focus.
  const toolCalls = page.getByRole('switch', { name: 'Show tool calls' })
  const thinking = page.getByRole('switch', { name: 'Show agent thinking' })
  await expect(toolCalls).toHaveAttribute('aria-checked', 'true')
  await expect(thinking).toHaveAttribute('aria-checked', 'true')
  await page.getByText('Show tool calls', { exact: true }).click()
  await expect(toolCalls).toHaveAttribute('aria-checked', 'false')
  // An on switch looks on: its fill is not the fill of an off switch, also
  // in the dark theme.
  const fill = (l: Locator) => l.evaluate(e => getComputedStyle(e).backgroundColor)
  expect(await fill(thinking)).not.toBe(await fill(toolCalls))
  await expect(bash).toHaveCount(0)
  await expect(subagent.getByRole('button', { name: /^Subagent: Find the cart code/ })).toBeVisible()
  await expect(person).toBeVisible()
  await page.reload()
  await expect(toolCalls).toHaveAttribute('aria-checked', 'false')
  await expect(bash).toHaveCount(0)
  await toolCalls.focus()
  await page.keyboard.press('Space')
  await expect(toolCalls).toHaveAttribute('aria-checked', 'true')
  await expect(bash).toBeVisible()

  // The order: the oldest step first by default, as a chat reads. The
  // newest order loads the end of the timeline and shows the last reply
  // first, with the subagent still inside its tool call. The choice stays
  // after a reload.
  const main = page.locator('[data-transcript="main"]')
  const firstStep = main.locator(':scope > [data-step]').first()
  await expect(page.getByRole('button', { name: 'Oldest first' })).toHaveAttribute('aria-pressed', 'true')
  await expect(firstStep).toHaveAttribute('data-step', 'person')
  await page.getByRole('button', { name: 'Newest first' }).click()
  await expect(main).toHaveAttribute('data-order', 'newest')
  await expect(firstStep).toHaveAttribute('data-step', 'agent')
  await expect(firstStep).toContainText('The cart code is in cart.go, and the command printed its lines.')
  // Each reply's title has its model and its effort.
  await expect(firstStep.locator('[data-model]')).toHaveText(`${MODEL} · ${EFFORT}`)
  // The agent's reply shows as Markdown: code and bold, with no marks left.
  await expect(firstStep.locator('code')).toHaveText('cart.go')
  await expect(firstStep.locator('strong')).toHaveText('its lines')
  await expect(firstStep).not.toContainText('**')
  // A GitHub table in the reply shows as a table.
  await expect(firstStep.locator('table td').first()).toHaveText('cart.go')
  await expect(firstStep).not.toContainText('| ---')
  await expect(main.locator(':scope > [data-step]').last()).toHaveAttribute('data-step', 'person')
  await expect(agentCall.locator(`[data-subagent="${subagentId}"]`)).toBeVisible()
  await page.reload()
  await expect(page.getByRole('button', { name: 'Newest first' })).toHaveAttribute('aria-pressed', 'true')
  await expect(firstStep).toContainText('The cart code is in cart.go')
  await page.getByRole('button', { name: 'Oldest first' }).click()
  await expect(firstStep).toHaveAttribute('data-step', 'person')

  expect(errors).toEqual([])
})

/**
 * A PR's Review record, as open-pr and update-pr write it (version 2): the
 * first four findings each show one path of a finding, and the rest make
 * the record longer than a Log keeps (64 KiB) when there are many.
 */
function reviewRecord(pr: string, round: number, findings: number): string {
  const f = (n: number) => `${round}.F${n}`
  const scenario = 'The cart page adds the line totals before it applies the discount, so a cart with a coupon shows a total that is too high. '.repeat(3)
  return JSON.stringify({
    version: 2,
    sessions: [{ id: randomUUID(), title: `Cart page ${RUN}` }],
    pr,
    round,
    story: {
      started_from: `The person asked for a cart page, round ${round}.`,
      changes: ['Add the cart page and its total.'],
      reviews: ['Run 1 reviewed the cart page step.'],
      after_review: ['The person asked for the trivial finding to be fixed too.'],
    },
    reviews: [{
      run: 1, skill: 'code-review', trigger: 'step', stage: 'step', step: 'cart page',
      reviewers: [{ name: 'bugs-and-spec', model: 'opus', effort: 'xhigh' }, { name: 'tests', model: 'opus', effort: 'xhigh' }, { name: 'code-quality', model: 'sonnet', effort: 'high' }],
      verifier: { model: 'opus', effort: 'high' },
    }],
    findings: [
      { id: f(1), run: 1, found_by: { kind: 'reviewer', name: 'bugs-and-spec' }, severity: 'high', file: 'cart.go', line: 42,
        title: 'The total skips the discount <img src=x onerror="window.__reviewRecordScript=1">', scenario,
        events: [{ type: 'found' }, { type: 'triage', decision: 'fix', why: 'A wrong total is a real fault.' }, { type: 'fix', files: ['cart.go'] }], outcome: 'fixed' },
      { id: f(2), run: 1, found_by: { kind: 'reviewer', name: 'tests' }, severity: 'low', title: 'No test for an empty cart',
        events: [{ type: 'found' }, { type: 'triage', decision: 'trivial' }, { type: 'verify', verdict: 'CONFIRMED', pushed_back: true },
          { type: 'person', said: 'Fix it anyway', asks: 'fix' }, { type: 'fix', files: ['cart_test.go'] }], outcome: 'fixed' },
      { id: f(3), found_by: { kind: 'person' }, title: 'The total has no currency sign',
        events: [{ type: 'person', said: 'Show the currency.', asks: 'fix' }], outcome: 'open' },
      { id: f(4), found_by: { kind: 'agent' }, title: 'The cart page has no cache',
        events: [{ type: 'agent', found: 'The page reads the prices each time.' }, { type: 'triage', decision: 'defer', why: 'Later step.' }], outcome: 'deferred' },
      ...Array.from({ length: findings - 4 }, (_, i) => ({
        id: f(i + 5), run: 1, found_by: { kind: 'reviewer', name: 'code-quality' }, severity: 'low', title: `Name ${i + 5} is not clear`, scenario,
        events: [{ type: 'found' }, { type: 'triage', decision: 'fix' }, { type: 'fix', files: ['cart.go'] }], outcome: 'fixed',
      })),
    ],
    confirmations: [
      { id: 'C1', run: 1, reviewer: 'security', ask: 'Confirm that the coupon code is checked on the server.', given: true },
      { id: 'C2', run: 1, reviewer: 'security', ask: 'Confirm that the total is not cached per person.', given: false },
      { id: 'C3', run: 1, reviewer: 'ha-and-infrastructure', ask: 'Confirm the deploy order.', given: null },
    ],
  }, null, 2)
}

test('the final reply of an open-pr Agent session gives its Review record, on the Review records pages and over MCP', async ({ page, baseURL }) => {
  test.setTimeout(240_000)
  const errors = watchErrors(page)
  const origin = new URL(baseURL!).origin
  const reviewer = machine('reviewer')
  await page.goto(`/${ORG}/review_records`)
  await signIn(page, reviewer, origin)

  // Two rounds of one PR, in two Agent sessions. Round 2 is past the Log
  // limit, so only the parse's own copy keeps all of it. The skill's text
  // is a user line with an example record, which is not a Review record.
  const shopDir = gitRepository(SHOP)
  const pr = `https://github.example/flowteam/shop-${RUN}/pull/7`
  const examplePr = `https://github.example/flowteam/shop-${RUN}/pull/EXAMPLE`
  const round1 = reviewRecord(pr, 1, 6)
  const round2 = reviewRecord(pr, 2, 240)
  expect(Buffer.byteLength(round2)).toBeGreaterThan(64 * 1024)
  const example = reviewRecord(examplePr, 1, 4)
  const first = openPrSession(shopDir, { exampleRecord: example, record: round1, minutesAgo: 30 })
  const second = openPrSession(shopDir, { exampleRecord: example, record: round2, minutesAgo: 10 })
  capture(reviewer, first, shopDir)
  capture(reviewer, second, shopDir)

  // The Review records page, from the navigation, once EventStore has
  // parsed the uploads: one row for the PR, its newest round.
  const table = page.getByRole('table', { name: 'Review records' })
  const row = table.locator(`tr[data-review-record="${pr}"]`)
  await expect(async () => {
    await page.goto(`/${ORG}/agent_sessions`)
    await page.getByRole('navigation', { name: 'Main' }).getByRole('link', { name: 'Review records' }).click()
    await expect(page).toHaveURL(new RegExp(`/${ORG}/review_records$`))
    await expect(row.locator('[data-cell="round"]')).toHaveText('2(1 earlier)', { timeout: 1_000 })
  }).toPass({ timeout: 90_000, intervals: [1_000, 2_000, 3_000] })
  await expect(row.locator('[data-cell="findings"]')).toHaveText('240')
  await expect(row.locator('[data-cell="fixed"]')).toHaveText('238')
  await expect(row.locator('[data-cell="missed"]')).toHaveText('2')
  await expect(row.locator('[data-cell="not-given"]')).toHaveText('2')
  await expect(row).toContainText(`Cart page ${RUN}`)
  await expect(row.getByRole('link', { name: 'Open the PR' })).toHaveAttribute('href', pr)
  await expect(row.getByRole('link', { name: second.id.slice(0, 8) })).toHaveAttribute('href', `/${ORG}/agent_sessions/${second.id}`)
  await expect(table.locator(`tr[data-review-record="${examplePr}"]`)).toHaveCount(0)
  await shoot(page, 'review-records-list')

  // The record's page: the story, the review run, each finding with its
  // decisions in order, the confirmations, and the other round. The text
  // is the agent's, so it shows as text, never as HTML.
  await row.getByRole('link', { name: pr }).click()
  await expect(page).toHaveURL(new RegExp(`/${ORG}/review_records/record\\?key=`))
  await expect(page.getByRole('heading', { level: 1 })).toContainText(pr)
  await expect(page.locator('[data-section="review-record-header"]')).toContainText('240 (238 fixed, 2 missed by the reviews)')
  await expect(page.getByRole('region', { name: 'Story' })).toContainText('The person asked for a cart page, round 2.')
  await expect(page.getByRole('table', { name: 'Reviews' }).locator('tr[data-run="1"]'))
    .toContainText('bugs-and-spec (opus, xhigh), tests (opus, xhigh), code-quality (sonnet, high)')
  const finding = (id: string) => page.getByRole('table', { name: 'Findings' }).locator(`tr[data-finding="${id}"]`)
  await expect(finding('2.F2').locator('[data-cell="decisions"]')).toHaveText('trivial, verifier pushed back, person asks fix, fixed')
  await expect(finding('2.F2').locator('[data-cell="found-by"]')).toHaveText('reviewer tests')
  await expect(finding('2.F2').locator('[data-cell="outcome"]')).toHaveText('fixed')
  await finding('2.F2').getByText('Details').click()
  await expect(finding('2.F2')).toContainText('“Fix it anyway”')
  await expect(finding('2.F3').locator('[data-cell="found-by"]')).toHaveText('personmissed')
  await expect(finding('2.F3').locator('[data-cell="outcome"]')).toHaveText('open')
  await expect(finding('2.F4').locator('[data-cell="decisions"]')).toHaveText('agent answered, defer')
  await expect(finding('2.F1')).toContainText('<img src=x onerror="window.__reviewRecordScript=1">')
  await expect(finding('2.F1').locator('img')).toHaveCount(0)
  expect(await page.evaluate(() => (window as unknown as Record<string, unknown>).__reviewRecordScript)).toBeUndefined()
  // The last finding is past the 64 KiB of a Log: the record is whole.
  await expect(finding('2.F240')).toContainText('Name 240 is not clear')
  const confirmations = page.getByRole('table', { name: 'Confirmations' })
  await expect(confirmations.locator('tr[data-confirmation="C1"]')).toContainText('Given')
  await expect(confirmations.locator('tr[data-confirmation="C2"]')).toContainText('Not given')
  await expect(confirmations.locator('tr[data-confirmation="C3"]')).toContainText('Not known')
  await shoot(page, 'review-record-page')
  const rounds = page.getByRole('region', { name: 'Rounds of this PR' })
  await expect(rounds.locator('li[data-round="2"]')).toContainText('Round 2 (shown)')
  await rounds.getByRole('link', { name: 'Round 1' }).click()
  await expect(page).toHaveURL(new RegExp(`/${ORG}/review_records/record\\?id=`))
  await expect(page.locator('[data-section="review-record-header"]')).toContainText('6 (4 fixed, 2 missed by the reviews)')
  await expect(page.getByRole('table', { name: 'Findings' }).locator('tr[data-finding]')).toHaveCount(6)

  // Over MCP, with a token that only reads: the same row, and the record as
  // the agent wrote it, with its findings in pages.
  const secret = rails(`
    org = Organization.find_by!(slug: ${JSON.stringify(ORG)})
    admin = User.find_by!(email: ${JSON.stringify(ADMIN.email)})
    print ApiToken.issue!(user: admin, name: ${JSON.stringify(TOKEN_NAME)}, scopes: ["mcp:read"], organization_id: org.id).last
  `)
  const tool = async (name: string, args: object) => {
    const resp = await fetch(`${origin}/mcp`, {
      method: 'POST',
      headers: { Accept: 'application/json, text/event-stream', 'Content-Type': 'application/json', Authorization: `Bearer ${secret}` },
      body: JSON.stringify({ jsonrpc: '2.0', id: 1, method: 'tools/call', params: { name, arguments: { organization: ORG, ...args } } }),
    })
    expect(resp.status, name).toBe(200)
    const body = await resp.json()
    expect(body.result?.isError, name).toBeFalsy()
    const text: string = body.result.content.map((c: { text: string }) => c.text).join('\n')
    return text.split('\nConsole: ')
  }
  const [list, listLink] = await tool('list_review_records', {})
  const listed = JSON.parse(list).review_records.find((r: { key: string }) => r.key === pr)
  expect(listed).toMatchObject({ pr, round: 2, earlier: 1, findings: 240, fixed: 238, missed: 2, confirmations_not_given: 2, session_id: second.id })
  expect(listLink).toBe(`${origin}/${ORG}/review_records`)
  const [got, gotLink] = await tool('get_review_record', { pr })
  const whole = JSON.parse(round2)
  const answer = JSON.parse(got)
  expect(answer.record, 'the record with its first 30 findings').toEqual({ ...whole, findings: whole.findings.slice(0, 30) })
  expect(answer).toMatchObject({ findings_total: 240, next_findings_from: 30 })
  expect(got.length).toBeLessThan(100_000)
  expect(answer.rounds.map((r: { round: number }) => r.round)).toEqual([2, 1])
  expect(gotLink).toBe(`${origin}/${ORG}/review_records/record?key=${encodeURIComponent(pr)}`)
  const findings = [...answer.record.findings]
  let from: number | null = answer.next_findings_from
  while (from !== null) {
    const [next] = await tool('get_review_record', { pr, findings_from: from, findings_limit: 100 })
    const nextPage = JSON.parse(next)
    findings.push(...nextPage.record.findings)
    from = nextPage.next_findings_from
  }
  expect(findings, 'all the findings, page by page').toEqual(whole.findings)

  expect(errors).toEqual([])
})
