import { test, expect } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import { resolve } from 'node:path'
import { ORG } from '../support/data'
import { watchErrors } from '../support/screens'

// An admin invites someone new from the Members page and copies their invite
// link from the Invited row. The invited person opens it in their own browser,
// sets a name and password, and lands on the Organization's Overview; the link
// then stops working. Each run invites its own email, removes the member from
// the page at the end, and deletes the user after.

const stamp = Date.now()
const email = `flow-invite-${stamp}@solidtrace.local`
const invitedName = `Flow Invite ${stamp}`
const chosenName = `Flow Joined ${stamp}`
const membersPath = `/${ORG}/settings/organization_users`

const rails = (code: string) =>
  execFileSync('mise', ['exec', '--', 'bin/rails', 'runner', code], {
    cwd: resolve(__dirname, '../..'), stdio: 'pipe', timeout: 120_000,
  })

test.use({ permissions: ['clipboard-read', 'clipboard-write'] })

test.afterAll(() => {
  rails(`User.where(email: ${JSON.stringify(email)}).each { |user| OrganizationUser.where(user: user).delete_all; user.destroy! }`)
})

test('an invited person joins through the link copied from their Invited row', async ({ page, browser }) => {
  const errors = watchErrors(page)
  await page.goto(membersPath)
  await page.getByLabel('Email').fill(email)
  await page.getByLabel('Name (optional)').fill(invitedName)
  await page.getByRole('button', { name: 'Invite', exact: true }).click()
  await expect(page.getByText(new RegExp(`^${invitedName} invited`))).toBeVisible()

  const row = page.getByRole('row').filter({ hasText: email })
  await expect(row.getByText('Invited', { exact: true })).toBeVisible()
  await expect(row.getByRole('button', { name: `Revoke invite for ${invitedName}` })).toBeVisible()

  await row.getByRole('button', { name: `Copy invite link for ${invitedName}` }).click()
  await expect(page.getByText('Invite link copied')).toBeVisible()
  const link = await page.evaluate(() => navigator.clipboard.readText())
  expect(new URL(link).pathname).toMatch(/^\/invites\//)

  // The invited person, in a browser that nobody is signed in to.
  const invitedContext = await browser.newContext({ storageState: { cookies: [], origins: [] } })
  const invited = await invitedContext.newPage()
  const invitedErrors = watchErrors(invited)
  await invited.goto(link)
  await expect(invited.getByLabel('Email')).toHaveValue(email)
  await expect(invited.getByLabel('Name')).toHaveValue(invitedName)
  await invited.getByLabel('Name').fill(chosenName)
  await invited.getByLabel('Password', { exact: true }).fill('invite-password-1')
  await invited.getByLabel('Confirm password').fill('invite-password-1')
  await invited.getByRole('button', { name: 'Set password and sign in' }).click()
  await invited.waitForURL(url => url.pathname === `/${ORG}/overview`)

  // The link worked once, and only once.
  await invited.goto(link)
  await expect(invited.getByText('Invite link expired')).toBeVisible()
  expect(invitedErrors).toEqual([])
  await invitedContext.close()

  // Back on the admin's page, they're an ordinary member now.
  await page.reload()
  const joined = page.getByRole('row').filter({ hasText: email })
  await expect(joined.getByText(chosenName)).toBeVisible()
  await expect(joined.getByText('Invited', { exact: true })).toHaveCount(0)
  await expect(joined.getByRole('button', { name: `Copy invite link for ${chosenName}` })).toHaveCount(0)

  let asked = ''
  page.once('dialog', dialog => { asked = dialog.message(); void dialog.accept() })
  await joined.getByRole('button', { name: `Remove ${chosenName}` }).click()
  await expect.poll(() => asked).toBe('Are you sure you want to remove this member?')
  await expect(page.getByRole('row').filter({ hasText: email })).toHaveCount(0)
  expect(errors).toEqual([])
})

test('revoking an invite takes the Invited row away, and its link stops working', async ({ page, browser }) => {
  const revokedEmail = `flow-revoke-${stamp}@solidtrace.local`
  const revokedName = `Flow Revoke ${stamp}`
  try {
    const errors = watchErrors(page)
    await page.goto(membersPath)
    await page.getByLabel('Email').fill(revokedEmail)
    await page.getByLabel('Name (optional)').fill(revokedName)
    await page.getByRole('button', { name: 'Invite', exact: true }).click()
    const row = page.getByRole('row').filter({ hasText: revokedEmail })
    await expect(row.getByText('Invited', { exact: true })).toBeVisible()

    await row.getByRole('button', { name: `Copy invite link for ${revokedName}` }).click()
    await expect(page.getByText('Invite link copied')).toBeVisible()
    const link = await page.evaluate(() => navigator.clipboard.readText())

    let asked = ''
    page.once('dialog', dialog => { asked = dialog.message(); void dialog.accept() })
    await row.getByRole('button', { name: `Revoke invite for ${revokedName}` }).click()
    await expect.poll(() => asked).toBe('Are you sure you want to revoke this invite?')
    await expect(page.getByRole('row').filter({ hasText: revokedEmail })).toHaveCount(0)
    expect(errors).toEqual([])

    const context = await browser.newContext({ storageState: { cookies: [], origins: [] } })
    const stranger = await context.newPage()
    await stranger.goto(link)
    await expect(stranger.getByText('Invite link expired')).toBeVisible()
    await context.close()
  } finally {
    rails(`User.where(email: ${JSON.stringify(revokedEmail)}).each { |user| OrganizationUser.where(user: user).delete_all; user.destroy! }`)
  }
})
