import { afterEach, describe, expect, it, vi } from 'vitest'
import { createApiToken, mcpSnippets, signInSnippets } from './apiTokens'

const url = 'https://console.example.com/mcp'
const secret = 'st_abc123'
const code = (id: string) => mcpSnippets(url, secret).find(s => s.id === id)!.code

describe('sign-in snippets', () => {
  const signIn = (id: string) => signInSnippets(url).find(s => s.id === id)!

  it('offers Claude Code and Cursor, in that order', () => {
    expect(signInSnippets(url).map(s => s.label)).toEqual(['Claude Code', 'Cursor'])
  })

  it('adds the MCP server to Claude Code with only its URL, and says how to sign in', () => {
    expect(signIn('claude-code').code).toBe('claude mcp add --transport http solidtrace https://console.example.com/mcp')
    expect(signIn('claude-code').note).toBe('Run in a terminal. Then sign in through the browser: /mcp in Claude Code, or claude mcp login solidtrace.')
  })

  it("gives Cursor's mcp.json only the URL", () => {
    expect(JSON.parse(signIn('cursor').code)).toEqual({ mcpServers: { solidtrace: { url } } })
  })

  it('holds no token', () => {
    for (const s of signInSnippets(url)) expect(s.code).not.toMatch(/Bearer|st_/)
  })
})

describe('API token snippets', () => {
  it('offers Claude Code and Cursor, in that order', () => {
    expect(mcpSnippets(url, secret).map(s => s.label)).toEqual(['Claude Code', 'Cursor'])
  })

  it('adds the MCP server to Claude Code over HTTP with the token as a Bearer header', () => {
    expect(code('claude-code')).toBe(
      'claude mcp add --transport http solidtrace https://console.example.com/mcp --header "Authorization: Bearer st_abc123"',
    )
  })

  it("gives Cursor's mcp.json the URL and the header", () => {
    expect(JSON.parse(code('cursor'))).toEqual({
      mcpServers: { solidtrace: { url, headers: { Authorization: 'Bearer st_abc123' } } },
    })
  })
})

describe('createApiToken', () => {
  afterEach(() => vi.unstubAllGlobals())

  const answer = (status: number, body: unknown) =>
    vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify(body), { status })))

  it('answers the token and its secret', async () => {
    const apiToken = { id: 1, name: 'Laptop', created_at: '2026-10-17T10:00:00Z', last_used_at: null }
    answer(201, { api_token: apiToken, secret })
    await expect(createApiToken('Laptop')).resolves.toEqual({ apiToken, secret })
    const [path, init] = vi.mocked(fetch).mock.calls[0]
    expect(path).toBe('/api_tokens')
    expect(JSON.parse(init!.body as string)).toEqual({ api_token: { name: 'Laptop' } })
  })

  it("says what's wrong with the name", async () => {
    answer(422, { errors: { name: ["can't be blank"] } })
    await expect(createApiToken('')).rejects.toThrow("Name can't be blank")
  })

  it('says when Console answers something else', async () => {
    answer(500, {})
    await expect(createApiToken('Laptop')).rejects.toThrow('Console answered HTTP 500')
  })
})
