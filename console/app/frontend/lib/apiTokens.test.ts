import { afterEach, describe, expect, it, vi } from 'vitest'
import { agentSessionIds, createApiToken, expiresAt, limitsText, mcpSnippets, tokenFields } from './apiTokens'

describe('tokenFields', () => {
  const form = {
    name: ' Run ', scopes: [] as string[], organizationId: '', expiry: 'never', agentSessionIds: '', projectId: '',
  }
  const now = new Date('2026-11-14T12:00:00Z')

  it('sends only the name for a token with no scope, no Organization and no expiry', () => {
    expect(tokenFields(form, now)).toEqual({ name: ' Run ' })
  })

  it('sends the scopes, the Organization and the expiry', () => {
    expect(tokenFields({ ...form, scopes: ['factory'], organizationId: '3', expiry: '30' }, now)).toEqual({
      name: ' Run ', scopes: ['factory'], organization_id: 3, expires_at: '2026-12-14T12:00:00.000Z',
    })
  })

  it('sends the Agent session ids only with a sessions scope', () => {
    const ids = 'one\ntwo, three'
    expect(tokenFields({ ...form, scopes: ['sessions:read'], agentSessionIds: ids }, now).agent_session_ids)
      .toEqual(['one', 'two', 'three'])
    expect(tokenFields({ ...form, scopes: ['mcp:read'], agentSessionIds: ids }, now).agent_session_ids).toBeUndefined()
  })

  it('sends the Project only with mcp:read and without mcp', () => {
    expect(tokenFields({ ...form, scopes: ['mcp:read'], projectId: '7' }, now).project_id).toBe(7)
    expect(tokenFields({ ...form, scopes: ['mcp:read', 'mcp'], projectId: '7' }, now).project_id).toBeUndefined()
    expect(tokenFields({ ...form, scopes: ['factory'], projectId: '7' }, now).project_id).toBeUndefined()
  })
})

describe('expiresAt', () => {
  it('is a number of days from now, or nothing for never', () => {
    const now = new Date('2026-11-14T12:00:00Z')
    expect(expiresAt('7', now)).toBe('2026-11-21T12:00:00.000Z')
    expect(expiresAt('never', now)).toBeUndefined()
  })
})

describe('agentSessionIds', () => {
  it('splits on lines, commas and spaces, and drops blanks and repeats', () => {
    expect(agentSessionIds(' a\n\nb , c a ')).toEqual(['a', 'b', 'c'])
    expect(agentSessionIds('  ')).toEqual([])
  })
})

describe('limitsText', () => {
  it('names the Agent sessions and the Project, or says none', () => {
    expect(limitsText({})).toBe('None')
    expect(limitsText({ agent_session_ids: ['one', 'two'] })).toBe('2 Agent sessions')
    expect(limitsText({ agent_session_ids: ['one'] })).toBe('1 Agent session')
    expect(limitsText({ project: { id: 1, slug: 'shop', name: 'Shop' } })).toBe('Project Shop')
  })
})

const url = 'https://console.example.com/mcp'
const secret = 'st_abc123'
const code = (id: string) => mcpSnippets(url, secret).find(s => s.id === id)!.code

describe('MCP snippets', () => {
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
    const apiToken = {
      id: 1, name: 'Laptop', created_at: '2026-10-17T10:00:00Z', last_used_at: null,
      scopes: [], organization: null, limits: {}, expires_at: null,
    }
    answer(201, { api_token: apiToken, secret })
    await expect(createApiToken({ name: 'Laptop', scopes: ['factory'] })).resolves.toEqual({ apiToken, secret })
    const [path, init] = vi.mocked(fetch).mock.calls[0]
    expect(path).toBe('/api_tokens')
    expect(JSON.parse(init!.body as string)).toEqual({ api_token: { name: 'Laptop', scopes: ['factory'] } })
  })

  it("says what's wrong with the name", async () => {
    answer(422, { errors: { name: ["can't be blank"] } })
    await expect(createApiToken({ name: '' })).rejects.toThrow("Name can't be blank")
  })

  it('says each other refusal as Console wrote it', async () => {
    answer(422, { errors: { organization: ['A token with scopes needs one Organization.'], limits: ['A Project limit needs mcp:read.'] } })
    await expect(createApiToken({ name: 'Run' })).rejects.toThrow(
      'A token with scopes needs one Organization. A Project limit needs mcp:read.',
    )
  })

  it('says when Console answers something else', async () => {
    answer(500, {})
    await expect(createApiToken({ name: 'Laptop' })).rejects.toThrow('Console answered HTTP 500')
  })
})
