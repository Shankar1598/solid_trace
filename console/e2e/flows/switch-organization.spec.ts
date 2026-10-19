import { test, expect, type Page } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import { resolve } from 'node:path'
import { MEMBER, ORG, logsPath } from '../support/data'
import { watchErrors } from '../support/screens'

// A user in two Organizations switches between them from the sidebar header
// and the command palette, and `/` follows the last one they opened. Each run
// makes its own user and second Organization, and deletes them after.

const stamp = Date.now()
const email = `flow-switch-${stamp}@solidtrace.local`
const password = 'password-1'
const name = `Flow Switch ${stamp}`
const slug = `flow-switch-${stamp}`
const project = 'flow-switch-project'

const rails = (code: string) =>
  execFileSync('mise', ['exec', '--', 'bin/rails', 'runner', code], {
    cwd: resolve(__dirname, '../..'), stdio: 'pipe', timeout: 120_000,
  })

test.use({ storageState: { cookies: [], origins: [] } })

test.beforeAll(() => {
  rails(`user = User.create!(name: "Flow Switch", email: ${JSON.stringify(email)}, password: ${JSON.stringify(password)}, password_confirmation: ${JSON.stringify(password)}); ` +
    `org = Organization.create!(name: ${JSON.stringify(name)}, slug: ${JSON.stringify(slug)}); ` +
    `org.projects.create!(name: "Flow Switch Project", slug: ${JSON.stringify(project)}); ` +
    `OrganizationUser.create!(organization: org, user: user, role: :admin); ` +
    `OrganizationUser.create!(organization: Organization.find_by!(slug: ${JSON.stringify(ORG)}), user: user, role: :member)`)
})

test.afterAll(() => {
  rails(`Organization.where(slug: ${JSON.stringify(slug)}).each { |org| OrganizationUser.where(organization: org).delete_all; org.projects.each(&:destroy!); org.destroy! }; ` +
    `User.where(email: ${JSON.stringify(email)}).each { |user| OrganizationUser.where(user: user).delete_all; user.destroy! }`)
})

async function signIn(page: Page, as: { email: string, password: string }) {
  await page.goto('/login')
  await page.fill('input[type=email]', as.email)
  await page.fill('input[type=password]', as.password)
  await page.click('button[type=submit]')
  await page.waitForURL(url => !url.pathname.startsWith('/login'))
}

const palette = (page: Page) => page.getByRole('dialog', { name: 'Command palette' })

test('a user in two Organizations switches from Logs, keeps it on /, and jumps back from the palette', async ({ page }) => {
  const errors = watchErrors(page)
  await signIn(page, { email, password })

  // From a Project's Logs, the header lists both, the current one marked.
  await page.goto(logsPath('?range=1h'))
  await page.getByRole('button', { name: 'Switch Organization' }).click()
  await expect(page.getByRole('menuitem')).toHaveText([name, 'SolidTrace Org'])
  await expect(page.getByRole('menuitem', { name: 'SolidTrace Org' })).toHaveAttribute('aria-current', 'page')
  await expect(page.getByRole('menuitem', { name })).not.toHaveAttribute('aria-current')

  // Picking the other lands on its Logs.
  await page.getByRole('menuitem', { name }).click()
  await page.waitForURL(url => url.pathname === `/${slug}/projects/${project}/logs`)
  await expect(page.getByRole('button', { name: 'Switch Organization' })).toContainText(name)

  // `/` keeps them there.
  await page.goto('/')
  await page.waitForURL(url => url.pathname === `/${slug}/issues`)

  // The palette's Organizations group lists only the other one, and jumps back.
  await page.keyboard.press('Control+k')
  await expect(palette(page)).toBeVisible()
  const group = palette(page).getByRole('group', { name: 'Organizations' })
  await expect(group.getByRole('option')).toHaveCount(1)
  await group.getByRole('option', { name: /^SolidTrace Org/ }).click()
  await page.waitForURL(url => url.pathname === `/${ORG}/issues`)

  await page.goto('/')
  await page.waitForURL(url => url.pathname === `/${ORG}/issues`)
  expect(errors).toEqual([])
})

test('a user in one Organization sees no dropdown', async ({ page }) => {
  const errors = watchErrors(page)
  await signIn(page, MEMBER)
  await page.goto(`/${ORG}/issues`)
  await expect(page.getByRole('navigation', { name: 'Main' })).toBeVisible()
  await expect(page.getByText('SolidTrace Org').first()).toBeVisible()
  await expect(page.getByRole('button', { name: 'Switch Organization' })).toHaveCount(0)
  expect(errors).toEqual([])
})
