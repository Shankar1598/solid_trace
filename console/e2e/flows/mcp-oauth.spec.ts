import { test, expect, request as apiRequest, type APIRequestContext } from '@playwright/test'
import { execFileSync } from 'node:child_process'
import { createHash, randomBytes } from 'node:crypto'
import { resolve } from 'node:path'
import { ADMIN, ORG } from '../support/data'
import { failedLoad, watchErrors } from '../support/screens'

// Sign-in through the browser, as an MCP client does it: a script
// registers a client (DCR) and opens the approval page; the signed-in admin
// approves; the script reads the redirect, exchanges the code with PKCE and
// calls POST /mcp. Then the admin revokes the client on Settings → User and
// POST /mcp refuses its token. OAuth needs a Console URL: the flow sets a
// loopback one, this stack's own, and puts the old one back after.

const ORG_NAME = 'SolidTrace Org'
const clientName = `Flow client ${Date.now()}`
// The client's loopback callback. Nothing listens there: the flow catches
// the browser's request to it.
const REDIRECT_URI = 'http://127.0.0.1:47613/callback'

const rails = (code: string) => execFileSync('mise', ['exec', '--', 'bin/rails', 'runner', code], {
  cwd: resolve(__dirname, '../..'), stdio: 'pipe', timeout: 120_000,
}).toString().trim()

let oldConsoleUrl = ''

test.beforeAll(({ baseURL }) => {
  oldConsoleUrl = rails('print InstanceSetting.current.console_url.to_s')
  // The list starts empty: the admin's Signed-in clients from other runs are revoked.
  rails(`InstanceSetting.current.update!(console_url: ${JSON.stringify(new URL(baseURL!).origin)}); ` +
    `User.find_by!(email: ${JSON.stringify(ADMIN.email)}).oauth_grants.revoke_all!`)
})

test.afterAll(() => {
  rails(`InstanceSetting.current.update!(console_url: ${oldConsoleUrl ? JSON.stringify(oldConsoleUrl) : "nil"}); ` +
    `Oauth::Client.where(name: ${JSON.stringify(clientName)}).destroy_all`)
})

const base64url = (bytes: Buffer) => bytes.toString('base64url')

async function mcpWhoami(client: APIRequestContext, token: string) {
  return client.post('/mcp', {
    headers: { Authorization: `Bearer ${token}`, Accept: 'application/json, text/event-stream' },
    data: { jsonrpc: '2.0', id: 1, method: 'tools/call', params: { name: 'whoami', arguments: {} } },
  })
}

test('an MCP client signs in through the browser, calls POST /mcp, and is revoked on Settings → User', async ({ page, baseURL }) => {
  // Approve answers 409 with X-Inertia-Location, which sends the browser to the client.
  const errors = watchErrors(page, [failedLoad(409)])
  // The script: no cookies, as an MCP client has none.
  const client = await apiRequest.newContext({ baseURL })

  // Settings → User says where clients show before there are any. Opening
  // it under the Organization also makes that the last used one, which the
  // approval page picks first.
  await page.goto(`/${ORG}/settings/user`)
  const card = page.getByTestId('signed-in-clients')
  await expect(card).toContainText('Clients that you sign in through the browser show here.')
  await expect(card.getByRole('link', { name: 'Settings → API tokens & MCP' })).toHaveAttribute('href', `/${ORG}/settings/api_tokens`)

  // Registration (DCR).
  const registered = await client.post('/oauth/register', { data: { client_name: clientName, redirect_uris: [REDIRECT_URI] } })
  expect(registered.status()).toBe(201)
  const { client_id: clientId } = await registered.json()

  // The approval page, from the authorize URL that the client opens.
  const verifier = base64url(randomBytes(32))
  const challenge = base64url(createHash('sha256').update(verifier).digest())
  const state = base64url(randomBytes(16))
  const resource = `${new URL(baseURL!).origin}/mcp`
  const authorize = new URLSearchParams({
    response_type: 'code', client_id: clientId, redirect_uri: REDIRECT_URI, code_challenge: challenge,
    code_challenge_method: 'S256', scope: 'mcp', resource, state,
  })
  let redirected: URL | null = null
  await page.route(`${new URL(REDIRECT_URI).origin}/**`, (route) => {
    redirected = new URL(route.request().url())
    return route.fulfill({ status: 200, contentType: 'text/html', body: '<p>You can close this window.</p>' })
  })
  await page.goto(`/oauth/authorize?${authorize}`)
  await expect(page.getByText(clientName, { exact: true })).toBeVisible()
  await expect(page.getByText('Not checked: the client gave this name')).toBeVisible()
  await expect(page.getByRole('alert')).toContainText('This program runs on a computer.')
  await expect(page.getByText(`in ${ORG_NAME}:`)).toBeVisible()
  await page.getByRole('button', { name: 'Approve' }).click()

  // The redirect carries the code, the state and the issuer.
  await expect(page.getByText('You can close this window.')).toBeVisible()
  expect(redirected).not.toBeNull()
  const answer = redirected!.searchParams
  expect(answer.get('state')).toBe(state)
  expect(answer.get('iss')).toBe(new URL(baseURL!).origin)
  const code = answer.get('code')!
  expect(code).toMatch(/^[A-Za-z0-9_-]{43}$/)

  // The code exchange with PKCE, then an MCP call as the admin.
  const exchanged = await client.post('/oauth/token', {
    form: { grant_type: 'authorization_code', code, code_verifier: verifier, client_id: clientId, redirect_uri: REDIRECT_URI, resource },
  })
  expect(exchanged.status()).toBe(200)
  const tokens = await exchanged.json()
  expect(tokens.access_token).toMatch(/^sto_/)
  expect(tokens.refresh_token).toMatch(/^str_/)
  const called = await mcpWhoami(client, tokens.access_token)
  expect(called.status()).toBe(200)
  expect(await called.text()).toContain(ADMIN.email)

  // Settings → User lists the client, and Revoke asks first.
  await page.goto(`/${ORG}/settings/user`)
  const row = card.getByRole('listitem', { name: `${clientName} in ${ORG_NAME}` })
  await expect(row).toContainText('Not checked')
  await expect(row).toContainText('127.0.0.1')
  await expect(row).toContainText(new RegExp(`${ORG_NAME} · Browser · Chrome on \\w+`))
  await expect(row).toContainText('Last used less than a minute ago')
  await row.getByRole('button', { name: `Revoke ${clientName} in ${ORG_NAME}` }).click()
  const confirm = page.getByRole('alertdialog')
  await expect(confirm).toContainText(`This client can no longer act as you in ${ORG_NAME}.`)
  await confirm.getByRole('button', { name: 'Cancel' }).click()
  await expect(row).toBeVisible()
  expect((await mcpWhoami(client, tokens.access_token)).status()).toBe(200)

  await row.getByRole('button', { name: `Revoke ${clientName} in ${ORG_NAME}` }).click()
  await confirm.getByRole('button', { name: 'Revoke client' }).click()
  await expect(page.getByText('Signed-in client revoked')).toBeVisible()
  await expect(row).toHaveCount(0)
  await expect(card).toContainText('Clients that you sign in through the browser show here.')
  await expect(page).toHaveURL(new RegExp(`/${ORG}/settings/user$`))

  // The token stops at once, and so does the refresh token.
  const refused = await mcpWhoami(client, tokens.access_token)
  expect(refused.status()).toBe(401)
  expect(refused.headers()['www-authenticate']).toContain('error="invalid_token"')
  const refreshed = await client.post('/oauth/token', {
    form: { grant_type: 'refresh_token', refresh_token: tokens.refresh_token, client_id: clientId },
  })
  expect(refreshed.status()).toBe(400)
  expect((await refreshed.json()).error).toBe('invalid_grant')

  await client.dispose()
  expect(errors).toEqual([])
})
