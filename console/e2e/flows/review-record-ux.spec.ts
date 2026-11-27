import { test, expect, type Browser, type Page } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import { createHash } from 'node:crypto'
import { readFileSync, writeFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { shortSession, type FixtureSession } from '../support/agentTranscripts'
import { ADMIN, ORG } from '../support/data'
import { watchErrors } from '../support/screens'

// The Evidence file popup on the Agent session page and the Review record
// page, the PR body on the Review record page, the row of a run with no
// data, and the Details of a finding with long paths. The flow uploads, as
// the capture and `solidtrace evidence add` do, an Agent session with a
// screenshot and a video and a Review record file whose pr_body links the
// screenshot, and a second Agent session with a record of plugin 0.24: no
// pr_body, a run that a findings block that could not be read made, and a
// finding whose events name long paths.

const RUN = Date.now().toString(36)
const ENTRY = 'flows.example/flowteam/*'
const REPOSITORY = `flows.example/flowteam/record-ux-${RUN}`
const TOKEN_NAME = `e2e review record ux ${RUN}`
const INGEST_URL = process.env.E2E_INGEST_URL ?? 'http://localhost:4000'
const NEW_PR = `https://github.example/flowteam/record-ux-${RUN}/pull/1`
const OLD_PR = `https://github.example/flowteam/record-ux-${RUN}/pull/2`
const consoleDir = resolve(__dirname, '../..')

const rails = (code: string) => execFileSync('mise', ['exec', '--', 'bin/rails', 'runner', code], {
  cwd: consoleDir, stdio: 'pipe', timeout: 120_000,
}).toString().trim()

const usage = { input: 10, output: 20, cacheRead: 0, cacheWrite: 0 }
const withBody = shortSession('/work/shop', 'Open the PR of the cart page', usage)
const oldStyle = shortSession('/work/shop', 'Open the PR of the cart total', usage)
let token = ''

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
  rails(`ApiToken.where(name: ${JSON.stringify(TOKEN_NAME)}).delete_all`)
})

async function makeFiles(browser: Browser): Promise<{ png: Buffer; webm: Buffer }> {
  const size = { width: 640, height: 360 }
  const context = await browser.newContext({ viewport: size, recordVideo: { dir: test.info().outputPath('seed'), size }, storageState: { cookies: [], origins: [] } })
  const page = await context.newPage()
  await page.setContent(`<body style="margin:0;height:100vh;display:grid;place-items:center;background:#123;color:#fff;font:64px monospace">
    <div id="clock"></div><script>const start = performance.now(); setInterval(() => { clock.textContent = ((performance.now() - start) / 1000).toFixed(1) + ' s' }, 100)</script></body>`)
  await page.waitForTimeout(3_000)
  const png = await page.screenshot()
  const path = await page.video()!.path()
  await context.close()
  return { png, webm: readFileSync(path) }
}

async function ingest(session: FixtureSession, path: string, query: Record<string, string>, body: Buffer) {
  const answer = await fetch(`${INGEST_URL}/api/agent/v1/agent-sessions/${session.id}/${path}?${new URLSearchParams(query)}`, {
    method: 'POST', headers: { Authorization: `Bearer ${token}` }, body: new Uint8Array(body),
  })
  const text = await answer.text()
  expect(answer.status, text).toBeLessThan(300)
  return JSON.parse(text)
}

const fileQuery = (path: string, offset = '0') => ({ path, version: '1', offset, harness: 'claude-code', repository: REPOSITORY })

/** Uploads the session's transcript, as the capture does. */
async function uploadTranscript(session: FixtureSession) {
  const [transcript, lines] = Object.entries(session.files)[0]
  await ingest(session, 'files', fileQuery(transcript), Buffer.from(lines))
}

/** Uploads the record as the file that solidtrace review final writes in the session's folder. */
async function uploadRecord(session: FixtureSession, record: string) {
  const name = `round-1-${createHash('sha256').update(JSON.parse(record).pr).digest('hex').slice(0, 12)}.json`
  const path = `${session.id}/solidtrace/review-records/${name}`
  await ingest(session, 'files', { ...fileQuery(path), content_hash: createHash('sha256').update(record).digest('hex') }, Buffer.from(record))
}

const LONG_PATH = `console/app/frontend/pages/ReviewRecords/${'AVeryLongFolderNameWithNoPlaceToBreak'.repeat(4)}/FindingDetails.tsx`

function record(session: FixtureSession, pr: string, extra: Record<string, unknown>) {
  return JSON.stringify({
    version: 2,
    sessions: [{ id: session.id, title: `Cart page ${RUN}` }],
    pr,
    round: 1,
    story: { started_from: 'The person asked for a cart page.', changes: ['Add the cart page.'], reviews: ['Run 1 reviewed the cart page.'], after_review: [], sessions: [] },
    reviews: [{ run: 1, block_run: 1, session: session.id, writer: '', skill: 'code-review', trigger: 'agent', stage: 'step', step: 'cart page', skipped: null,
      start: 'u-1', reviewers: [{ name: 'bugs-and-spec', model: 'opus', effort: 'high' }], verifier: { model: 'opus', effort: 'high' } }],
    findings: [],
    confirmations: [],
    ...extra,
  })
}

const theme = (page: Page, scheme: 'light' | 'dark') => page.evaluate(s => localStorage.setItem('theme', s), scheme)

const shot = (page: Page, name: string) => page.screenshot({ path: test.info().outputPath(`${name}.png`), animations: 'disabled' })

test('an Evidence file opens in a popup, and the Review record page shows the PR body, a run with no data and Details that wrap', async ({ page, browser, baseURL }) => {
  test.setTimeout(180_000)
  const origin = new URL(baseURL!).origin
  const errors = watchErrors(page)
  const { png, webm } = await makeFiles(browser)

  await uploadTranscript(withBody)
  await uploadTranscript(oldStyle)
  const image = await ingest(withBody, 'evidence', { caption: 'Cart page' }, png)
  const video = await ingest(withBody, 'evidence', { caption: 'Checkout, with the coupon field and the total that it changes' }, webm)
  const body = [
    '## What changes', '', 'The cart page shows the **total** with the discount.', '',
    '## Evidence', '', `- [Screenshot: Cart page](${image.url})`, `- [Video: Checkout](${video.url})`, '- [The spec](https://example.com/spec)', '',
    '<b data-raw-html>raw HTML</b>', '', '<!-- solidtrace-record round 1 -->',
  ].join('\n')
  await uploadRecord(withBody, record(withBody, NEW_PR, { pr_body: body }))
  await uploadRecord(oldStyle, record(oldStyle, OLD_PR, {
    reviews: [
      { run: 1, block_run: 1, session: oldStyle.id, writer: '', skill: 'code-review', trigger: 'agent', stage: 'step', step: 'cart total', skipped: null,
        start: 'u-1', reviewers: [{ name: 'tests', model: 'opus', effort: 'high' }], verifier: null },
      { run: 2, block_run: null, session: oldStyle.id, writer: '', skill: null, trigger: null, stage: null, step: null, skipped: null,
        start: 'u-bad', reviewers: null, verifier: null },
    ],
    findings: [{
      id: '1.F1', block_id: 'F1', run: 1, found_by: { kind: 'reviewer', name: 'tests' }, severity: 'medium', file: 'cart.go', line: 3,
      title: 'No test for the total', scenario: 'A cart with a coupon.',
      events: [{ type: 'found', source: 'block', evidence: ['u-1'] },
        { type: 'agent', found: `The test belongs in ${LONG_PATH}`, changed: [LONG_PATH], source: 'record', evidence: ['u-1'] },
        { type: 'fix', files: [LONG_PATH, `${LONG_PATH}.snap`], commit: null, source: 'record', evidence: ['u-1'] }],
      outcome: 'fixed', confidence: 'high',
    }],
  }))
  writeFileSync(test.info().outputPath('uploads.json'), JSON.stringify({ withBody: withBody.id, oldStyle: oldStyle.id, image, video }, null, 2))

  // The Agent session page: a plain click opens the popup, and the URL stays.
  const sessionPath = `/${ORG}/agent_sessions/${withBody.id}`
  const files = page.getByRole('region', { name: 'Evidence files' })
  await expect(async () => {
    await page.goto(sessionPath)
    await expect(files.getByRole('link')).toHaveCount(2, { timeout: 1_000 })
  }).toPass({ timeout: 30_000 })
  await expect(files.getByRole('link')).toHaveText(['Video: Checkout, with the coupon field and the total that it changes', 'Screenshot: Cart page'])
  await files.getByRole('link', { name: 'Screenshot: Cart page' }).click()
  const dialog = page.getByRole('dialog')
  await expect(dialog).toHaveAccessibleName('Screenshot: Cart page')
  await expect(page).toHaveURL(`${origin}${sessionPath}`)
  await expect(dialog.locator('[data-position]')).toHaveText('2 of 2')
  const popupImage = dialog.locator('img[data-evidence="image"]')
  await expect.poll(() => popupImage.evaluate((el: HTMLImageElement) => el.complete && el.naturalWidth)).toBeGreaterThan(0)
  const fit = await popupImage.evaluate(el => {
    const box = el.getBoundingClientRect()
    return { right: box.right <= innerWidth, bottom: box.bottom <= innerHeight }
  })
  expect(fit, 'the image fits the window').toEqual({ right: true, bottom: true })

  await page.keyboard.press('ArrowLeft')
  await expect(dialog).toHaveAccessibleName('Video: Checkout, with the coupon field and the total that it changes')
  await expect(dialog.locator('[data-position]')).toHaveText('1 of 2')
  await expect(dialog.locator('video[data-evidence="video"]')).toBeVisible()
  await expect(dialog.getByRole('group', { name: 'Speed' }).getByRole('button')).toHaveText(['0.25x', '0.5x', '1x', '2x'])
  await expect(dialog.getByRole('button', { name: 'Previous Evidence file' })).toHaveAttribute('aria-disabled', 'true')
  await page.keyboard.press('ArrowLeft')
  await expect(dialog.locator('[data-position]')).toHaveText('1 of 2')
  await page.keyboard.press('ArrowRight')
  await expect(dialog).toHaveAccessibleName('Screenshot: Cart page')
  await dialog.getByRole('button', { name: 'Previous Evidence file' }).click()
  await expect(dialog.locator('[data-position]')).toHaveText('1 of 2')
  await page.keyboard.press('Escape')
  await expect(dialog).toHaveCount(0)
  await expect(page).toHaveURL(`${origin}${sessionPath}`)

  // A click with Ctrl or Cmd opens the file's page in a new tab, as a link does.
  const [tab] = await Promise.all([
    page.context().waitForEvent('page'),
    files.getByRole('link', { name: 'Screenshot: Cart page' }).click({ modifiers: ['ControlOrMeta'] }),
  ])
  await expect(tab).toHaveURL(`${origin}/${ORG}/evidence/${image.id}`)
  await tab.close()
  await expect(dialog).toHaveCount(0)

  // The Review record page with a PR body: before the Story, as Markdown.
  const recordPage = `/${ORG}/review_records/record?key=${encodeURIComponent(NEW_PR)}`
  const prBody = page.locator('[data-section="pr-body"]')
  await expect(async () => {
    await page.goto(recordPage)
    await expect(prBody.getByRole('heading', { name: 'What changes' })).toBeVisible({ timeout: 1_000 })
  }).toPass({ timeout: 30_000 })
  const sections = await page.locator('[data-section]').evaluateAll(els => els.map(el => el.getAttribute('data-section')))
  expect(sections.indexOf('pr-body')).toBeGreaterThan(sections.indexOf('evidence-files'))
  expect(sections.indexOf('pr-body')).toBeLessThan(sections.indexOf('story'))
  await expect(prBody.locator('strong')).toHaveText('total')
  // Raw HTML tags are skipped, as in a Text block; the text between them stays text.
  await expect(prBody.locator('[data-raw-html], b')).toHaveCount(0)
  await expect(prBody.getByRole('link', { name: 'The spec' })).toHaveAttribute('target', '_blank')
  await prBody.getByRole('link', { name: 'Screenshot: Cart page' }).click()
  await expect(dialog).toHaveAccessibleName('Screenshot: Cart page')
  await expect(page).toHaveURL(`${origin}${recordPage}`)
  await expect(dialog.locator('[data-position]')).toHaveText('2 of 2')
  await expect.poll(() => popupImage.evaluate((el: HTMLImageElement) => el.complete && el.naturalWidth)).toBeGreaterThan(0)
  await theme(page, 'light')
  await page.reload()
  await prBody.getByRole('link', { name: 'Screenshot: Cart page' }).click()
  await expect.poll(() => popupImage.evaluate((el: HTMLImageElement) => el.complete && el.naturalWidth)).toBeGreaterThan(0)
  await shot(page, 'desktop-light-popup')
  await page.keyboard.press('Escape')
  await expect(dialog).toHaveCount(0)
  await prBody.screenshot({ path: test.info().outputPath('desktop-light-pr-body.png'), animations: 'disabled' })

  // The Review record of plugin 0.24: no PR body, a run with no data, Details that wrap.
  await expect(async () => {
    await page.goto(`/${ORG}/review_records/record?key=${encodeURIComponent(OLD_PR)}`)
    await expect(page.getByRole('table', { name: 'Reviews' })).toBeVisible({ timeout: 1_000 })
  }).toPass({ timeout: 30_000 })
  await expect(prBody).toContainText('This record has no PR body. Records from plugin 0.24 and earlier do not keep it.')
  const reviews = page.getByRole('table', { name: 'Reviews' })
  await expect(reviews.locator('tr[data-run="2"] td')).toHaveCount(2)
  await expect(reviews.locator('tr[data-run="2"] td').first()).toHaveText('2')
  await expect(reviews.locator('tr[data-run="2"] [data-cell="no-data"]')).toHaveText('No data: the findings block of this run could not be read.')
  await expect(reviews.locator('tr[data-run="1"] [data-cell="no-data"]')).toHaveCount(0)
  const finding = page.locator('tr[data-finding="1.F1"] [data-cell="finding"]')
  await finding.locator('summary', { hasText: 'Details' }).click()
  await expect(finding).toContainText('FindingDetails.tsx')
  const overflow = await finding.evaluate(el => ({
    cell: el.scrollWidth - el.clientWidth,
    lines: Array.from(el.querySelectorAll('li')).map(li => li.scrollWidth - li.clientWidth),
    table: (() => { const box = el.closest('[data-slot="table-container"]')!; return box.scrollWidth - box.clientWidth })(),
  }))
  writeFileSync(test.info().outputPath('details-overflow.json'), JSON.stringify(overflow))
  expect(overflow, 'the Details text stays in its cell, and the table does not scroll sideways').toEqual({ cell: 0, lines: [0, 0], table: 0 })
  await page.screenshot({ path: test.info().outputPath('desktop-light-old-record.png'), animations: 'disabled', fullPage: true })

  // A phone, dark: the popup and the PR body fit 390 px with no sideways scroll.
  await page.setViewportSize({ width: 390, height: 844 })
  await theme(page, 'dark')
  await page.goto(recordPage)
  await expect(prBody.getByRole('heading', { name: 'What changes' })).toBeVisible()
  const across = () => page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)
  expect(await across()).toBe(0)
  await prBody.screenshot({ path: test.info().outputPath('phone-dark-pr-body.png'), animations: 'disabled' })
  await prBody.getByRole('link', { name: 'Video: Checkout' }).click()
  await expect(dialog.locator('[data-position]')).toHaveText('1 of 2')
  const player = dialog.locator('video[data-evidence="video"]')
  await expect.poll(() => player.evaluate((v: HTMLVideoElement) => v.readyState)).toBeGreaterThanOrEqual(1)
  const phone = await dialog.evaluate(el => {
    const box = el.getBoundingClientRect()
    return { left: box.left >= 0, right: box.right <= innerWidth, across: el.scrollWidth - el.clientWidth }
  })
  expect(phone).toEqual({ left: true, right: true, across: 0 })
  expect(await across()).toBe(0)
  // Tab goes round the popup: the video's own controls (Chrome draws their
  // ring), the speeds, the buttons and Open page. The focus never leaves it,
  // and a button shows its ring.
  const focusOf = () => page.evaluate(() => {
    const el = document.activeElement as HTMLElement
    return { inDialog: !!el.closest('[role="dialog"]'), button: el.getAttribute('aria-label') ?? el.textContent ?? '', ring: getComputedStyle(el).boxShadow }
  })
  const seen: { inDialog: boolean; button: string; ring: string }[] = []
  for (let i = 0; i < 12; i++) {
    await page.keyboard.press('Tab')
    seen.push(await focusOf())
  }
  writeFileSync(test.info().outputPath('phone-focus.json'), JSON.stringify(seen, null, 2))
  expect(seen.every(f => f.inDialog), 'the focus stays in the popup').toBe(true)
  const next = seen.find(f => f.button === 'Next Evidence file')
  expect(next?.ring, 'the focused button shows its ring').not.toBe('none')
  while ((await focusOf()).button !== 'Next Evidence file') await page.keyboard.press('Tab')
  await shot(page, 'phone-dark-popup')
  await page.keyboard.press('Escape')
  await expect(dialog).toHaveCount(0)

  expect(errors).toEqual([])
})
