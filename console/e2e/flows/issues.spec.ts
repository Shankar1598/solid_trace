import { test, expect, type Page } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import { resolve } from 'node:path'
import { ORG } from '../support/data'
import { watchErrors } from '../support/screens'

// The Issues page's filters and table, on Issues each run makes in its own
// Project of the seed Organization, last seen today, 5 days ago and 20 days
// ago.

const slug = `flow-issues-${Date.now()}`

const rails = (code: string) =>
  execFileSync('mise', ['exec', '--', 'bin/rails', 'runner', code], {
    cwd: resolve(__dirname, '../..'), stdio: 'pipe', timeout: 120_000,
  }).toString().trim()

let projectId: string

test.beforeAll(() => {
  projectId = rails(`project = Organization.find_by!(slug: ${JSON.stringify(ORG)}).projects.create!(name: ${JSON.stringify(slug)}); ` +
    `{ "Flow seen today" => 1.hour, "Flow seen 5 days ago" => 5.days, "Flow seen 20 days ago" => 20.days }.each { |title, ago| ` +
    `project.issues.create!(title: title, kind: :error, first_seen_at: ago.ago - 1.hour, last_seen_at: ago.ago, times_seen: 1) }; ` +
    `print project.id`).split('\n').pop()!
})

test.afterAll(() => {
  rails(`Project.where(slug: ${JSON.stringify(slug)}).each { |project| ProjectIssueCounter.where(project: project).delete_all; project.destroy! }`)
})

const titles = (page: Page) => page.locator('tbody h3')

async function chooseRange(page: Page, from: string, to: string) {
  await page.getByRole('combobox').filter({ hasText: from }).click()
  await page.getByRole('option', { name: to, exact: true }).click()
}

test('the time filter lists the Issues seen in the range chosen', async ({ page }) => {
  const errors = watchErrors(page)
  await page.goto(`/${ORG}/issues?project_id=${projectId}`)
  await expect(titles(page)).toHaveText(['Flow seen today', 'Flow seen 5 days ago'])

  await chooseRange(page, 'Last 14 days', 'Last 24 hours')
  await expect(titles(page)).toHaveText(['Flow seen today'])
  await expect(page).toHaveURL(/range=24h/)

  await chooseRange(page, 'Last 24 hours', 'Last 30 days')
  await expect(titles(page)).toHaveText(['Flow seen today', 'Flow seen 5 days ago', 'Flow seen 20 days ago'])

  expect(errors).toEqual([])
})

test('ticked Issues are resolved and unresolved together', async ({ page }) => {
  const errors = watchErrors(page)
  await page.goto(`/${ORG}/issues?project_id=${projectId}&range=30d`)
  const resolved = page.locator('tbody').getByText('Resolved', { exact: true })
  await expect(titles(page)).toHaveCount(3)

  await page.getByRole('checkbox', { name: 'Select Flow seen today' }).click()
  await page.getByRole('checkbox', { name: 'Select Flow seen 20 days ago' }).click()
  await expect(page.getByText('2 selected')).toBeVisible()
  await page.getByRole('button', { name: 'Resolve', exact: true }).click()

  await expect(page.getByText('2 Issues resolved')).toBeVisible()
  await expect(resolved).toHaveCount(2)
  await expect(page.getByText('2 selected')).toHaveCount(0)

  await page.getByRole('checkbox', { name: 'Select all Issues on this page' }).click()
  await expect(page.getByText('3 selected')).toBeVisible()
  await page.getByRole('button', { name: 'Unresolve', exact: true }).click()

  await expect(page.getByText('3 Issues unresolved')).toBeVisible()
  await expect(resolved).toHaveCount(0)
  expect(errors).toEqual([])
})

test("an Issue's age counts from its first Event, though the Issues were stored together", async ({ page }) => {
  const errors = watchErrors(page)
  await page.goto(`/${ORG}/issues?project_id=${projectId}&range=30d`)
  const age = (title: string) => page.getByRole('row').filter({ hasText: title }).getByRole('cell').nth(3)

  await expect(age('Flow seen today')).toHaveText('2 hours')
  await expect(age('Flow seen 5 days ago')).toHaveText('5 days')
  await expect(age('Flow seen 20 days ago')).toHaveText('20 days')
  expect(errors).toEqual([])
})
