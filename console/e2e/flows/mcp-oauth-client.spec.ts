import { test, expect, type Page } from '@playwright/test'
import { Client } from '@modelcontextprotocol/sdk/client/index.js'
import { StreamableHTTPClientTransport } from '@modelcontextprotocol/sdk/client/streamableHttp.js'
import { UnauthorizedError, type OAuthClientProvider } from '@modelcontextprotocol/sdk/client/auth.js'
import type { OAuthClientInformationMixed, OAuthClientMetadata, OAuthTokens } from '@modelcontextprotocol/sdk/shared/auth.js'
import { execFileSync } from 'node:child_process'
import { randomBytes } from 'node:crypto'
import { resolve } from 'node:path'
import { MCP_DOCS } from '../../app/frontend/lib/apiTokens'
import { ADMIN, ORG } from '../support/data'
import { failedLoad, watchErrors } from '../support/screens'

// Sign-in through the browser with a real MCP client library: the MCP
// TypeScript SDK's client and its OAuth code, headless, as Claude Code and
// other clients built on it do it. The flow only gives the SDK a place to keep
// what it saves, and approves on the approval page in Playwright's browser.
// The stack has a loopback Console URL (its own origin), so OAuth is on.

const ORG_NAME = 'SolidTrace Org'
const clientName = `SDK client ${Date.now()}`
// The client's loopback callback. Nothing listens there: the flow catches the
// browser's request to it, as a client's local listener would get it.
const REDIRECT_URI = 'http://127.0.0.1:47614/callback'

const rails = (code: string) => execFileSync('mise', ['exec', '--', 'bin/rails', 'runner', code], {
  cwd: resolve(__dirname, '../..'), stdio: 'pipe', timeout: 120_000,
}).toString().trim()

/** An OAuthClientProvider that keeps everything in memory and records what the SDK asks of it. */
class MemoryProvider implements OAuthClientProvider {
  info?: OAuthClientInformationMixed
  saved: OAuthTokens[] = []
  authorizationUrls: URL[] = []
  invalidated: string[] = []
  private verifier = ''
  readonly stateValue = randomBytes(16).toString('base64url')

  constructor(readonly clientMetadataUrl?: string) {}

  get redirectUrl() { return REDIRECT_URI }
  get clientMetadata(): OAuthClientMetadata {
    return {
      client_name: clientName, redirect_uris: [REDIRECT_URI], grant_types: ['authorization_code', 'refresh_token'],
      response_types: ['code'], token_endpoint_auth_method: 'none',
    }
  }
  state() { return this.stateValue }
  clientInformation() { return this.info }
  saveClientInformation(info: OAuthClientInformationMixed) { this.info = info }
  tokens() { return this.saved.at(-1) }
  saveTokens(tokens: OAuthTokens) { this.saved.push(tokens) }
  redirectToAuthorization(url: URL) { this.authorizationUrls.push(url) }
  saveCodeVerifier(verifier: string) { this.verifier = verifier }
  codeVerifier() { return this.verifier }
  invalidateCredentials(scope: 'all' | 'client' | 'tokens' | 'verifier' | 'discovery') {
    this.invalidated.push(scope)
    if (scope === 'all' || scope === 'tokens') this.saved = []
    if (scope === 'all' || scope === 'client') this.info = undefined
  }
}

type Call = { method: string; path: string; status: number; grantType?: string }

/** The calls without the metadata reads, which the SDK makes again before each sign-in or refresh. */
const pastDiscovery = (calls: Call[]) =>
  calls.filter(c => !c.path.startsWith('/.well-known/')).map(c => [c.method, c.path, c.status, c.grantType ?? null])

/** A fetch for the SDK that records each request's method, path, answer and grant type. */
function recordingFetch(calls: Call[]): typeof fetch {
  return async (input, init) => {
    const url = new URL(input instanceof Request ? input.url : String(input))
    const answer = await fetch(input, init)
    const body = init?.body
    const grantType = body instanceof URLSearchParams ? body.get('grant_type') ?? undefined : undefined
    calls.push({ method: init?.method ?? 'GET', path: url.pathname, status: answer.status, grantType })
    return answer
  }
}

function connect(mcpUrl: string, provider: MemoryProvider, calls: Call[]) {
  const client = new Client({ name: 'solidtrace-flow', version: '1.0.0' })
  const transport = new StreamableHTTPClientTransport(new URL(mcpUrl), { authProvider: provider, fetch: recordingFetch(calls) })
  return { client, transport, connected: client.connect(transport) }
}

/** Approves the SDK's last authorization URL on the approval page; answers the callback's query. */
async function approve(page: Page, provider: MemoryProvider) {
  let callback: URL | null = null
  await page.route(`${new URL(REDIRECT_URI).origin}/**`, (route) => {
    callback = new URL(route.request().url())
    return route.fulfill({ status: 200, contentType: 'text/html', body: '<p>You can close this window.</p>' })
  })
  await page.goto(provider.authorizationUrls.at(-1)!.href)
  await expect(page.getByText(clientName, { exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Approve' }).click()
  await expect(page.getByText('You can close this window.')).toBeVisible()
  await page.unroute(`${new URL(REDIRECT_URI).origin}/**`)
  return callback!.searchParams
}

const whoamiText = async (client: Client) => {
  const answer = await client.callTool({ name: 'whoami', arguments: {} })
  expect(answer.isError).toBeFalsy()
  return (answer.content as { type: string; text: string }[]).map(c => c.text).join('\n')
}

let oldConsoleUrl = ''
let origin = ''

test.beforeAll(({ baseURL }) => {
  origin = new URL(baseURL!).origin
  oldConsoleUrl = rails('print InstanceSetting.current.console_url.to_s')
  rails(`InstanceSetting.current.update!(console_url: ${JSON.stringify(origin)}); ` +
    `User.find_by!(email: ${JSON.stringify(ADMIN.email)}).oauth_grants.revoke_all!`)
})

test.afterAll(() => {
  rails(`InstanceSetting.current.update!(console_url: ${oldConsoleUrl ? JSON.stringify(oldConsoleUrl) : 'nil'}); ` +
    `Oauth::Client.where(name: ${JSON.stringify(clientName)}).destroy_all`)
})

test('the MCP SDK signs in from the 401, calls a tool, refreshes after its token expires, and is signed out by a revoke', async ({ page }) => {
  // Approve answers 409 with X-Inertia-Location, which sends the browser to the client.
  const errors = watchErrors(page, [failedLoad(409)])
  const provider = new MemoryProvider()
  const calls: Call[] = []
  const mcpUrl = `${origin}/mcp`

  // Settings → API tokens & MCP shows the sign-in snippet first, with only
  // the URL that the SDK is given here, and API tokens for scripts second.
  await page.goto(`/${ORG}/settings/api_tokens`)
  const titles = page.locator('[data-slot="card-title"]')
  await expect(titles).toHaveText(['Sign in through the browser', 'API tokens'])
  // It says where to revoke a client, and where the docs are.
  const card = page.locator('[data-slot="card"]').filter({ has: titles.filter({ hasText: 'Sign in through the browser' }) })
  await expect(card.getByRole('link', { name: 'Settings → User' })).toHaveAttribute('href', `/${ORG}/settings/user`)
  await expect(card.getByRole('link', { name: 'How to connect, and what it can do' })).toHaveAttribute('href', MCP_DOCS)
  const signIn = page.getByRole('region', { name: 'Sign-in snippets' })
  await expect(signIn.getByRole('tabpanel')).toHaveText(new RegExp(`claude mcp add --transport http solidtrace ${mcpUrl}$`))
  await expect(signIn).not.toContainText('Bearer')
  await signIn.getByRole('tab', { name: 'Cursor' }).click()
  await expect(signIn.getByRole('tabpanel')).toContainText(`"url": "${mcpUrl}"`)
  await expect(page.getByRole('note')).toHaveCount(0)

  // The first call gets the 401; the SDK finds the metadata from it,
  // registers (DCR), and asks to open the approval page.
  const first = connect(mcpUrl, provider, calls)
  await expect(first.connected).rejects.toBeInstanceOf(UnauthorizedError)
  expect(calls.slice(0, 4)).toEqual([
    { method: 'POST', path: '/mcp', status: 401, grantType: undefined },
    { method: 'GET', path: '/.well-known/oauth-protected-resource/mcp', status: 200, grantType: undefined },
    { method: 'GET', path: '/.well-known/oauth-authorization-server', status: 200, grantType: undefined },
    { method: 'POST', path: '/oauth/register', status: 201, grantType: undefined },
  ])
  expect(provider.info?.client_id).toMatch(/\S/)
  expect(provider.authorizationUrls).toHaveLength(1)
  const asked = provider.authorizationUrls[0]
  expect(`${asked.origin}${asked.pathname}`).toBe(`${origin}/oauth/authorize`)
  expect(Object.fromEntries(asked.searchParams)).toMatchObject({
    response_type: 'code', client_id: provider.info!.client_id, redirect_uri: REDIRECT_URI,
    code_challenge_method: 'S256', scope: 'mcp', resource: mcpUrl, state: provider.stateValue,
  })

  // The user approves; the callback carries the code, the state and the issuer.
  const callback = await approve(page, provider)
  expect(callback.get('state')).toBe(provider.stateValue)
  expect(callback.get('iss')).toBe(origin)
  await first.transport.finishAuth(callback.get('code')!)
  expect(calls.at(-1)).toEqual({ method: 'POST', path: '/oauth/token', status: 200, grantType: 'authorization_code' })
  const [signedIn] = provider.saved
  expect(signedIn.access_token).toMatch(/^sto_/)
  expect(signedIn.refresh_token).toMatch(/^str_/)
  expect(signedIn.expires_in).toBe(3600)

  // Connected again with the tokens: a tool call acts as the admin, in the approved Organization only.
  const second = connect(mcpUrl, provider, calls)
  await second.connected
  const tools = (await second.client.listTools()).tools.map(t => t.name)
  expect(tools).toContain('whoami')
  expect(tools).not.toContain('get_instance_settings')
  const me = await whoamiText(second.client)
  expect(me).toContain(ADMIN.email)
  expect(me).toContain(ORG)

  // The access token expires (forced here, not waited for an hour). The next
  // call gets a 401, the SDK refreshes by itself, and calls again.
  const clientId = provider.info!.client_id
  rails(`Oauth::Token.access.where(oauth_grant_id: Oauth::Grant.where(oauth_client_id: Oauth::Client.find_by!(client_id: ${JSON.stringify(clientId)}).id)).update_all(expires_at: 1.second.ago)`)
  const before = calls.length
  expect(await whoamiText(second.client)).toContain(ADMIN.email)
  expect(pastDiscovery(calls.slice(before))).toEqual([
    ['POST', '/mcp', 401, null],
    ['POST', '/oauth/token', 200, 'refresh_token'],
    ['POST', '/mcp', 200, null],
  ])
  const refreshed = provider.saved.at(-1)!
  expect(provider.saved).toHaveLength(2)
  expect(refreshed.access_token).not.toBe(signedIn.access_token)
  expect(refreshed.refresh_token).not.toBe(signedIn.refresh_token)
  // The expired token stays refused.
  const old = await fetch(mcpUrl, {
    method: 'POST',
    headers: { Authorization: `Bearer ${signedIn.access_token}`, 'Content-Type': 'application/json', Accept: 'application/json, text/event-stream' },
    body: JSON.stringify({ jsonrpc: '2.0', id: 1, method: 'tools/list' }),
  })
  expect(old.status).toBe(401)

  // Settings → User lists the sign-in.
  await page.goto(`/${ORG}/settings/user`)
  const row = page.getByTestId('signed-in-clients').getByRole('listitem', { name: `${clientName} in ${ORG_NAME}` })
  await expect(row).toBeVisible()

  // A sign-out as `claude mcp logout` does it: RFC 7009 with the refresh token.
  const revoked = await fetch(`${origin}/oauth/revoke`, {
    method: 'POST', body: new URLSearchParams({ token: refreshed.refresh_token!, client_id: clientId, token_type_hint: 'refresh_token' }),
  })
  expect(revoked.status).toBe(200)

  // The next call gets a 401; the refresh is refused, the SDK drops its
  // tokens and asks to open the approval page again.
  const afterRevoke = calls.length
  await expect(second.client.callTool({ name: 'whoami', arguments: {} })).rejects.toBeInstanceOf(UnauthorizedError)
  expect(pastDiscovery(calls.slice(afterRevoke))).toEqual([
    ['POST', '/mcp', 401, null],
    ['POST', '/oauth/token', 400, 'refresh_token'],
  ])
  expect(provider.invalidated).toEqual(['tokens'])
  expect(provider.saved).toEqual([])
  expect(provider.authorizationUrls).toHaveLength(2)

  // The Grant is gone from Settings → User.
  await page.reload()
  await expect(row).toHaveCount(0)

  await second.client.close()
  expect(errors).toEqual([])
})

test('an MCP URL on another host than the Console URL is refused by the client before it registers', async () => {
  // The same Console, reached at 127.0.0.1 while the Console URL is localhost.
  const provider = new MemoryProvider()
  const calls: Call[] = []
  const otherHost = `${origin.replace('localhost', '127.0.0.1')}/mcp`
  expect(otherHost).not.toBe(`${origin}/mcp`)

  const { connected } = connect(otherHost, provider, calls)
  await expect(connected).rejects.toThrow(`Protected resource ${origin}/mcp does not match expected ${otherHost}`)
  expect(calls.map(c => c.path)).not.toContain('/oauth/register')
  expect(provider.info).toBeUndefined()
  expect(provider.authorizationUrls).toEqual([])
})

test('a client whose published description Console cannot fetch gets the error page, and never its callback', async ({ page }) => {
  // The SDK uses the description's URL as its client_id, as Console's
  // metadata offers (CIMD), and does not register. .invalid never resolves.
  const errors = watchErrors(page, [failedLoad(400)])
  const provider = new MemoryProvider('https://client.invalid/oauth/client.json')
  const calls: Call[] = []

  const { connected } = connect(`${origin}/mcp`, provider, calls)
  await expect(connected).rejects.toBeInstanceOf(UnauthorizedError)
  expect(calls.map(c => c.path)).not.toContain('/oauth/register')
  expect(provider.authorizationUrls[0].searchParams.get('client_id')).toBe('https://client.invalid/oauth/client.json')

  let called = false
  await page.route(`${new URL(REDIRECT_URI).origin}/**`, (route) => { called = true; return route.abort() })
  const shown = await page.goto(provider.authorizationUrls[0].href)
  await expect(page.getByText('Console could not find the address of client.invalid')).toBeVisible()
  await expect(page.getByRole('button', { name: 'Approve' })).toHaveCount(0)
  expect(shown?.url()).toContain('/oauth/authorize')
  expect(called).toBe(false)
  expect(errors).toEqual([])
})

test('with OAuth off, the client finds no metadata and signs in nowhere', async ({ page }) => {
  rails('InstanceSetting.current.update!(console_url: nil)')
  try {
    // The page says why, in place of the sign-in snippet.
    await page.goto(`/${ORG}/settings/api_tokens`)
    await expect(page.getByRole('note')).toContainText('Sign-in through the browser needs an HTTPS Console URL')
    await expect(page.getByRole('region', { name: 'Sign-in snippets' })).toHaveCount(0)

    const provider = new MemoryProvider()
    const calls: Call[] = []
    const { connected } = connect(`${origin}/mcp`, provider, calls)
    await expect(connected).rejects.toThrow()
    expect(calls[0]).toEqual({ method: 'POST', path: '/mcp', status: 401, grantType: undefined })
    expect(calls.find(c => c.path === '/.well-known/oauth-protected-resource/mcp')?.status).toBe(404)
    expect(calls.find(c => c.path === '/.well-known/oauth-authorization-server')?.status).toBe(404)
    expect(calls.filter(c => c.path.startsWith('/oauth/'))).toEqual([])
    expect(provider.saved).toEqual([])
  } finally {
    rails(`InstanceSetting.current.update!(console_url: ${JSON.stringify(origin)})`)
  }
})
