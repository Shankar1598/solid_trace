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

export async function shoot(page: Page, name: string, opts: { fullPage?: boolean } = {}) {
  const info = test.info()
  const dir = join(root, set)
  mkdirSync(dir, { recursive: true })
  await page.waitForLoadState('networkidle').catch(() => {})
  await page.waitForTimeout(250)
  const path = join(dir, `${name}--${info.project.name.replace(/^screens-/, '')}.png`)
  await page.screenshot({ path, fullPage: opts.fullPage ?? false, animations: 'disabled' })
  return path
}

// Fails the test on uncaught page errors and console errors, so a screen that
// renders but throws is not mistaken for a good one.
export function watchErrors(page: Page) {
  const errors: string[] = []
  page.on('pageerror', e => errors.push(`pageerror: ${e.message}`))
  page.on('console', m => { if (m.type() === 'error') errors.push(`console: ${m.text()}`) })
  return errors
}
