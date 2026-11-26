import { test, expect, type Browser, type BrowserContext, type Locator, type Page } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import { createHash, randomUUID } from 'node:crypto'
import { readFileSync, writeFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { shortSession } from '../support/agentTranscripts'
import { ADMIN, MEMBER, ORG } from '../support/data'
import { expectFits, watchErrors } from '../support/screens'
import { newContext } from '../support/video'

const RUN = Date.now().toString(36)
const ENTRY = 'flows.example/flowteam/*'
const REPOSITORY = `flows.example/flowteam/evidence-${RUN}`
const TOKEN_NAME = `e2e evidence files ${RUN}`
const OTHER_ORG = `flow-evidence-${RUN}`
const DELETED = randomUUID()
const OUTSIDER = { email: `flow-evidence-${RUN}@solidtrace.local`, password: 'password-1' }
const INGEST_URL = process.env.E2E_INGEST_URL ?? 'http://localhost:4000'
const consoleDir = resolve(__dirname, '../..')

const rails = (code: string) => execFileSync('mise', ['exec', '--', 'bin/rails', 'runner', code], {
  cwd: consoleDir, stdio: 'pipe', timeout: 120_000,
}).toString().trim()

const session = shortSession('/work/shop', 'Prove the cart page with a screenshot and a video', { input: 10, output: 20, cacheRead: 0, cacheWrite: 0 })
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
    other = Organization.create!(name: "Flow Evidence ${RUN}", slug: ${JSON.stringify(OTHER_ORG)})
    outsider = User.create!(name: "Flow Outsider", email: ${JSON.stringify(OUTSIDER.email)}, password: ${JSON.stringify(OUTSIDER.password)})
    OrganizationUser.create!(organization: other, user: outsider, role: :admin)
    print ApiToken.issue!(user: admin, name: ${JSON.stringify(TOKEN_NAME)}, scopes: ["sessions:write"], organization_id: org.id).last
  `)
})

test.afterAll(() => {
  rails(`ApiToken.where(name: ${JSON.stringify(TOKEN_NAME)}).delete_all
    EvidenceFile.where(id: ${JSON.stringify(DELETED)}).delete_all
    Organization.where(slug: ${JSON.stringify(OTHER_ORG)}).each { |org| OrganizationUser.where(organization: org).delete_all; org.destroy! }
    User.where(email: ${JSON.stringify(OUTSIDER.email)}).each { |user| OrganizationUser.where(user: user).delete_all; user.destroy! }`)
})

async function makeFiles(browser: Browser): Promise<{ png: Buffer; webm: Buffer }> {
  const size = { width: 640, height: 360 }
  const context = await browser.newContext({ viewport: size, recordVideo: { dir: test.info().outputPath('seed'), size }, storageState: { cookies: [], origins: [] } })
  const page = await context.newPage()
  await page.setContent(`<body style="margin:0;height:100vh;display:grid;place-items:center;background:#123;color:#fff;font:64px monospace">
    <div id="clock"></div><script>const start = performance.now(); setInterval(() => { clock.textContent = ((performance.now() - start) / 1000).toFixed(1) + ' s' }, 100)</script></body>`)
  await page.waitForTimeout(12_000)
  const png = await page.screenshot()
  const path = await page.video()!.path()
  await context.close()
  return { png, webm: readFileSync(path) }
}

async function ingest(path: string, query: Record<string, string>, body: Buffer) {
  const answer = await fetch(`${INGEST_URL}/api/agent/v1/agent-sessions/${session.id}/${path}?${new URLSearchParams(query)}`, {
    method: 'POST', headers: { Authorization: `Bearer ${token}` }, body: new Uint8Array(body),
  })
  const text = await answer.text()
  expect(answer.status, text).toBeLessThan(300)
  return { status: answer.status, json: JSON.parse(text) }
}

async function signIn(page: Page, as: { email: string; password: string }) {
  await page.fill('input[type=email]', as.email)
  await page.fill('input[type=password]', as.password)
  await page.click('button[type=submit]')
}

const shot = (page: Page, name: string) => page.screenshot({ path: test.info().outputPath(`${name}.png`), animations: 'disabled' })

const gateShot = (page: Page, theme: string, name: string) =>
  page.screenshot({ path: test.info().outputPath(`review-gate/${theme}-${name}.png`), animations: 'disabled' })

const cacheControl = (headers: string) => (headers.match(/^cache-control: (.*)$/mi)?.[1] ?? '').split(/,\s*/).sort()

async function expectMarks(entry: Locator, mark: 'video' | 'deleted') {
  await expect(entry.locator(`[data-mark="${mark}"]`)).toHaveCount(1)
  await expect(entry.locator('img')).toHaveCount(0)
}

function insertDeletedRow() {
  rails(`EvidenceFile.insert!({ id: ${JSON.stringify(DELETED)}, organization_id: Organization.find_by!(slug: ${JSON.stringify(ORG)}).id,
    agent_session_id: ${JSON.stringify(session.id)}, sha256: SecureRandom.hex(32), caption: "Old screen", content_type: "image/png",
    byte_size: 84, storage_key: "evidence/flow-deleted-${RUN}.png", created_at: 91.days.ago, deleted_at: 1.hour.ago })`)
}

async function curlHeaders(context: BrowserContext, url: string, headers: string[] = []): Promise<string> {
  const cookie = (await context.cookies()).map(c => `${c.name}=${c.value}`).join('; ')
  const args = ['-sS', '-D', '-', '-o', '/dev/null', '-H', `Cookie: ${cookie}`, ...headers.flatMap(header => ['-H', header]), url]
  return execFileSync('curl', args).toString().split('\r\n').filter(line => !/^set-cookie:/i.test(line)).join('\n')
}

test('a member opens Evidence files after sign-in, plays the video at 0.5x, and finds them on the lists', async ({ browser, baseURL }) => {
  test.setTimeout(180_000)
  const origin = new URL(baseURL!).origin
  const { png, webm } = await makeFiles(browser)

  const [transcript, lines] = Object.entries(session.files)[0]
  await ingest('files', { path: transcript, version: '1', offset: '0', harness: 'claude-code', repository: REPOSITORY }, Buffer.from(lines))
  const image = await ingest('evidence', { caption: 'Cart page' }, png)
  const video = await ingest('evidence', { caption: 'Checkout' }, webm)
  expect([image.status, image.json.content_type, video.status, video.json.content_type]).toEqual([201, 'image/png', 201, 'video/webm'])
  expect(image.json.url).toBe(`${origin}/${ORG}/evidence/${image.json.id}`)
  const imagePath = `/${ORG}/evidence/${image.json.id}`
  const videoPath = `/${ORG}/evidence/${video.json.id}`
  const deletedPath = `/${ORG}/evidence/${DELETED}`
  insertDeletedRow()
  writeFileSync(test.info().outputPath('uploads.json'), JSON.stringify({ session: session.id, image: image.json, video: video.json, webm_bytes: webm.length }, null, 2))

  const member = await newContext(browser, { storageState: { cookies: [], origins: [] } })
  const page = await member.newPage()
  const errors = watchErrors(page)
  await page.goto(imagePath)
  await expect(page).toHaveURL(/\/login$/)
  await signIn(page, MEMBER)
  await expect(page).toHaveURL(`${origin}${imagePath}`)
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Screenshot: Cart page')
  const img = page.locator('img[data-evidence="image"]')
  await expect.poll(() => img.evaluate((el: HTMLImageElement) => el.complete && el.naturalWidth)).toBeGreaterThan(0)
  await expect(page.locator('[data-section="evidence-file-facts"]')).toContainText('Admin User')
  await shot(page, 'evidence-image-page')

  const statuses: number[] = []
  page.on('response', response => { if (response.url().endsWith(`${videoPath}/file`)) statuses.push(response.status()) })
  await page.goto(videoPath)
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Video: Checkout')
  const player = page.locator('video[data-evidence="video"]')
  const speeds = page.getByRole('group', { name: 'Speed' }).getByRole('button')
  await expect(speeds).toHaveText(['0.25x', '0.5x', '1x', '2x'])
  await expect.poll(() => player.evaluate((v: HTMLVideoElement) => v.readyState)).toBeGreaterThanOrEqual(1)
  await player.evaluate(async (v: HTMLVideoElement) => {
    v.muted = true
    await v.play()
    v.currentTime = 5
  })
  await expect.poll(() => player.evaluate((v: HTMLVideoElement) => v.currentTime)).toBeGreaterThanOrEqual(5)
  expect(statuses).toContain(206)
  await page.getByRole('button', { name: '0.5x' }).click()
  await expect(page.getByRole('button', { name: '0.5x' })).toHaveAttribute('aria-pressed', 'true')
  const played = await player.evaluate(async (v: HTMLVideoElement) => {
    if (v.paused) await v.play()
    const [media, wall] = [v.currentTime, performance.now()]
    await new Promise(done => setTimeout(done, 1000))
    return { rate: v.playbackRate, moved: v.currentTime - media, wall: (performance.now() - wall) / 1000 }
  })
  writeFileSync(test.info().outputPath('video-play.json'), JSON.stringify({ statuses, played }, null, 2))
  expect(played.rate).toBe(0.5)
  expect(played.moved / played.wall).toBeGreaterThanOrEqual(0.4)
  expect(played.moved / played.wall).toBeLessThanOrEqual(0.6)
  await shot(page, 'evidence-video-page')
  await page.reload()
  await expect(page.getByRole('button', { name: '0.5x' })).toHaveAttribute('aria-pressed', 'true')
  await expect.poll(() => player.evaluate((v: HTMLVideoElement) => v.playbackRate)).toBe(0.5)

  const imageHeaders = await curlHeaders(member, `${origin}${imagePath}/file`)
  const videoHeaders = await curlHeaders(member, `${origin}${videoPath}/file`, ['Range: bytes=100-199'])
  const etag = imageHeaders.match(/^etag: (.*)$/mi)?.[1] ?? ''
  const notModified = await curlHeaders(member, `${origin}${imagePath}/file`, [`If-None-Match: ${etag}`])
  const goneHeaders = await curlHeaders(member, `${origin}${deletedPath}/file`)
  writeFileSync(test.info().outputPath('headers.txt'), [
    `${imagePath}/file`, imageHeaders, `${videoPath}/file (Range: bytes=100-199)`, videoHeaders,
    `${imagePath}/file (If-None-Match: ${etag})`, notModified, `${deletedPath}/file`, goneHeaders,
  ].join('\n'))
  for (const [headers, status, type] of [[imageHeaders, '200', 'image/png'], [videoHeaders, '206', 'video/webm']]) {
    expect(headers).toMatch(new RegExp(`^HTTP/1.1 ${status}`, 'm'))
    expect(headers).toMatch(new RegExp(`^content-type: ${type}$`, 'mi'))
    expect(headers).toMatch(/^x-content-type-options: nosniff$/mi)
    expect(headers).toMatch(/^content-security-policy: sandbox$/mi)
    expect(headers).toMatch(/^content-disposition: inline$/mi)
    expect(cacheControl(headers)).toEqual(['immutable', 'max-age=31536000', 'private'])
  }
  expect(videoHeaders).toMatch(new RegExp(`^content-range: bytes 100-199/${webm.length}$`, 'mi'))
  expect(etag).toBe(`"${createHash('sha256').update(png).digest('hex')}"`)
  expect(notModified).toMatch(/^HTTP\/1.1 304/m)
  expect(goneHeaders).toMatch(/^HTTP\/1.1 410/m)
  expect(goneHeaders).toMatch(/^x-content-type-options: nosniff$/mi)
  expect(cacheControl(goneHeaders)).toEqual(['private'])

  const files = page.getByRole('region', { name: 'Evidence files' })
  await expect(async () => {
    await page.goto(`/${ORG}/agent_sessions/${session.id}`)
    await expect(files.getByRole('link')).toHaveText(['Video: Checkout', 'Screenshot: Cart page', 'Screenshot: Old screen'], { timeout: 1_000 })
  }).toPass({ timeout: 30_000 })
  await expectMarks(files.locator(`[data-evidence-file="${video.json.id}"]`), 'video')
  await expectMarks(files.locator(`[data-evidence-file="${DELETED}"]`), 'deleted')
  await expect(files.locator(`[data-evidence-file="${image.json.id}"] img`)).toHaveCount(1)
  await shot(page, 'agent-session-evidence-files')
  await files.getByRole('link', { name: 'Screenshot: Cart page' }).click()
  await expect(page).toHaveURL(`${origin}${imagePath}`)

  await page.goto(`/${ORG}/evidence`)
  const rows = page.getByRole('table', { name: 'Evidence files' }).locator('tbody tr')
  const ids = await rows.evaluateAll(trs => trs.map(tr => tr.getAttribute('data-evidence-file')))
  expect(ids.indexOf(video.json.id)).toBeGreaterThanOrEqual(0)
  expect(ids.indexOf(video.json.id)).toBeLessThan(ids.indexOf(image.json.id))
  expect(ids.indexOf(image.json.id)).toBeLessThan(ids.indexOf(DELETED))
  const row = (id: string) => page.locator(`tbody tr[data-evidence-file="${id}"]`)
  await expectMarks(row(video.json.id), 'video')
  await expectMarks(row(DELETED), 'deleted')
  await expect(row(DELETED).locator('[data-badge="deleted"]')).toHaveText('deleted')
  await expect(row(image.json.id).locator('[data-badge="deleted"]')).toHaveCount(0)
  await expect(row(image.json.id).locator('img')).toHaveCount(1)
  const nav = page.getByRole('navigation', { name: 'Main' })
  await expect(nav.getByRole('button', { name: 'Coding agents' })).toHaveAttribute('aria-expanded', 'true')
  await expect(nav.locator('[aria-current="page"]')).toHaveText('Evidence files')
  await shot(page, 'evidence-files-list')

  await page.goto(deletedPath)
  await expect(page.locator('[data-notice="evidence-deleted"]')).toContainText(/^This evidence was deleted on \w{3} \d{1,2}, \d{4} by Evidence Retention, 90 days after its upload\.$/)
  await expect(page.locator('[data-evidence]')).toHaveCount(0)
  expect(errors).toEqual([])

  for (const theme of ['light', 'dark']) {
    await page.evaluate(scheme => localStorage.setItem('theme', scheme), theme)
    await page.goto(imagePath)
    await expect.poll(() => img.evaluate((el: HTMLImageElement) => el.complete && el.naturalWidth)).toBeGreaterThan(0)
    await gateShot(page, theme, 'image-page')
    await page.goto(videoPath)
    await expect.poll(() => player.evaluate((v: HTMLVideoElement) => v.readyState)).toBeGreaterThanOrEqual(1)
    await player.evaluate((v: HTMLVideoElement) => new Promise(done => {
      v.addEventListener('seeked', done, { once: true })
      v.currentTime = 5
    }))
    await expect.poll(() => player.evaluate((v: HTMLVideoElement) => v.readyState)).toBeGreaterThanOrEqual(2)
    await gateShot(page, theme, 'video-page')
    await page.goto(deletedPath)
    await gateShot(page, theme, 'deleted-page')
    await page.goto(`/${ORG}/evidence`)
    await expect(rows.first()).toBeVisible()
    await gateShot(page, theme, 'list')
    await page.goto(`/${ORG}/agent_sessions/${session.id}`)
    await files.scrollIntoViewIfNeeded()
    await gateShot(page, theme, 'agent-session-evidence-files')
  }

  await page.setViewportSize({ width: 390, height: 844 })
  for (const theme of ['light', 'dark']) {
    await page.evaluate(scheme => localStorage.setItem('theme', scheme), theme)
    await page.goto(`/${ORG}/evidence`)
    const phoneRow = row(image.json.id)
    await expect(phoneRow.getByRole('link', { name: 'Screenshot: Cart page' })).toBeVisible()
    await expect(phoneRow.locator('[data-cell="phone-facts"] time')).toBeVisible()
    for (const line of await phoneRow.locator('[data-cell="phone-facts"] > div').all()) await expectFits(line)
    const fits = await page.evaluate(() => {
      const table = document.querySelector('[data-slot="table-container"]')!
      const time = document.querySelector('[data-cell="phone-facts"] time')!.getBoundingClientRect()
      return { page: document.documentElement.scrollWidth - document.documentElement.clientWidth, table: table.scrollWidth - table.clientWidth, timeRight: time.right }
    })
    writeFileSync(test.info().outputPath(`phone-fit-${theme}.json`), JSON.stringify(fits))
    expect(fits).toEqual({ page: 0, table: 0, timeRight: expect.any(Number) })
    expect(fits.timeRight).toBeLessThanOrEqual(390)
    await gateShot(page, theme, 'list-phone')
  }

  const reviewer = await newContext(browser, { storageState: { cookies: [], origins: [] } })
  const bytesPage = await reviewer.newPage()
  await bytesPage.goto(`${imagePath}/file`)
  await expect(bytesPage).toHaveURL(/\/login$/)
  await signIn(bytesPage, MEMBER)
  await expect(bytesPage).toHaveURL(`${origin}${imagePath}/file`)
  expect(await bytesPage.evaluate(() => document.contentType)).toBe('image/png')
  await expect.poll(() => bytesPage.locator('img').evaluate((el: HTMLImageElement) => el.complete && el.naturalWidth)).toBeGreaterThan(0)
  await shot(bytesPage, 'file-after-sign-in')
  await reviewer.close()

  const outsider = await newContext(browser, { storageState: { cookies: [], origins: [] } })
  const other = await outsider.newPage()
  await other.goto('/login')
  await signIn(other, OUTSIDER)
  await expect(other).not.toHaveURL(/\/login/)
  for (const path of [imagePath, videoPath]) {
    expect((await other.goto(path))!.status()).toBe(404)
    expect((await other.request.get(`${path}/file`)).status()).toBe(404)
  }
  await outsider.close()
  await member.close()
})
