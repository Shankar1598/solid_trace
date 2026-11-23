// Signs a seed user in on the stack's Console, opens one page, and saves its
// screenshot and ARIA snapshot. `verify snap` runs it from console/, with
// Playwright from console/node_modules.
//
//   verify snap --path /solid-trace-org/runs --name runs-page [--as member] [--wait-text Runs]
import { createRequire } from 'node:module'
import { existsSync, writeFileSync } from 'node:fs'
import { homedir } from 'node:os'
import { join } from 'node:path'
import { parseArgs } from 'node:util'

const require = createRequire(join(process.cwd(), 'package.json'))
const { chromium } = require('@playwright/test')

const { values: a } = parseArgs({
  options: {
    base: { type: 'string' }, out: { type: 'string' }, path: { type: 'string' }, name: { type: 'string' },
    as: { type: 'string', default: 'admin' }, 'wait-text': { type: 'string' },
  },
})
if (!a.base || !a.out || !a.path || !a.name) {
  console.error('usage: verify snap --path /<org>/... --name <file name> [--as admin|member] [--wait-text <text>]')
  process.exit(2)
}
const users = {
  admin: { email: 'admin@solidtrace.local', password: 'password123' },
  member: { email: 'member@solidtrace.local', password: 'password123' },
}
const user = users[a.as] ?? (console.error(`--as is admin or member, not ${a.as}`), process.exit(2))

// Chromium's system libraries, unpacked without root on the dev box (as playwright.config.ts does).
const libs = process.env.PW_LIBS ?? join(homedir(), '.cache/solid-trace/pw/libs/usr/lib/x86_64-linux-gnu')
const env = existsSync(libs) ? { ...process.env, LD_LIBRARY_PATH: [libs, process.env.LD_LIBRARY_PATH].filter(Boolean).join(':') } : process.env

const browser = await chromium.launch({ env })
const errors = []
try {
  const page = await browser.newPage({ baseURL: a.base, viewport: { width: 1440, height: 900 }, colorScheme: 'dark' })
  page.on('pageerror', e => errors.push(String(e)))
  await page.goto('/login')
  await page.fill('input[type=email]', user.email)
  await page.fill('input[type=password]', user.password)
  await page.click('button[type=submit]')
  await page.waitForURL(url => !url.pathname.startsWith('/login'), { timeout: 30_000 })
  const resp = await page.goto(a.path)
  // A missing text is a failed proof, but the page as it is still gets saved.
  let missing = null
  if (a['wait-text']) {
    await page.getByText(a['wait-text']).first().waitFor({ timeout: 30_000 })
      .catch(() => { missing = a['wait-text'] })
  } else await page.waitForLoadState('networkidle', { timeout: 15_000 }).catch(() => {})
  const png = join(a.out, `${a.name}.png`)
  const aria = join(a.out, `${a.name}.aria.txt`)
  await page.screenshot({ path: png, fullPage: true })
  writeFileSync(aria, await page.locator('body').ariaSnapshot())
  const status = resp?.status()
  console.log(JSON.stringify({ url: page.url(), status, as: a.as, png, aria, page_errors: errors, missing_text: missing }))
  process.exitCode = status && status < 400 && errors.length === 0 && !missing ? 0 : 1
} finally {
  await browser.close()
}
