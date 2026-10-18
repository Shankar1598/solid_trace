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
