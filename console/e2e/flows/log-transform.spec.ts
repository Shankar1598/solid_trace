import { test, expect, type Page } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import { resolve } from 'node:path'
import { ADMIN, MEMBER, ORG } from '../support/data'
import { expectEditorText, settled, streamRows } from '../support/logs'
import { watchErrors } from '../support/screens'

// A Project's Log transform page: an admin opens it from the Project's
// settings, sees a mistake underlined as they type, saves the fixed program,
// finds its Attribute on a test Log, and restores the earlier version; a
// member reads it only. Each run makes its own Project, with a key for the
// test Log and one saved version to restore, since a test Log leaves it
// empty no more.

const slug = `flow-log-transform-${Date.now()}`
const SEEDED = '.flow_check = "seeded"'
const FIXED = '.flow_check = "transformed"'

const rails = (code: string) =>
  execFileSync('mise', ['exec', '--', 'bin/rails', 'runner', code], {
    cwd: resolve(__dirname, '../..'), stdio: 'pipe', timeout: 120_000,
  })

test.beforeAll(() => {
  rails(`org = Organization.find_by!(slug: ${JSON.stringify(ORG)}); ` +
    `project = org.projects.build_with_key(name: ${JSON.stringify(slug)}, slug: ${JSON.stringify(slug)}); project.save!; ` +
    `project.log_transform_versions.create!(program: ${JSON.stringify(SEEDED)}, user: User.find_by!(email: ${JSON.stringify(ADMIN.email)}))`)
})

test.afterAll(() => {
  rails(`Project.find_by(slug: ${JSON.stringify(slug)})&.destroy`)
})

const program = (page: Page) => page.getByLabel('Program', { exact: true })
const versionRows = (page: Page) => page.getByRole('row').filter({ has: page.getByRole('button', { name: 'View' }) })

/** Opens the Project's settings, then its Log transform page from there. */
async function openFromSettings(page: Page) {
  await page.goto(`/${ORG}/projects/${slug}`)
  await page.getByRole('link', { name: 'Open the Log transform' }).click()
  await expect(page).toHaveURL(new RegExp(`/${ORG}/projects/${slug}/log_transform$`))
  await expect(page.getByRole('heading', { name: 'Log transform' })).toBeVisible()
}

/** Replaces the program's text, as a paste would. */
async function replaceProgram(page: Page, text: string) {
  await program(page).click()
  await page.keyboard.press('Control+A')
  await page.keyboard.insertText(text)
  await expectEditorText(program(page), text)
}

test('an admin fixes a mistake, saves, finds the Attribute on a test Log, and restores the earlier version', async ({ page }) => {
  const errors = watchErrors(page)
  await openFromSettings(page)
  await expectEditorText(program(page), SEEDED)
  await expect(versionRows(page)).toHaveCount(1)

  // A call to a function VRL doesn't have is underlined where it is.
  await replaceProgram(page, '.flow_check = nope("transformed")')
  await expect(page.locator('.cm-lintRange-error')).toHaveText('nope')
  await expect(page.getByText('1 error', { exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Save', exact: true })).toBeDisabled()

  await replaceProgram(page, FIXED)
  await expect(page.getByText('It compiles.')).toBeVisible()
  await expect(page.locator('.cm-lintRange-error')).toHaveCount(0)

  // Save asks first, saying stored Logs don't change.
  await page.getByRole('button', { name: 'Save', exact: true }).click()
  const confirm = page.getByRole('alertdialog')
  await expect(confirm).toContainText("Logs already stored don't change.")
  await confirm.getByRole('button', { name: 'Save', exact: true }).click()
  await expect(page.getByText(/^Log transform saved\./)).toBeVisible()
  await expect(versionRows(page)).toHaveCount(2)
  await expectEditorText(program(page), FIXED)
  const failed = page.getByRole('link', { name: /^\d+$/ })
  await expect(failed).toHaveAttribute('href', /isnotnull%28solidtrace\.transform_error%29|isnotnull\(solidtrace\.transform_error\)/)

  // A test Log sent after the save has the Attribute the program sets, once Log processing has stored it.
  await page.goto(`/${ORG}/projects/${slug}/logs?range=1h`)
  await page.getByRole('button', { name: 'Send a test Log' }).click()
  await expect(page.getByText(/A test Log from SolidTrace/).first()).toBeVisible({ timeout: 20_000 })
  const inspector = page.getByRole('complementary', { name: 'Log inspector' })
  await expect(async () => {
    await page.reload()
    await settled(page)
    await streamRows(page).first().click({ position: { x: 60, y: 15 } })
    await expect(inspector).toContainText('flow_check', { timeout: 2_000 })
  }).toPass({ timeout: 30_000 })
  await expect(inspector).toContainText('transformed')

  // Restoring the earlier version saves it again as the newest.
  await page.goto(`/${ORG}/projects/${slug}/log_transform`)
  await page.getByRole('button', { name: /^Restore the version saved / }).click()
  await expect(confirm).toContainText('It is saved again as the newest version.')
  await confirm.getByRole('button', { name: 'Restore', exact: true }).click()
  await expect(page.getByText(/^Log transform saved\./)).toBeVisible()
  await expectEditorText(program(page), SEEDED)
  await expect(versionRows(page)).toHaveCount(3)
  expect(errors).toEqual([])
})

test.describe('as a member', () => {
  test.use({ storageState: { cookies: [], origins: [] } })

  test('a member reads the Log transform, with no Save or Restore', async ({ page }) => {
    const errors = watchErrors(page)
    await page.goto('/login')
    await page.fill('input[type=email]', MEMBER.email)
    await page.fill('input[type=password]', MEMBER.password)
    await page.click('button[type=submit]')
    await page.waitForURL(url => !url.pathname.startsWith('/login'))

    await openFromSettings(page)
    await expectEditorText(program(page), /^\.flow_check = /)
    await expect(program(page)).not.toBeEditable()
    await expect(page.getByText('Only Organization admins can save or restore the Log transform.')).toBeVisible()
    await expect(page.getByRole('button', { name: 'Save', exact: true })).toHaveCount(0)
    await expect(page.getByRole('button', { name: /^Restore/ })).toHaveCount(0)
    await expect(versionRows(page).first()).toBeVisible()
    expect(errors).toEqual([])
  })
})
