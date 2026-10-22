import { test, expect, type Page } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import { resolve } from 'node:path'
import { MEMBER, ORG } from '../support/data'
import { watchErrors } from '../support/screens'

// A Project's Log file order on its settings page: an admin adds, removes
// and resets its Fields and is told why a Field isn't added; a member sees
// it read-only. Each run makes its own Project, so the seed Project's order
// stays the default.

const slug = `flow-file-order-${Date.now()}`
const projectPath = `/${ORG}/projects/${slug}`

const rails = (code: string) =>
  execFileSync('mise', ['exec', '--', 'bin/rails', 'runner', code], {
    cwd: resolve(__dirname, '../..'), stdio: 'pipe', timeout: 120_000,
  }).toString().trim()

const storedOrder = () =>
  rails(`print Project.find_by!(slug: ${JSON.stringify(slug)}).log_file_order.to_json`).split('\n').pop()

const summary = (page: Page) => page.getByTestId('log-file-order')

// The Log file order card's Save; Retention has one too.
const save = (page: Page) =>
  page.locator('form').filter({ has: page.getByTestId('log-file-order') }).getByRole('button', { name: 'Save', exact: true })

const addField = async (page: Page, name: string) => {
  await page.getByLabel('Add a Field').fill(name)
  await page.getByRole('button', { name: 'Add', exact: true }).click()
}

test.beforeAll(() => {
  rails(`Organization.find_by!(slug: ${JSON.stringify(ORG)}).projects.create!(name: ${JSON.stringify(slug)})`)
})

test.afterAll(() => {
  rails(`Project.where(slug: ${JSON.stringify(slug)}).destroy_all`)
})

test.describe('as a member', () => {
  test.use({ storageState: { cookies: [], origins: [] } })

  test('a member sees the Log file order read-only', async ({ page }) => {
    const errors = watchErrors(page)
    await page.goto('/login')
    await page.fill('input[type=email]', MEMBER.email)
    await page.fill('input[type=password]', MEMBER.password)
    await page.click('button[type=submit]')
    await page.waitForURL(url => !url.pathname.startsWith('/login'))

    await page.goto(projectPath)
    await expect(summary(page)).toHaveText('Default (service, file)')
    await expect(page.getByText('Only organization admins can change this.').last()).toBeVisible()
    await expect(page.getByRole('button', { name: /^Remove / })).toHaveCount(0)
    await expect(page.getByRole('button', { name: 'Reset to default' })).toHaveCount(0)
    await expect(page.getByLabel('Add a Field')).toHaveCount(0)
    expect(errors).toEqual([])
  })
})

test('an admin adds, removes and resets the Fields of a Log file order', async ({ page }) => {
  const errors = watchErrors(page)
  await page.goto(projectPath)
  await expect(summary(page)).toHaveText('Default (service, file)')

  await page.getByRole('button', { name: 'Remove file' }).click()
  await addField(page, 'level')
  await expect(summary(page)).toHaveText('service, level, then time')
  await save(page).click()
  await expect(save(page)).toBeDisabled()

  await page.reload()
  await expect(summary(page)).toHaveText('service, level, then time')
  expect(storedOrder()).toBe('["service","level"]')

  await page.getByRole('button', { name: 'Time only' }).click()
  await expect(summary(page)).toHaveText('Time only')
  await expect(page.getByRole('button', { name: /^Remove / })).toHaveCount(0)
  await expect.poll(storedOrder).toBe('[]')

  await page.getByRole('button', { name: 'Reset to default' }).click()
  await expect(summary(page)).toHaveText('Default (service, file)')
  await expect.poll(storedOrder).toBe('null')
  expect(errors).toEqual([])
})

test('an admin is told a Field that would be fourth or a repeat is not added', async ({ page }) => {
  await page.goto(projectPath)
  await expect(summary(page)).toHaveText('Default (service, file)')

  await addField(page, 'level')
  await addField(page, 'environment')
  await expect(page.getByText('A Log file order has at most 3 Fields')).toBeVisible()
  await expect(summary(page)).toHaveText('service, file, level, then time')

  await page.getByRole('button', { name: 'Remove level' }).click()
  await addField(page, 'service')
  await expect(page.getByText('service is in the order already')).toBeVisible()
  await expect(summary(page)).toHaveText('service, file, then time')
  expect(storedOrder()).toBe('null')
})
