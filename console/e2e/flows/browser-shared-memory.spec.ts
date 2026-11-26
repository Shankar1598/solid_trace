import { chromium, expect, test } from '@playwright/test'
import { chmodSync, mkdtempSync, rmSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

// The browser of the flows keeps its shared memory in /dev/shm, not in
// TMPDIR. A full load of a page from Vite's dev server holds about 2 MB of
// shared memory for each module that answers 304, until garbage collection,
// so a flow with many loads can fill the dev box's disk. Then Chromium fails
// the module requests with ERR_INSUFFICIENT_RESOURCES and the page stays
// empty. Here Chromium's TMPDIR is read-only, and the page must still load.
test('the browser of the flows loads a page when its TMPDIR has no space', async ({ baseURL }) => {
  const dir = mkdtempSync(join(tmpdir(), 'flows-tmpdir-'))
  chmodSync(dir, 0o555)
  const options = test.info().project.use.launchOptions ?? {}
  try {
    const browser = await chromium.launch({ ...options, env: { ...options.env, TMPDIR: dir } })
    const page = await browser.newPage()
    const failed: string[] = []
    page.on('requestfailed', request => failed.push(`${request.failure()?.errorText} ${request.url()}`))
    await page.goto(`${baseURL}/login`)
    await expect(page.locator('input[type=email]')).toBeVisible()
    expect(failed).toEqual([])
    await browser.close()
  } finally {
    rmSync(dir, { recursive: true, force: true })
  }
})
