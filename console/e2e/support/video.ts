import { test, type Browser, type BrowserContext, type BrowserContextOptions } from '@playwright/test'

// Videos of the flows, for `verify flow --video` (E2E_VIDEO=on): each browser
// context is recorded, with a pointer that moves to each action and clicks
// there, the action's name, and the test's title. Headless Chromium shows no
// pointer of its own.
const on = process.env.E2E_VIDEO === 'on'
const size = { width: 1440, height: 900 }
const actions = { cursor: 'pointer' as const, duration: 800, position: 'top-right' as const }

/** The config's `use.video`, which records the `page` and `context` fixtures. */
export const video = on
  ? { mode: 'on' as const, size, show: { actions, test: { level: 'title' as const, position: 'bottom-left' as const } } }
  : 'off' as const

/** A new browser context, such as a second user's, recorded as the `page` fixture is when videos are on. */
export function newContext(browser: Browser, options: BrowserContextOptions = {}): Promise<BrowserContext> {
  const recordVideo = on ? { recordVideo: { dir: test.info().outputDir, size, showActions: actions } } : {}
  return browser.newContext({ ...recordVideo, ...options })
}
