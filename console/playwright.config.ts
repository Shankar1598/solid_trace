import { defineConfig, devices } from '@playwright/test'
import { existsSync, readdirSync, readFileSync } from 'node:fs'
import { homedir } from 'node:os'
import { join } from 'node:path'
import { ADMIN_STATE } from './e2e/support/data'
import { video } from './e2e/support/video'

// End-to-end flows and screenshots against the dev stack (Procfile.dev).
// `bun run e2e` runs the flows; `bun run e2e:screens` captures every screen
// at desktop and phone sizes in dark and light into SCREENS_DIR.
// E2E_VIDEO=on records a video of each test into its output directory
// (e2e/support/video.ts).
//
// `bun run e2e` (e2e/run-flows) runs the flows in two runs on one stack.
// First the project flows, with E2E_WORKERS workers (default 3) at once:
// each worker signs in as its own users to its own Organization and Project
// (e2e/support/data.ts), so they do not see each other's records. Then the
// project flows-alone, with one worker: the flows in ALONE change the
// install, which every Organization sees.
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

// The flows that change the install, not only their Organization: an
// InstanceSetting (the Console URL, the Dispatcher URL, sign-up, email,
// retention, SQL queries), the factory's flow stack or demo (which set the
// Console and Dispatcher URLs), or EventStore's query port.
const ALONE = [
  'agent-sessions', 'dashboard-sql', 'demo', 'email-settings', 'factory-integration', 'instance-settings',
  'make-organization', 'mcp-oauth', 'mcp-oauth-client', 'palette', 'passkeys', 'runs', 'sign-up-closed',
  'trace-retention', 'ui-pass',
  // Its second test cuts EventStore's query port, and runs only with the port's switch.
  ...(process.env.E2E_QUERY_PORT_SWITCH ? ['monitor-status'] : []),
].map(name => `${name}.spec.ts`)

// A flow that changes the install but is not in ALONE would break the flows
// beside it now and then, so the config refuses to load.
const changesInstall = /InstanceSetting|instance_settings:set|setSqlQueries\(|support\/factoryFlow|factory-demo/
const flowsDir = join(__dirname, 'e2e/flows')
const notAlone = readdirSync(flowsDir).filter(file => !ALONE.includes(file) && changesInstall.test(readFileSync(join(flowsDir, file), 'utf8')))
if (notAlone.length) throw new Error(`playwright.config.ts: add ${notAlone.join(', ')} to ALONE, as it changes the install`)
const alone = new RegExp(`flows/(${ALONE.join('|').replace(/\./g, '\\.')})$`)

export default defineConfig({
  testDir: './e2e',
  workers: Number(process.env.E2E_WORKERS ?? 3),
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
    // The admin of this worker (e2e/support/data.ts). Each worker loads this file again with its own TEST_PARALLEL_INDEX.
    storageState: ADMIN_STATE,
    trace: 'retain-on-failure',
    video,
    // Playwright's --disable-dev-shm-usage puts Chromium's shared memory in
    // /tmp, on the dev box's nearly full disk (e2e/flows/browser-shared-memory.spec.ts).
    launchOptions: { env: launchEnv, ignoreDefaultArgs: ['--disable-dev-shm-usage'] },
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
      testIgnore: alone,
      use: { ...devices['Desktop Chrome'], viewport: { width: 1440, height: 900 }, colorScheme: 'dark' },
    },
    {
      name: 'flows-alone',
      testMatch: alone,
      workers: 1,
      outputDir: './tmp/playwright/alone',
      use: { ...devices['Desktop Chrome'], viewport: { width: 1440, height: 900 }, colorScheme: 'dark' },
    },
    ...screens.map(s => ({
      name: s.name,
      testMatch: /screens\/.*\.spec\.ts/,
      use: { viewport: s.viewport, colorScheme: s.colorScheme },
    })),
  ],
})
