import { test, expect } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import { resolve } from 'node:path'
import { ORG } from '../support/data'
import { watchErrors } from '../support/screens'

// Trace Retention: the seeded admin, an instance admin, changes the default
// on Settings → Instance, then a Project's own on its page, and clears it back
// to the default. A Retention below a day is refused. Each run makes its own
// Project, and the default is put back after.

const slug = `flow-trace-retention-${Date.now()}`

const rails = (code: string) =>
  execFileSync('mise', ['exec', '--', 'bin/rails', 'runner', code], {
    cwd: resolve(__dirname, '../..'), stdio: 'pipe', timeout: 120_000,
  }).toString().trim().split('\n').pop()!

let saved = '14'

test.beforeAll(() => {
  saved = rails('print InstanceSetting.current.default_trace_retention_days')
  rails(`InstanceSetting.current.update!(default_trace_retention_days: 14); ` +
    `Organization.find_by!(slug: ${JSON.stringify(ORG)}).projects.create!(name: ${JSON.stringify(slug)}, slug: ${JSON.stringify(slug)})`)
})

test.afterAll(() => {
  rails(`InstanceSetting.current.update!(default_trace_retention_days: ${Number(saved) || 14}); ` +
    `Project.where(slug: ${JSON.stringify(slug)}).destroy_all`)
})

test("an admin changes the default trace Retention and a Project's own", async ({ page }) => {
  const errors = watchErrors(page)

  await page.goto(`/${ORG}/settings/instance`)
  const instanceDefault = page.getByLabel('Default trace Retention (days)')
  await instanceDefault.fill('0')
  await page.getByRole('button', { name: 'Save changes' }).click()
  await expect(page.getByText('must be greater than 0')).toBeVisible()

  await instanceDefault.fill('7')
  await page.getByRole('button', { name: 'Save changes' }).click()
  await expect(page.getByText('Instance settings saved')).toBeVisible()
  await page.reload()
  await expect(instanceDefault).toHaveValue('7')

  await page.goto(`/${ORG}/projects/${slug}`)
  const own = page.getByLabel('Traces (days)')
  const retentionForm = page.locator('form').filter({ has: own })
  await expect(own).toHaveValue('')
  await expect(own).toHaveAttribute('placeholder', 'Instance default (7)')

  await own.fill('3')
  await retentionForm.getByRole('button', { name: 'Save' }).click()
  await expect(page.getByText('Project updated successfully')).toBeVisible()
  await page.reload()
  await expect(own).toHaveValue('3')

  await own.fill('')
  await retentionForm.getByRole('button', { name: 'Save' }).click()
  await expect(page.getByText('Project updated successfully')).toBeVisible()
  await page.reload()
  await expect(own).toHaveValue('')

  expect(errors).toEqual([])
})
