import { test, expect } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import { resolve } from 'node:path'
import { MEMBER, ORG } from '../support/data'
import { watchErrors } from '../support/screens'

// Deleting a Project from its page, which only an Organization's admins
// can: the button asks first, and Cancel keeps the Project. Each run makes
// its own Project, with an Issue so that it has the rows EventStore writes.

const slug = `flow-delete-${Date.now()}`

const rails = (code: string) =>
  execFileSync('mise', ['exec', '--', 'bin/rails', 'runner', code], {
    cwd: resolve(__dirname, '../..'), stdio: 'pipe', timeout: 120_000,
  }).toString().trim()

test.beforeAll(() => {
  rails(`project = Organization.find_by!(slug: ${JSON.stringify(ORG)}).projects.create!(name: ${JSON.stringify(slug)}); ` +
    `project.issues.create!(title: "Flow delete", kind: :error)`)
})

test.afterAll(() => {
  rails(`Project.where(slug: ${JSON.stringify(slug)}).destroy_all`)
})

test.describe('as a member', () => {
  test.use({ storageState: { cookies: [], origins: [] } })

  test('a member sees no button to delete a Project', async ({ page }) => {
    const errors = watchErrors(page)
    await page.goto('/login')
    await page.fill('input[type=email]', MEMBER.email)
    await page.fill('input[type=password]', MEMBER.password)
    await page.click('button[type=submit]')
    await page.waitForURL(url => !url.pathname.startsWith('/login'))

    await page.goto(`/${ORG}/projects/${slug}`)
    await expect(page.getByRole('heading', { name: slug })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Delete project' })).toHaveCount(0)
    expect(errors).toEqual([])
  })
})

test('an admin deletes a Project after confirming', async ({ page }) => {
  const errors = watchErrors(page)
  await page.goto(`/${ORG}/projects/${slug}`)

  await page.getByRole('button', { name: 'Delete project' }).click()
  const dialog = page.getByRole('alertdialog')
  await expect(dialog.getByText(`Delete "${slug}"?`)).toBeVisible()
  await dialog.getByRole('button', { name: 'Cancel' }).click()
  await expect(dialog).toHaveCount(0)

  await page.getByRole('button', { name: 'Delete project' }).click()
  await dialog.getByRole('button', { name: 'Delete project' }).click()

  await page.waitForURL(url => url.pathname === `/${ORG}/projects`)
  await expect(page.getByText('Project deleted.')).toBeVisible()
  await expect(page.getByText(slug)).toHaveCount(0)
  expect(rails(`print Project.where(slug: ${JSON.stringify(slug)}).count`).split('\n').pop()).toBe('0')
  expect(errors).toEqual([])
})
