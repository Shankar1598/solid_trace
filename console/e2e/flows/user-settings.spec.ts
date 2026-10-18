import { test, expect, type Page } from '@playwright/test'
import { ORG } from '../support/data'
import { watchErrors } from '../support/screens'

// User settings' profile form saves the name from both places the page is
// served, with and without an Organization, and the name is put back after.

async function saveName(page: Page, path: string, name: string) {
  await page.goto(path)
  await page.getByLabel('Name', { exact: true }).fill(name)
  await page.getByRole('button', { name: 'Save changes' }).click()
  await expect(page.getByText('User settings updated')).toBeVisible()
  await page.reload()
  await expect(page.getByLabel('Name', { exact: true })).toHaveValue(name)
}

for (const path of [`/${ORG}/settings/user`, '/user_settings']) {
  test(`the profile form saves the name at ${path}`, async ({ page }) => {
    const errors = watchErrors(page)
    await page.goto(path)
    const before = await page.getByLabel('Name', { exact: true }).inputValue()

    await saveName(page, path, `${before} Flow`)
    await saveName(page, path, before)

    expect(errors).toEqual([])
  })
}
