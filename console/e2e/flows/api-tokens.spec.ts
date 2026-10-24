import { test, expect } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import { resolve } from 'node:path'
import { ORG } from '../support/data'
import { watchErrors } from '../support/screens'

// Settings → API tokens & MCP: make a token, see its secret once with the snippets
// that add the MCP server, see it listed, and revoke it. Each run makes its
// own token.

const name = `Flow ${Date.now()}`

test.afterAll(() => {
  // So a run that fails before Revoke leaves no token behind.
  execFileSync('mise', ['exec', '--', 'bin/rails', 'runner', `ApiToken.where(name: ${JSON.stringify(name)}).delete_all`], {
    cwd: resolve(__dirname, '../..'), stdio: 'pipe', timeout: 120_000,
  })
})

test('an API token is shown once with its snippets, listed, and revoked', async ({ page }) => {
  const errors = watchErrors(page)
  // Reached from its own item in the Settings sidebar, which alone is lit.
  await page.goto(`/${ORG}/settings/user`)
  await expect(page.getByText('API tokens', { exact: true })).toHaveCount(0)
  await page.getByRole('link', { name: 'API tokens & MCP', exact: true }).click()
  await expect(page).toHaveURL(new RegExp(`/${ORG}/settings/api_tokens$`))
  await expect(page.getByRole('heading', { name: 'API tokens & MCP' })).toBeVisible()
  await expect(page.getByRole('link', { name: 'API tokens & MCP', exact: true })).toHaveClass(/bg-secondary/)
  await expect(page.getByRole('link', { name: 'Account', exact: true })).not.toHaveClass(/bg-secondary/)
  await expect(page.getByText('API tokens', { exact: true })).toBeVisible()

  await page.getByLabel('Token name').fill(name)
  await page.getByRole('button', { name: 'Make token' }).click()

  // The secret, once, and the snippets that use it.
  const made = page.getByRole('region', { name: 'New API token' })
  await expect(made).toContainText(/st_[A-Za-z0-9_-]{43}/)
  const secret = (await made.locator('code').textContent())!.trim()
  const shown = made.getByRole('tabpanel')
  await expect(shown).toContainText(
    new RegExp(`claude mcp add --transport http solidtrace https?://[^ ]+/mcp --header "Authorization: Bearer ${secret}"`),
  )
  await made.getByRole('tab', { name: 'Cursor' }).click()
  await expect(shown).toContainText('"mcpServers"')
  await expect(shown).toContainText(`"Authorization": "Bearer ${secret}"`)

  // Listed by name, creation and last use.
  const row = page.getByRole('table', { name: 'API tokens' }).getByRole('row').filter({ hasText: name })
  await expect(row).toContainText('ago')
  await expect(row).toContainText('Never')

  // Not shown again.
  await made.getByRole('button', { name: 'Done' }).click()
  await expect(made).toHaveCount(0)
  await page.reload()
  await expect(row).toBeVisible()
  await expect(page.getByText(secret)).toHaveCount(0)

  await row.getByRole('button', { name: `Revoke ${name}` }).click()
  await page.getByRole('alertdialog').getByRole('button', { name: 'Revoke token' }).click()
  await expect(page.getByText('API token revoked')).toBeVisible()
  await expect(row).toHaveCount(0)
  await expect(page).toHaveURL(new RegExp(`/${ORG}/settings/api_tokens$`))
  expect(errors).toEqual([])
})
