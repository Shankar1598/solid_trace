import { test, type Page, type TestInfo } from '@playwright/test'
import { mkdirSync } from 'node:fs'
import { homedir } from 'node:os'
import { join } from 'node:path'

// Screenshots for reviewing the UI by eye: one file per screen per project
// (size and theme), written outside the repo to
// SCREENS_DIR/SCREENS_SET/<screen>--<project>.png.
const root = process.env.SCREENS_DIR ?? join(homedir(), '.cache/solid-trace/screenshots')
const set = process.env.SCREENS_SET ?? 'current'

// Applies the project's colour scheme to the app's own theme switch, which
// reads localStorage before prefers-color-scheme.
export async function useTheme(page: Page, info: TestInfo) {
  const scheme = info.project.use.colorScheme ?? 'dark'
  await page.addInitScript(theme => {
    try { localStorage.setItem('theme', theme) } catch { /* private mode */ }
  }, scheme)
}

// idle: false shoots without waiting for the network to settle, for a
// loading state whose requests a test holds open.
export async function shoot(page: Page, name: string, opts: { fullPage?: boolean; idle?: boolean } = {}) {
  const info = test.info()
  const dir = join(root, set)
  mkdirSync(dir, { recursive: true })
  if (opts.idle ?? true) await page.waitForLoadState('networkidle').catch(() => {})
  await page.waitForTimeout(250)
  const path = join(dir, `${name}--${info.project.name.replace(/^screens-/, '')}.png`)
  await page.screenshot({ path, fullPage: opts.fullPage ?? false, animations: 'disabled' })
  return path
}

// Fails the test on uncaught page errors and console errors, so a screen that
// renders but throws is not mistaken for a good one. `expected` lists console
// errors a test brings about on purpose, such as the 400 a bad SQL query
// answers, which the browser logs as a failed load.
export function watchErrors(page: Page, expected: RegExp[] = []) {
  const errors: string[] = []
  page.on('pageerror', e => errors.push(`pageerror: ${e.message}`))
  page.on('console', m => {
    if (m.type() === 'error' && !expected.some(e => e.test(m.text()))) errors.push(`console: ${m.text()}`)
  })
  return errors
}

/** The console error a request the page expects to fail logs, by its status. */
export const failedLoad = (status: number) => new RegExp(`^Failed to load resource: the server responded with a status of ${status}\\b`)
