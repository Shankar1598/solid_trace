import { Link, usePage } from '@inertiajs/react'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { MCP_DOCS, signInSnippets } from '@/lib/apiTokens'
import { SnippetTabs } from './SnippetTabs'

type McpSignInProps = {
  /** Where a client adds the MCP server: with OAuth on, the resource at the Console URL's host. */
  mcp_url: string
  /** Why MCP clients can't sign in through the browser on this install; null when they can. */
  browser_sign_in_note: string | null
  current_org?: { slug: string }
}

/**
 * Settings → API tokens & MCP, first: add the MCP server with only its URL,
 * and sign in through the browser. Where OAuth is off, it says why instead.
 */
export function McpSignIn() {
  const { mcp_url: mcpUrl, browser_sign_in_note: note, current_org: org } = usePage<McpSignInProps>().props
  return (
    <Card aria-labelledby="mcp-sign-in-title">
      <CardHeader>
        <CardTitle id="mcp-sign-in-title">Sign in through the browser</CardTitle>
        <CardDescription>
          Add the MCP server with only its URL. Claude Code, Cursor and other MCP clients then open Console in the browser,
          and you approve them for one Organization. No token is copied. You can revoke each one in{' '}
          {org ? <Link href={`/${org.slug}/settings/user`} className="underline underline-offset-2">Settings → User</Link> : 'Settings → User'}.{' '}
          <a href={MCP_DOCS} target="_blank" rel="noreferrer" className="underline underline-offset-2">How to connect, and what it can do</a>
        </CardDescription>
      </CardHeader>
      <CardContent className="pb-6">
        {note
          ? <p role="note" className="border p-3 text-sm text-muted-foreground">{note}</p>
          : <section aria-label="Sign-in snippets" className="text-sm"><SnippetTabs snippets={signInSnippets(mcpUrl)} /></section>}
      </CardContent>
    </Card>
  )
}
