import { test, type Page } from '@playwright/test'
import { ORG, PROJECT } from '../support/data'
import { openLogs, streamRows } from '../support/logs'
import { shoot, useTheme } from '../support/screens'

// Every page in the Console, for reviewing the design language as a whole.
// Run `bin/rails db:seed dev:data` first so every page has something to show.

test.beforeEach(async ({ page }, info) => { await useTheme(page, info) })

const pages: [string, string][] = [
  ['projects', `/${ORG}/projects`],
  ['project-new', `/${ORG}/projects/new`],
  ['project', `/${ORG}/projects/${PROJECT}`],
  ['issues', `/${ORG}/issues`],
  ['logs', `/${ORG}/projects/${PROJECT}/logs?range=24h`],
  ['dashboards', `/${ORG}/dashboards`],
  ['monitors', `/${ORG}/monitors`],
  ['monitor-new', `/${ORG}/monitors/new`],
  ['settings-organization', `/${ORG}/settings/organization`],
  ['settings-members', `/${ORG}/settings/organization_users`],
  ['settings-integrations', `/${ORG}/settings/integrations`],
  ['settings-integration-new', `/${ORG}/settings/integrations/new`],
  ['settings-user', `/${ORG}/settings/user`],
]

for (const [name, path] of pages) {
  test(`tour: ${name}`, async ({ page }) => {
    await page.goto(path)
    await page.waitForTimeout(1000)
    await shoot(page, `tour-${name}`)
  })
}

// Show pages are reached through their index by the name `bin/rails
// dev:data` gives them, so the tour shows that data rather than whatever
// a flow test left behind.
async function openFrom(page: Page, index: string, name: string) {
  await page.goto(index)
  await page.getByRole('link', { name, exact: true }).first().click()
  await page.waitForLoadState('networkidle').catch(() => {})
  await page.waitForTimeout(1000)
}

for (const tab of ['overview', 'events', 'comments']) {
  test(`tour: issue ${tab}`, async ({ page }) => {
    await page.goto(`/${ORG}/projects/${PROJECT}/issues/1`)
    await page.getByRole('tab', { name: new RegExp(`^${tab}`, 'i') }).click()
    await page.waitForTimeout(1000)
    await shoot(page, `tour-issue-${tab}`)
  })
}

test('tour: logs inspector', async ({ page }) => {
  await openLogs(page)
  await streamRows(page).nth(3).click({ position: { x: 60, y: 15 } })
  await page.getByRole('complementary', { name: 'Log inspector' }).waitFor()
  await page.waitForTimeout(500)
  await shoot(page, 'tour-logs-inspector')
})

test('tour: dashboard', async ({ page }) => {
  await openFrom(page, `/${ORG}/dashboards`, 'Checkout service health')
  await shoot(page, 'tour-dashboard')
})

test('tour: monitor', async ({ page }) => {
  await openFrom(page, `/${ORG}/monitors`, 'Server errors on checkout')
  await shoot(page, 'tour-monitor')
})

test('tour: monitor edit', async ({ page }) => {
  await openFrom(page, `/${ORG}/monitors`, 'Server errors on checkout')
  await page.goto(`${page.url()}/edit`)
  await page.waitForTimeout(1000)
  await shoot(page, 'tour-monitor-edit')
})

test('tour: settings integration edit', async ({ page }) => {
  await page.goto(`/${ORG}/settings/integrations`)
  await page.locator('a[href$="/edit"]').first().click()
  await page.waitForLoadState('networkidle').catch(() => {})
  await page.waitForTimeout(1000)
  await shoot(page, 'tour-settings-integration-edit')
})

test.describe('signed out', () => {
  test.use({ storageState: { cookies: [], origins: [] } })
  test('tour: login', async ({ page }) => {
    await page.goto('/login')
    await shoot(page, 'tour-login')
  })
  test('tour: register', async ({ page }) => {
    await page.goto('/registration/new')
    await shoot(page, 'tour-register')
  })
})
