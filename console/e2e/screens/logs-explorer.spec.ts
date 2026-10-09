import { test, expect } from '@playwright/test'
import { logsPath } from '../support/data'
import { shoot, useTheme, watchErrors } from '../support/screens'

test.beforeEach(async ({ page }, info) => { await useTheme(page, info) })

test('Logs explorer: Builder with the stream', async ({ page }) => {
  const errors = watchErrors(page)
  await page.goto(logsPath('?range=24h'))
  await expect(page.getByText(/Logs/).first()).toBeVisible()
  await page.waitForTimeout(1500)
  await shoot(page, 'logs-builder')
  expect(errors).toEqual([])
})
