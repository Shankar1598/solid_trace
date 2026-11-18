import { expect, test } from '@playwright/test'
import { ORG } from '../support/data'
import { expectEditorText, holdSqlQueries, releaseSqlQueries } from '../support/logs'
import { watchErrors } from '../support/screens'

// SQL Monitors on the form, on an instance with SQL queries on: a member
// switches a Threshold monitor to SQL, which opens with the Query's readable
// SQL, previews the groups the SQL finds, gets SQL that does not run back at
// the SQL editor, saves it, and sees its language on its page and the list.
// Edit opens it in SQL again, and saving keeps it SQL.

test.beforeAll(() => { holdSqlQueries() })
test.afterAll(() => { releaseSqlQueries() })

// The groups preview answers SQL that does not run with a 422, which the browser logs.
const REFUSED_PREVIEW = /status of 422 \(Unprocessable Content\)/

test('a member makes a SQL Threshold monitor on the form, previews it, saves it and sees it in the list', async ({ page }) => {
  test.setTimeout(120_000)
  const errors = watchErrors(page, [REFUSED_PREVIEW])
  const name = `Flow SQL monitor ${Date.now()}`

  await page.goto(`/${ORG}/monitors/new`)
  await expect(page.getByRole('heading', { name: 'New monitor' })).toBeVisible()
  await page.getByLabel('Name').fill(name)
  await page.getByLabel('Over the last (minutes)').fill('1440')
  await page.getByLabel('Run every (minutes)').fill('60')
  await page.getByLabel('Threshold').fill('0')

  // The switch, as in the Logs explorer: SQL opens with the Query's readable SQL.
  const modes = page.getByRole('group', { name: 'Edit the Query in' })
  await modes.getByRole('button', { name: 'SQL', exact: true }).click()
  const box = page.getByLabel('SQL', { exact: true })
  await expectEditorText(box, /FROM logs/i)

  // A Match monitor runs a Query: the switch hides, and Threshold brings it back.
  await page.getByRole('group', { name: 'Kind' }).getByRole('button', { name: 'Match' }).click()
  await expect(modes.getByRole('button', { name: 'SQL', exact: true })).toHaveCount(0)
  await page.getByRole('group', { name: 'Kind' }).getByRole('button', { name: 'Threshold' }).click()
  await modes.getByRole('button', { name: 'SQL', exact: true }).click()

  // The preview lists the SQL's groups, with no chart.
  await box.fill('SELECT level, count() AS n FROM logs GROUP BY level')
  const wouldFire = page.locator('[data-monitor-groups]')
  await expect(wouldFire).toContainText(/\d+ groups? would fire now\./)
  await expect(wouldFire.locator('[data-group-firing="true"]').first()).toBeVisible()
  await expect(page.locator('[data-monitor-preview]')).toHaveCount(0)
  await expect(page.getByText('A SQL Monitor has no chart')).toBeVisible()

  // SQL that does not run: saving gives ClickHouse's message at the SQL editor.
  await box.fill('SELECT levl, count() AS n FROM logs GROUP BY levl')
  await page.getByRole('button', { name: 'Create monitor' }).click()
  await expect(page.locator('[data-monitor-sql]').getByRole('alert').filter({ hasText: /^SQL can't run: / })).toBeVisible()

  await box.fill('SELECT level, count() AS n FROM logs GROUP BY level')
  await page.getByRole('button', { name: 'Create monitor' }).click()
  await expect(page.getByRole('heading', { name })).toBeVisible()
  await expect(page.getByText('Threshold monitor in SQL on')).toBeVisible()
  await expect(page.locator('[data-monitor-language]')).toHaveText('SQL')
  await expect(page.locator('[data-monitor-query]')).toHaveText('SELECT level, count() AS n FROM logs GROUP BY level')

  // The list shows its language.
  await page.getByRole('link', { name: 'Monitors' }).first().click()
  const row = page.locator(`[data-monitor-row="${name}"]`)
  await expect(row.locator('[data-monitor-language]')).toHaveText('SQL')

  // Edit opens it in SQL, and saving keeps it SQL.
  await row.getByRole('link', { name }).click()
  await page.getByRole('link', { name: 'Edit' }).click()
  await expect(page.getByRole('heading', { name: `Edit ${name}` })).toBeVisible()
  await expectEditorText(page.getByLabel('SQL', { exact: true }), 'SELECT level, count() AS n FROM logs GROUP BY level')
  await page.getByRole('button', { name: 'Save monitor' }).click()
  await expect(page.getByRole('heading', { name })).toBeVisible()
  await expect(page.locator('[data-monitor-language]')).toHaveText('SQL')

  await page.getByRole('button', { name: 'Delete' }).click()
  await page.getByRole('alertdialog').getByRole('button', { name: 'Delete monitor' }).click()
  await expect(page.locator(`[data-monitor-row="${name}"]`)).toHaveCount(0)
  expect(errors).toEqual([])
})
