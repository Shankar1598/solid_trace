import { defineConfig, devices } from '@playwright/test'
import { existsSync } from 'node:fs'
import { homedir } from 'node:os'
import { join } from 'node:path'

// End-to-end flows and screenshots against the dev stack (Procfile.dev).
// `bun run e2e` runs the flows; `bun run e2e:screens` captures every screen
// at desktop and phone sizes in dark and light into SCREENS_DIR.
const baseURL = process.env.E2E_BASE_URL ?? 'http://localhost:4001'

// Chromium's system libraries, unpacked without root on the dev box.
const libs = process.env.PW_LIBS ?? join(homedir(), '.cache/solid-trace/pw/libs/usr/lib/x86_64-linux-gnu')
const launchEnv: Record<string, string> = existsSync(libs)
  ? { ...(process.env as Record<string, string>), LD_LIBRARY_PATH: [libs, process.env.LD_LIBRARY_PATH].filter(Boolean).join(':') }
  : (process.env as Record<string, string>)

const screens = [
  { name: 'screens-desktop-dark', viewport: { width: 1440, height: 900 }, colorScheme: 'dark' as const },
  { name: 'screens-desktop-light', viewport: { width: 1440, height: 900 }, colorScheme: 'light' as const },
  { name: 'screens-phone-dark', viewport: { width: 390, height: 844 }, colorScheme: 'dark' as const },
  { name: 'screens-phone-light', viewport: { width: 390, height: 844 }, colorScheme: 'light' as const },
]

export default defineConfig({
  testDir: './e2e',
  workers: 1,
  fullyParallel: false,
  retries: 0,
  timeout: 60_000,
  expect: { timeout: 15_000 },
  reporter: [['list']],
  globalSetup: './e2e/support/global-setup.ts',
  outputDir: './tmp/playwright',
  use: {
    baseURL,
    headless: true,
    storageState: './tmp/playwright-auth/admin.json',
    trace: 'retain-on-failure',
    launchOptions: { env: launchEnv },
  },
  webServer: {
    command: 'cd .. && PORT=4001 foreman start -f Procfile.dev',
    url: `${baseURL}/login`,
    reuseExistingServer: true,
    timeout: 180_000,
  },
  projects: [
    {
      name: 'flows',
      testMatch: /flows\/.*\.spec\.ts/,
      use: { ...devices['Desktop Chrome'], viewport: { width: 1440, height: 900 }, colorScheme: 'dark' },
    },
    ...screens.map(s => ({
      name: s.name,
      testMatch: /screens\/.*\.spec\.ts/,
      use: { viewport: s.viewport, colorScheme: s.colorScheme },
    })),
  ],
})
