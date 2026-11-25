import { expect, type APIRequestContext, type Page } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { gzipSync } from 'node:zlib'
import { PROJECT } from './data'

// What the OTLP flows (logs-otlp, logs-otlp-metrics, claude-code-otel) share: the ingest URL,
// the captured fixtures, the Project's key, a post to an OTLP route, the
// explorer's search for the Logs that a post made, and the inspector.

export const ingest = process.env.E2E_INGEST_URL ?? 'http://localhost:4000'

const fixtures = resolve(__dirname, '../../../event_store/ingest/normalise/testdata/otlp')

/** The bytes of a captured export, by file name, for example `collector.pb`. */
export const fixture = (name: string): Buffer => readFileSync(resolve(fixtures, name))

/** The public key of the Project's first key. */
export const key = execFileSync('mise', ['exec', '--', 'bin/rails', 'runner',
  `puts Project.find_by!(slug: ${JSON.stringify(PROJECT)}).project_keys.first.public_key`], {
  cwd: resolve(__dirname, '../..'), stdio: 'pipe', timeout: 120_000,
}).toString().trim()

/** Posts an export to an OTLP route (`/v1/logs` or `/v1/metrics`) with the Project's key, and expects a 200. */
export async function postOtlp(request: APIRequestContext, route: string, body: Buffer, type: string, gzip = false) {
  const response = await request.post(`${ingest}${route}`, {
    headers: { Authorization: `Bearer ${key}`, 'Content-Type': type, ...(gzip ? { 'Content-Encoding': 'gzip' } : {}) },
    data: gzip ? gzipSync(body) : body,
  })
  expect(response.status(), await response.text()).toBe(200)
}

/**
 * The search of the Logs explorer for Logs from the start of a day (UTC,
 * `2026-11-24`) to an hour ahead. The fixtures' Logs keep their own time until
 * the Project's Retention passes it. Then they take the time they arrived.
 * This range holds both. The Table view needs a Query that the Builder cannot
 * make (a summarize), so it opens the explorer in its Query mode.
 */
export function logsSince(day: string, query: string, view: 'stream' | 'table' = 'stream'): string {
  const from = `${day}T00:00:00.000Z`
  const to = new Date(Date.now() + 3600_000).toISOString()
  const params = new URLSearchParams({ from, to, mode: view === 'table' ? 'query' : 'builder', q: query })
  if (view === 'table') params.set('viz', 'table')
  return `?${params}`
}

export const inspector = (page: Page) => page.getByRole('complementary', { name: 'Log inspector' })

/** An inspector row by its Field name, its value in the second cell. */
export const field = (page: Page, name: string) =>
  inspector(page).locator('div.group', { has: page.getByText(name, { exact: true }) }).first()
