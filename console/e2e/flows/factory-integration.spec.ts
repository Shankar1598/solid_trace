import { test, expect, type Page } from '@playwright/test'
import { existsSync, readFileSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { ORG, PROJECT } from '../support/data'
import { factoryFlow, rails, type ChildToken } from '../support/factoryFlow'
import { last24h, openLogs, runBy } from '../support/logs'
import { failedLoad, shoot, useTheme, watchErrors } from '../support/screens'

// Seam 5: SolidTrace starts Runs through the Factory Integration, against
// the real Console, the real Dispatcher (the flow stack of
// factory/internal/flowstack) and the real MCP server.
//
// An Admin adds a 'Factory' Integration (Console makes its Webhook sender
// through the Dispatcher) and makes a Monitor that tells it. The Monitor
// fires, and the Dispatcher starts one Run, with the Monitor's cause report
// on its Run page. A new Issue starts an Issue Run: the Execution's token
// (mcp:read, limited to the Project) reads the Issue's Project over
// POST /mcp, is refused for another Project and for the Agent Project, and
// stops working after the Execution; the cause report is a comment on the
// Issue by 'Factory'. The starter 'Factory' Dashboard has its four Charts
// and the Project filter.

const ENTRY = 'flows.example/flowteam/*'
const flow = factoryFlow('factory-integration', ENTRY)
const RUN = flow.run
const SOURCE = `['${PROJECT}']`
const REPOSITORY = `bitbucket.org/example/${PROJECT}`
const INTEGRATION = `Flow Factory ${RUN}`
const MONITOR = `Flow Factory monitor ${RUN}`
const ISSUE = `Flow Factory issue ${RUN}`
const MONITOR_CAUSE = `The monitor's query counts the Logs of every service. Cause ${RUN}.`
const ISSUE_CAUSE = `The cart rounds each line before the sum. Cause ${RUN}.`
const OTHER = `flow-other-${RUN}`

/** What the flow reads from Console before it changes anything, and what it makes. */
let facts: { project_id: string; agent_slug: string; dashboard_before: boolean }
let origin = ''

const admin = () => flow.machine('admin')

// The scripted Harness of one Execution (the same lines as the Seam 3
// tests of the wrapper): it takes the task, runs the steps, and ends.
const takeTask = JSON.stringify({ fake: 'await_message' })
const init = JSON.stringify({ type: 'system', subtype: 'init', session_id: '', uuid: '', cwd: '', permissionMode: '', model: 'claude-haiku-5-5', tools: ['Bash', 'Read'] })
const say = (text: string) => JSON.stringify({
  type: 'assistant', session_id: '', uuid: '', parent_tool_use_id: null,
  message: { id: 'msg_01Flow', type: 'message', role: 'assistant', model: 'claude-haiku-5-5', stop_reason: 'end_turn', content: [{ type: 'text', text }] },
})
const program = (...args: string[]) => JSON.stringify({ fake: 'run', args })
const shell = (command: string) => program('sh', '-c', command)
const endResult = JSON.stringify({ type: 'result', subtype: 'success', is_error: false, num_turns: 1, result: 'Done.', stop_reason: 'end_turn', session_id: '', uuid: '', total_cost_usd: 0 })
const causeReport = (summary: string, cause: string) => program('solidtrace', 'report', '--outcome', 'done', '--summary', summary, '--cause-report', cause)

/** A rows of the Runs page whose text has the words. */
const runRows = (page: Page, text: string) => page.locator('tr[data-run]').filter({ hasText: text })

/** One call of POST /mcp with a token, as an MCP client makes it. */
async function mcp(token: string, method: string, params: object = {}) {
  const resp = await fetch(`${origin}/mcp`, {
    method: 'POST',
    headers: { Accept: 'application/json, text/event-stream', 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
    body: JSON.stringify({ jsonrpc: '2.0', id: 1, method, params }),
  })
  const body = await resp.json().catch(() => null)
  const text: string = body?.result?.content?.map((c: { text?: string }) => c.text ?? '').join('\n') ?? ''
  return { status: resp.status, isError: Boolean(body?.result?.isError), text, body }
}
const tool = (token: string, name: string, args: object) => mcp(token, 'tools/call', { name, arguments: args })

test.describe.configure({ mode: 'serial' })

test.beforeAll(async ({ baseURL }) => {
  test.setTimeout(420_000)
  origin = new URL(baseURL!).origin
  await flow.start(baseURL!)
  facts = JSON.parse(rails(`
    org = Organization.find_by!(slug: ${JSON.stringify(ORG)})
    project = org.projects.find_by!(slug: ${JSON.stringify(PROJECT)})
    org.projects.create!(name: ${JSON.stringify(OTHER)})
    print({ project_id: project.id.to_s, agent_slug: org.agent_project.slug,
            dashboard_before: org.dashboards.where(name: "Factory").exists? }.to_json)
  `))
})

test.afterAll(async () => {
  try {
    // What the flow made in Console: its Issue, Integration (and the
    // Notifications and Monitors that name it), Project, and the starter
    // Dashboard when this flow caused it.
    rails(`
      org = Organization.find_by!(slug: ${JSON.stringify(ORG)})
      LogMonitor.where(name: ${JSON.stringify(MONITOR)}).destroy_all
      org.integrations.where(name: ${JSON.stringify(INTEGRATION)}).each { |i| Notification.where(integration_id: i.id).delete_all; i.destroy }
      Issue.where(title: ${JSON.stringify(ISSUE)}).destroy_all
      org.projects.where(name: ${JSON.stringify(OTHER)}).destroy_all
      ${facts?.dashboard_before === false ? 'org.dashboards.where(name: "Factory").destroy_all; org.update_columns(factory_dashboard_made_at: nil)' : ''}
    `)
  } finally {
    await flow.stop()
  }
})

test('a Monitor with the Factory Integration starts one Run, with its cause report on the Run page', async ({ page, baseURL }) => {
  test.setTimeout(300_000)
  await useTheme(page, test.info())
  // The dev Console may answer 503 (busy) to the explorer's first run.
  const errors = watchErrors(page, [failedLoad(503)])

  // The Admin signs the CLI in and sets the forge and the Project's repository.
  await page.goto(`/${ORG}/runs`)
  await flow.signIn(page, admin(), origin)
  const dispatcher = ['--dispatcher', flow.stack().dispatcher_url]
  let r = flow.solidtrace(admin(), ['connections', 'set', 'forge', '--kind', 'bitbucket', ...dispatcher], flow.stack().forge_token)
  expect(r.code, r.out + r.err).toBe(0)
  r = flow.solidtrace(admin(), ['projects', 'set', PROJECT, '--repository', REPOSITORY, '--connection', 'forge', ...dispatcher])
  expect(r.code, r.out + r.err).toBe(0)

  // The Admin adds the Factory Integration. It has no URL: Console makes its
  // Webhook sender through the Dispatcher, as the Admin.
  await page.goto(`/${ORG}/settings/integrations/new`)
  await page.getByRole('combobox').first().click()
  await page.getByRole('option', { name: 'Factory', exact: true }).click()
  await page.getByLabel('Name (optional)').fill(INTEGRATION)
  await expect(page.getByLabel('Webhook URL')).toHaveCount(0)
  await page.getByRole('button', { name: 'Add integration' }).click()
  await expect(page.getByText(INTEGRATION).first()).toBeVisible()

  // A Monitor that tells it, made from the explorer.
  await openLogs(page, last24h('query'))
  await page.getByLabel('Query', { exact: true }).fill(`${SOURCE}\n| summarize count() by bin_auto(time)`)
  // The dev Console has few threads: its Query and histogram can meet the
  // busy limit, and then the run answers "busy" and shows no chart. Run again.
  await expect(async () => {
    await runBy(page, () => page.getByLabel('Query', { exact: true }).press('Control+Enter'))
    await expect(page.getByRole('link', { name: 'Create monitor' })).toBeVisible({ timeout: 2_000 })
  }).toPass({ timeout: 60_000 })
  await page.getByRole('link', { name: 'Create monitor' }).click()
  await expect(page.getByRole('heading', { name: 'New monitor' })).toBeVisible()
  await page.getByLabel('Name').fill(MONITOR)
  await page.getByLabel('Run every (minutes)').fill('60')
  await page.getByLabel('Threshold').fill('0')
  await page.getByRole('group', { name: 'Integrations' }).getByRole('checkbox', { name: INTEGRATION }).click()
  await expect(page.locator('[data-monitor-groups]')).toContainText('1 group would fire now.')
  await page.getByRole('button', { name: 'Create monitor' }).click()
  await expect(page.getByRole('heading', { name: MONITOR })).toBeVisible()
  await expect(page.getByText(`Tells ${INTEGRATION} (Factory).`)).toBeVisible()

  // The agent of the Monitor's Run finds a cause and reports it.
  await flow.control('POST', '/agent', {
    lines: [takeTask, init, say('I read the Logs of the Monitor.'),
      causeReport('Found the cause', MONITOR_CAUSE), say('Done.'), endResult, takeTask],
  })

  // Run now fires the Monitor. Console sends the Notification to the
  // Dispatcher, which starts one Run for the Monitor.
  await page.getByRole('button', { name: 'Run now' }).click()
  // Run now waits for the run, which can be slow on a cold dev stack.
  await expect(page.getByText('Ran the Monitor: 1 group firing.')).toBeVisible({ timeout: 60_000 })
  await expect(async () => {
    await page.goto(`/${ORG}/runs`)
    await expect(runRows(page, MONITOR)).toHaveCount(1, { timeout: 1_000 })
  }).toPass({ timeout: 120_000, intervals: [2_000, 3_000] })
  await expect(async () => {
    await page.reload()
    await expect(runRows(page, MONITOR)).toContainText('Done', { timeout: 1_000 })
  }).toPass({ timeout: 120_000, intervals: [2_000, 3_000] })
  await expect(runRows(page, MONITOR)).toHaveCount(1)
  // One Execution only: the Monitor did not start a second Run.
  expect((await flow.control('GET', '/executions')).executions).toHaveLength(1)

  // The Run page shows who started it and the cause report. A Monitor has no
  // comments, so the report is on this page only.
  const row = runRows(page, MONITOR)
  const runId = (await row.getAttribute('data-run'))!
  await row.getByRole('link').first().click()
  await expect(page).toHaveURL(new RegExp(`/${ORG}/runs/${runId}`))
  await expect(page.getByRole('heading', { name: 'Cause report' })).toBeVisible()
  await expect(page.getByText(MONITOR_CAUSE, { exact: true })).toBeVisible()
  await expect(page.getByText(INTEGRATION).first()).toBeVisible()

  expect(errors, flow.log()).toEqual([])
})

test('an Issue Run reads only its Project over MCP, and its cause report is a comment by Factory', async ({ page }) => {
  test.setTimeout(300_000)
  const errors = watchErrors(page)
  const gate = join(flow.work, 'gate')
  const seen = join(flow.work, 'mcp-token-seen')
  const before = (await flow.childTokens()).length

  // The agent copies its MCP token file, waits until the flow has used the
  // token, and then reports the cause.
  await flow.control('POST', '/agent', {
    lines: [takeTask, init, say('I read the Issue.'),
      shell(`cat "$SOLIDTRACE_MCP_TOKEN_FILE" > '${seen}'`),
      shell(`i=0; while [ ! -e '${gate}' ] && [ $i -lt 1500 ]; do sleep 0.2; i=$((i+1)); done`),
      causeReport('Found the cause', ISSUE_CAUSE), say('Done.'), endResult, takeTask],
  })

  // A new Issue in the Project: the job that EventStore's message starts.
  const number = Number(rails(`
    org = Organization.find_by!(slug: ${JSON.stringify(ORG)})
    project = org.projects.find_by!(slug: ${JSON.stringify(PROJECT)})
    issue = project.issues.create!(title: ${JSON.stringify(ISSUE)}, kind: :error, first_seen_at: 1.hour.ago, last_seen_at: Time.current, times_seen: 1)
    IntegrationNotificationJob.perform_later(issue, true)
    print issue.number
  `).split('\n').pop())

  // The Execution gets two child tokens: one for its Agent session, and
  // mcp:read for the Issue's Project only.
  let mcpToken: ChildToken | undefined
  await expect.poll(async () => {
    mcpToken = (await flow.childTokens()).slice(before).find(t => t.scopes.includes('mcp:read'))
    return mcpToken?.project_id
  }, { message: `the Dispatcher makes the Issue Run's MCP token\n${flow.log()}`, timeout: 120_000, intervals: [2_000] }).toBe(facts.project_id)
  const token = mcpToken!
  expect(token.scopes).toEqual(['mcp:read'])
  expect(token.agent_session_ids ?? []).toEqual([])
  // The wrapper gives the agent this very token, in a file.
  await expect.poll(() => existsSync(seen), { message: 'the agent copies its MCP token file', timeout: 60_000 }).toBe(true)
  expect(readFileSync(seen, 'utf8').trim()).toBe(token.secret)

  // It reads the Issue's Project: the tools of one Project, and its Issue.
  const tools = await mcp(token.secret, 'tools/list')
  expect(tools.status).toBe(200)
  const names: string[] = tools.body.result.tools.map((t: { name: string }) => t.name)
  expect(names).toContain('get_issue')
  expect(names).not.toContain('list_projects')
  const issue = await tool(token.secret, 'get_issue', { project: PROJECT, issue: number })
  expect(issue.status).toBe(200)
  expect(issue.isError, issue.text).toBe(false)
  expect(issue.text).toContain(ISSUE)
  const list = await tool(token.secret, 'list_issues', {})
  expect(list.isError, list.text).toBe(false)
  expect(list.text).toContain(ISSUE)

  // Another Project, and the Agent Project, are errors.
  for (const slug of [OTHER.toLowerCase(), facts.agent_slug]) {
    const refused = await tool(token.secret, 'get_issue', { project: slug, issue: 1 })
    expect(refused.isError, `${slug}: ${refused.text}`).toBe(true)
    expect(refused.text).toContain(`This token reads only the Project ${PROJECT}`)
    const logs = await tool(token.secret, 'query_logs', { project: slug, query: '' })
    expect(logs.isError, `${slug}: ${logs.text}`).toBe(true)
    expect(logs.text).toContain('This token reads only the Project')
  }

  // The agent goes on: it reports the cause and the Execution ends.
  writeFileSync(gate, '')
  await expect(async () => {
    await page.goto(`/${ORG}/runs`)
    await expect(runRows(page, ISSUE)).toContainText('Done', { timeout: 1_000 })
  }).toPass({ timeout: 120_000, intervals: [2_000, 3_000] })
  await expect(runRows(page, ISSUE)).toHaveCount(1)
  const runId = (await runRows(page, ISSUE).getAttribute('data-run'))!

  // After the Execution, the token stops working.
  await expect.poll(async () => (await mcp(token.secret, 'tools/list')).status,
    { message: 'the Dispatcher revokes the MCP token in the end step', timeout: 30_000 }).toBe(401)

  // The cause report is a comment on the Issue, by Factory, with a link to the Run.
  await page.goto(`/${ORG}/projects/${PROJECT}/issues/${number}`)
  await page.getByRole('tab', { name: /^Comments/ }).click()
  await expect(page.getByText(ISSUE_CAUSE)).toBeVisible({ timeout: 60_000 })
  const factory = page.getByRole('link', { name: 'Factory', exact: true })
  await expect(factory).toHaveAttribute('href', new RegExp(`/${ORG}/runs/${runId}$`))

  expect(errors, flow.log()).toEqual([])
})

test('the starter Factory Dashboard shows its four Charts with the Project filter', async ({ page }) => {
  test.setTimeout(180_000)
  await useTheme(page, test.info())
  // The dev Console has few threads (it runs one chart at a time), so some of
  // the four charts can answer "busy" (503) on a run. The flow presses
  // Refresh until each chart has run once, and a chart that did not run says
  // only "busy": no other error.
  const titles = ['Tokens for each merged PR', 'Cycle time of merged PRs', 'Acceptance rate', 'Interventions for each Run']
  const errors = watchErrors(page, [failedLoad(503)])
  const chartsRan = async () => {
    const ran = new Set<string>()
    await expect(async () => {
      await expect(page.getByRole('status', { name: "Running the chart's Query" })).toHaveCount(0, { timeout: 5_000 })
      for (const title of titles) {
        const card = page.locator(`[data-chart-card="${title}"]`)
        if (await card.getByText(/^[\d,]+ rows?$|^No rows/).count() > 0) ran.add(title)
        else await expect(card).toContainText('Console is busy running other charts')
      }
      if (ran.size < titles.length) {
        await page.getByRole('button', { name: 'Refresh' }).click()
        throw new Error(`these charts have not run yet: ${titles.filter(t => !ran.has(t)).join(', ')}`)
      }
    }).toPass({ timeout: 90_000, intervals: [500, 1_000] })
  }

  // Console made the Dashboard when it first saw the Factory settings.
  await page.goto(`/${ORG}/dashboards`)
  await page.getByRole('link', { name: 'Factory', exact: true }).first().click()
  await expect(page.getByRole('heading', { name: 'Factory', exact: true })).toBeVisible()

  for (const title of titles) await expect(page.locator(`[data-chart-card="${title}"]`)).toBeVisible()
  await expect(page.locator('[data-chart-card]')).toHaveCount(4)
  await chartsRan()

  // The Project filter lists the Projects that the run events name.
  const filter = page.getByRole('combobox', { name: 'Project', exact: true })
  await expect(filter).toBeVisible()
  await filter.click()
  await expect(page.getByRole('option', { name: PROJECT })).toBeVisible({ timeout: 30_000 })
  await page.getByRole('option', { name: PROJECT }).click()
  await page.keyboard.press('Escape')
  await expect(page).toHaveURL(/[?&]v_project=/)
  await chartsRan()
  await expect(page.locator('[data-chart-card]')).toHaveCount(4)
  await shoot(page, 'factory-dashboard', { fullPage: true })

  expect(errors, flow.log()).toEqual([])
})
