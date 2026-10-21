// A user's API tokens, which let Claude Code or Cursor act as them through
// the MCP server: making one, and the snippets that add the MCP server with it.

/** An API token as User settings list it. Its secret is answered once, when it is made. */
export interface ApiToken {
  id: number
  name: string
  created_at: string
  last_used_at: string | null
}

export interface McpSnippet {
  id: 'claude-code' | 'cursor'
  label: string
  /** Where the snippet goes. */
  note: string
  code: string
}

/** The snippets that add the MCP server at mcpUrl, signed in with secret. */
export function mcpSnippets(mcpUrl: string, secret: string): McpSnippet[] {
  const cursor = { mcpServers: { solidtrace: { url: mcpUrl, headers: { Authorization: `Bearer ${secret}` } } } }
  return [
    {
      id: 'claude-code',
      label: 'Claude Code',
      note: 'Run in a terminal.',
      code: `claude mcp add --transport http solidtrace ${mcpUrl} --header "Authorization: Bearer ${secret}"`,
    },
    {
      id: 'cursor',
      label: 'Cursor',
      note: 'Add to ~/.cursor/mcp.json, or to .cursor/mcp.json in a project.',
      code: JSON.stringify(cursor, null, 2),
    },
  ]
}

/** Makes a token named name. The secret in the answer is shown once and never again. */
export async function createApiToken(name: string): Promise<{ apiToken: ApiToken; secret: string }> {
  let response: Response
  try {
    response = await fetch('/api_tokens', {
      method: 'POST',
      credentials: 'same-origin',
      headers: { Accept: 'application/json', 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken() },
      body: JSON.stringify({ api_token: { name } }),
    })
  } catch {
    throw new Error('Console could not be reached. Check your connection and try again.')
  }
  const answer = await response.json().catch(() => null)
  if (response.ok && typeof answer?.secret === 'string') return { apiToken: answer.api_token as ApiToken, secret: answer.secret }
  const nameErrors: unknown = answer?.errors?.name
  if (Array.isArray(nameErrors) && nameErrors.length > 0) throw new Error(`Name ${nameErrors.join(', ')}`)
  throw new Error(`Console answered HTTP ${response.status}. Try again.`)
}

/** The page's CSRF token, for the X-CSRF-Token header of a fetch to Console. */
export function csrfToken(): string {
  if (typeof document === 'undefined') return ''
  return document.querySelector<HTMLMetaElement>('meta[name="csrf-token"]')?.content ?? ''
}
