import { test, expect, type Page } from '@playwright/test'
import { ORG } from '../support/data'
import { watchErrors } from '../support/screens'

// User settings' profile form saves the name from both places the page is
// served, with and without an Organization, and the name is put back after.
// The Passkeys card has a Name field too when the page is at the Console URL,
// so the flow finds the profile's Name in the Profile form.

const profileName = (page: Page) => page.getByRole('form', { name: 'Profile' }).getByLabel('Name', { exact: true })

async function saveName(page: Page, path: string, name: string) {
  await page.goto(path)
  await profileName(page).fill(name)
  await page.getByRole('button', { name: 'Save changes' }).click()
  await expect(page.getByText('User settings updated')).toBeVisible()
  await page.reload()
  await expect(profileName(page)).toHaveValue(name)
}

// The title names the path without the Organization's slug, which differs
// in each Playwright worker (e2e/support/data.ts), and Playwright finds a test
// in its worker by its title.
for (const [shown, path] of [['/:org/settings/user', `/${ORG}/settings/user`], ['/user_settings', '/user_settings']]) {
  test(`the profile form saves the name at ${shown}`, async ({ page }) => {
    const errors = watchErrors(page)
    await page.goto(path)
    const before = await profileName(page).inputValue()

    await saveName(page, path, `${before} Flow`)
    await saveName(page, path, before)

    expect(errors).toEqual([])
  })
}

test('Account is about the profile alone, and lit alone in the sidebar', async ({ page }) => {
  await page.goto(`/${ORG}/settings/user`)
  await expect(profileName(page)).toBeVisible()
  await expect(page.getByRole('link', { name: 'Account', exact: true })).toHaveClass(/bg-secondary/)
  await expect(page.getByRole('link', { name: 'API tokens & MCP', exact: true })).not.toHaveClass(/bg-secondary/)
  await expect(page.getByLabel('Token name')).toHaveCount(0)
})
