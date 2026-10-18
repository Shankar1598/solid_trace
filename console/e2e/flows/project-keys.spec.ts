import { test, expect, type Page } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import { resolve } from 'node:path'
import { MEMBER, ORG } from '../support/data'
import { watchErrors } from '../support/screens'

// The Project page's keys, which only an Organization's admins change: an
// admin generates a key-less Project's key and sees its DSN, and a member
// sees no button to. Each run makes its own key-less Projects.

const stamp = Date.now()
const forAdmin = `flow-keys-admin-${stamp}`
const forMember = `flow-keys-member-${stamp}`

const rails = (code: string) =>
  execFileSync('mise', ['exec', '--', 'bin/rails', 'runner', code], {
    cwd: resolve(__dirname, '../..'), stdio: 'pipe', timeout: 120_000,
  })

test.beforeAll(() => {
  // Made without the key the New Project page gives, so the page offers one.
  rails(`org = Organization.find_by!(slug: ${JSON.stringify(ORG)}); ` +
    `${JSON.stringify([forAdmin, forMember])}.each { |slug| org.projects.create!(name: slug, slug: slug) }`)
})

test.afterAll(() => {
  rails(`Project.where(slug: ${JSON.stringify([forAdmin, forMember])}).destroy_all`)
})

const noKeys = (page: Page) => page.getByText('No client keys yet')

test("an admin generates a key-less Project's key and sees its DSN", async ({ page }) => {
  const errors = watchErrors(page)
  await page.goto(`/${ORG}/projects/${forAdmin}`)
  await expect(noKeys(page)).toBeVisible()

  await page.getByRole('button', { name: 'Generate a key' }).click()

  await expect(page.getByText('Key created successfully')).toBeVisible()
  await expect(noKeys(page)).toHaveCount(0)
  // The DSN, the page's one read-only box.
  await expect(page.locator('input[readonly]')).toHaveValue(/^https?:\/\/[0-9a-f]{32}@[^/]+\/\d+$/)
  expect(errors).toEqual([])
})

test.describe('as a member', () => {
  test.use({ storageState: { cookies: [], origins: [] } })

  test('a member sees no button to generate a key', async ({ page }) => {
    const errors = watchErrors(page)
    await page.goto('/login')
    await page.fill('input[type=email]', MEMBER.email)
    await page.fill('input[type=password]', MEMBER.password)
    await page.click('button[type=submit]')
    await page.waitForURL(url => !url.pathname.startsWith('/login'))

    await page.goto(`/${ORG}/projects/${forMember}`)
    await expect(noKeys(page)).toBeVisible()
    await expect(page.getByRole('button', { name: 'Generate a key' })).toHaveCount(0)
    await expect(page.getByText('Only an Organization admin can generate one.')).toBeVisible()
    expect(errors).toEqual([])
  })
})
