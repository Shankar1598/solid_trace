import { test, expect } from '@playwright/test'
import { MEMBER, ORG } from '../support/data'
import { watchErrors } from '../support/screens'

// The Overview's Storage: the seeded admin, an Instance admin, sees the
// whole install's disk against the disk limit, the bucket, the growth and
// the old Parquet files; a member who isn't one sees no Storage section.

test('an Instance admin sees the whole install\'s Storage on the Overview', async ({ page }) => {
  const errors = watchErrors(page)
  await page.goto(`/${ORG}/overview`)

  const storage = page.locator('[data-section="storage"]')
  await expect(storage.getByRole('heading', { name: 'Storage of the whole install' })).toBeVisible()
  await expect(storage).toContainText("not only this one's")
  await expect(storage.locator('[data-storage="local"]')).toContainText(/B on local disk of a .*B limit/)
  await expect(storage.getByRole('meter', { name: 'Local disk against the disk limit' })).toBeVisible()
  await expect(storage.locator('[data-storage-status]')).toContainText(/the limit/)
  await expect(storage.locator('[data-storage="free"]')).toContainText(/free of .*B on ClickHouse/)
  await expect(storage.locator('[data-storage="bucket"]')).toContainText(/of Logs and Spans|every Log and Span stays on local disk/)
  await expect(storage.locator('[data-storage="growth"]')).toContainText(/a day over the last 7 days/)
  await expect(storage.locator('[data-storage="old-files"]')).toContainText(/files from before the switch|None left/)

  expect(errors).toEqual([])
})

test.describe('as a member', () => {
  test.use({ storageState: { cookies: [], origins: [] } })

  test('a member who isn\'t an Instance admin sees no Storage section', async ({ page }) => {
    const errors = watchErrors(page)
    await page.goto('/login')
    await page.fill('input[type=email]', MEMBER.email)
    await page.fill('input[type=password]', MEMBER.password)
    await page.click('button[type=submit]')
    await page.waitForURL(url => !url.pathname.startsWith('/login'))

    await page.goto(`/${ORG}/overview`)
    await expect(page.getByRole('heading', { name: 'Overview', level: 1 })).toBeVisible()
    await expect(page.locator('[data-section="health"]')).toBeVisible()
    await expect(page.locator('[data-section="storage"]')).toHaveCount(0)

    expect(errors).toEqual([])
  })
})
