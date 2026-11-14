// A user's API tokens, which let Claude Code, Cursor or the factory act as
// them: making one with its scopes, Organization, limits and expiry, and the
// snippets that add the MCP server with it.

export interface NamedRecord {
  id: number
  slug: string
  name: string
}

/** What a token is limited to: Agent session ids for sessions:*, one Project for mcp:read. */
export interface ApiTokenLimits {
  agent_session_ids?: string[]
  project?: NamedRecord
}

/** An API token as Settings → API tokens & MCP lists it. Its secret is answered once, when it is made. */
export interface ApiToken {
  id: number
  name: string
  created_at: string
  last_used_at: string | null
  /** [] is no scope: the token acts as you over the MCP server. */
  scopes: string[]
  /** null acts in each of your Organizations. */
  organization: NamedRecord | null
  limits: ApiTokenLimits
  /** null does not expire. */
  expires_at: string | null
}

/** A scope that you can put on your own token, with the text that says what it gives. */
export interface OfferedScope {
  name: string
  /** Has %{organization} for the Organization's name. */
  text: string
}

/** The form's values, as its fields hold them. */
export interface TokenForm {
  name: string
  scopes: string[]
  /** '' is each Organization. */
  organizationId: string
  /** A number of days, or 'never'. */
  expiry: string
  /** Agent session ids, one a line or split by commas. */
  agentSessionIds: string
  /** '' is no Project limit. */
  projectId: string
}

/** What POST /api_tokens takes. */
export interface TokenFields {
  name: string
  scopes?: string[]
  organization_id?: number
  expires_at?: string
  agent_session_ids?: string[]
  project_id?: number
}

export const EXPIRY_CHOICES: Record<string, string> = {
  '7': '7 days', '30': '30 days', '90': '90 days', '365': '1 year', never: 'No expiry',
}
export const DEFAULT_EXPIRY = '90'

const SESSION_SCOPES = ['sessions:write', 'sessions:read']

/** The Agent session ids in text, split on lines, commas and spaces, without repeats. */
export function agentSessionIds(text: string): string[] {
  return [...new Set(text.split(/[\s,]+/).filter(id => id !== ''))]
}

/** The expiry days from now, as ISO 8601; undefined for 'never'. */
export function expiresAt(expiry: string, now: Date = new Date()): string | undefined {
  const days = Number(expiry)
  if (expiry === 'never' || !Number.isFinite(days)) return undefined
  return new Date(now.getTime() + days * 86_400_000).toISOString()
}

/**
 * The fields to send for form. A limit goes only with the scope it is for,
 * so a limit left from an unticked scope is not sent.
 */
export function tokenFields(form: TokenForm, now: Date = new Date()): TokenFields {
  const fields: TokenFields = { name: form.name }
  if (form.scopes.length > 0) fields.scopes = form.scopes
  if (form.organizationId !== '') fields.organization_id = Number(form.organizationId)
  const expires = expiresAt(form.expiry, now)
  if (expires) fields.expires_at = expires
  const ids = agentSessionIds(form.agentSessionIds)
  if (ids.length > 0 && form.scopes.some(scope => SESSION_SCOPES.includes(scope))) fields.agent_session_ids = ids
  if (form.projectId !== '' && form.scopes.includes('mcp:read') && !form.scopes.includes('mcp')) {
    fields.project_id = Number(form.projectId)
  }
  return fields
}

/** A token's limits in a few words. */
export function limitsText(limits: ApiTokenLimits): string {
  const parts: string[] = []
  const ids = limits.agent_session_ids ?? []
  if (ids.length > 0) parts.push(`${ids.length} Agent session${ids.length === 1 ? '' : 's'}`)
  if (limits.project) parts.push(`Project ${limits.project.name}`)
  return parts.length > 0 ? parts.join(', ') : 'None'
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

/** Makes a token with fields. The secret in the answer is shown once and never again. */
export async function createApiToken(fields: TokenFields): Promise<{ apiToken: ApiToken; secret: string }> {
  let response: Response
  try {
    response = await fetch('/api_tokens', {
      method: 'POST',
      credentials: 'same-origin',
      headers: { Accept: 'application/json', 'Content-Type': 'application/json', 'X-CSRF-Token': csrfToken() },
      body: JSON.stringify({ api_token: fields }),
    })
  } catch {
    throw new Error('Console could not be reached. Check your connection and try again.')
  }
  const answer = await response.json().catch(() => null)
  if (response.ok && typeof answer?.secret === 'string') return { apiToken: answer.api_token as ApiToken, secret: answer.secret }
  const nameErrors: unknown = answer?.errors?.name
  if (Array.isArray(nameErrors) && nameErrors.length > 0) throw new Error(`Name ${nameErrors.join(', ')}`)
  // Each other field's refusals are whole sentences.
  const others = Object.values((answer?.errors ?? {}) as Record<string, unknown>)
    .flatMap(messages => (Array.isArray(messages) ? messages : []))
    .filter((message): message is string => typeof message === 'string')
  if (others.length > 0) throw new Error(others.join(' '))
  throw new Error(`Console answered HTTP ${response.status}. Try again.`)
}

/** The page's CSRF token, for the X-CSRF-Token header of a fetch to Console. */
export function csrfToken(): string {
  if (typeof document === 'undefined') return ''
  return document.querySelector<HTMLMetaElement>('meta[name="csrf-token"]')?.content ?? ''
}
