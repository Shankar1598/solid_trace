import { test, expect, type Page } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import { existsSync, rmSync } from 'node:fs'
import { join, resolve } from 'node:path'
import { randomUUID } from 'node:crypto'
import { ADMIN, ORG } from '../support/data'
import { watchErrors } from '../support/screens'

const RUN = Date.now().toString(36)
const ENTRY = 'flows.example/flowteam/*'
const TOKEN_NAME = `e2e evidence retention ${RUN}`
const SESSION = randomUUID()
const INGEST_URL = process.env.E2E_INGEST_URL ?? 'http://localhost:4000'
const QUERY_URL = process.env.EVENT_STORE_URL ?? 'http://127.0.0.1:4100'
const STACK = process.env.SOLIDTRACE_VERIFY_RUN
const consoleDir = resolve(__dirname, '../..')

const rails = (code: string) => execFileSync('mise', ['exec', '--', 'bin/rails', 'runner', code], {
  cwd: consoleDir, stdio: 'pipe', timeout: 120_000,
}).toString().trim()

let token = ''
let uploadedId = ''
let object = ''

test.beforeAll(() => {
  token = rails(`
    org = Organization.find_by!(slug: ${JSON.stringify(ORG)})
    admin = User.find_by!(email: ${JSON.stringify(ADMIN.email)})
    project = org.agent_project
    result = if project
      AgentSessionsSettings.save(project: project, user: admin, entries: project.captured_repositories | [${JSON.stringify(ENTRY)}], confirmed: true)
    else
      AgentSessionsSettings.turn_on(organization: org, user: admin, entries: [${JSON.stringify(ENTRY)}], confirmed: true)
    end
    raise result.errors.inspect unless result.done?
    print ApiToken.issue!(user: admin, name: ${JSON.stringify(TOKEN_NAME)}, scopes: ["sessions:write"], organization_id: org.id).last
  `)
})

test.afterAll(() => {
  rails(`ApiToken.where(name: ${JSON.stringify(TOKEN_NAME)}).delete_all
    EvidenceFile.where(id: ${JSON.stringify(uploadedId)}).delete_all
    Organization.find_by!(slug: ${JSON.stringify(ORG)}).agent_project&.update!(evidence_retention_days: 90)`)
  if (object) rmSync(object, { force: true })
})

const shot = (page: Page, name: string) => page.screenshot({ path: test.info().outputPath(`${name}.png`), animations: 'disabled' })

const png = () => Buffer.concat([Buffer.from('\x89PNG\r\n\x1a\n', 'binary'), Buffer.from(`evidence retention ${RUN}`)])

test('an admin sets Evidence Retention to 1 day, and the daily pass deletes an older file and its bytes', async ({ page, baseURL }) => {
  test.setTimeout(120_000)
  const errors = watchErrors(page)

  const answer = await fetch(`${INGEST_URL}/api/agent/v1/agent-sessions/${SESSION}/evidence?caption=Retention`, {
    method: 'POST', headers: { Authorization: `Bearer ${token}` }, body: new Uint8Array(png()),
  })
  const uploaded = await answer.json()
  expect(answer.status, JSON.stringify(uploaded)).toBe(201)
  uploadedId = uploaded.id
  const filePath = `/${ORG}/evidence/${uploaded.id}`
  const storageKey = rails(`print EvidenceFile.find(${JSON.stringify(uploaded.id)}).storage_key`)
  expect(STACK, 'run the flow with verify flow, which sets SOLIDTRACE_VERIFY_RUN').toBeTruthy()
  object = join(STACK ?? '', 'stack', 'evidence', storageKey)
  expect(existsSync(object)).toBe(true)

  await page.goto(`/${ORG}/settings/agent_sessions`)
  const setting = page.locator('[data-section="evidence-retention"]')
  const days = setting.getByLabel('Evidence Retention (days)')
  await expect(days).toHaveValue(/^\d+$/)
  await days.fill('0')
  await setting.getByRole('button', { name: 'Save' }).click()
  await expect(setting).toContainText('Evidence Retention must be in 1..3650')
  await days.fill('1')
  await setting.getByRole('button', { name: 'Save' }).click()
  await expect(page.getByText('Evidence Retention saved')).toBeVisible()
  await expect(days).toHaveValue('1')
  expect(rails(`print Organization.find_by!(slug: ${JSON.stringify(ORG)}).agent_project.evidence_retention_days`)).toBe('1')

  rails(`EvidenceFile.where(id: ${JSON.stringify(uploaded.id)}).update_all(created_at: 2.days.ago)`)
  const pass = await fetch(`${QUERY_URL}/retention`, { method: 'POST' })
  const passed = await pass.json()
  expect(pass.status, JSON.stringify(passed)).toBe(200)
  expect(passed.evidence_files_deleted).toBeGreaterThanOrEqual(1)

  await page.goto(filePath)
  await expect(page.locator('[data-notice="evidence-deleted"]'))
    .toHaveText(/^This evidence was deleted on \w{3} \d{1,2}, \d{4} by Evidence Retention, 2 days after its upload\.$/)
  await expect(page.locator('[data-evidence]')).toHaveCount(0)
  expect((await page.request.get(`${baseURL}${filePath}/file`)).status()).toBe(410)
  expect(existsSync(object)).toBe(false)
  expect(errors).toEqual([])

  for (const theme of ['light', 'dark']) {
    await page.evaluate(scheme => localStorage.setItem('theme', scheme), theme)
    await page.goto(`/${ORG}/settings/agent_sessions`)
    await expect(days).toHaveValue('1')
    await setting.scrollIntoViewIfNeeded()
    await shot(page, `review-gate/${theme}-evidence-retention-setting`)
    await page.goto(filePath)
    await expect(page.locator('[data-notice="evidence-deleted"]')).toBeVisible()
    await shot(page, `review-gate/${theme}-deleted-page`)
  }
})
