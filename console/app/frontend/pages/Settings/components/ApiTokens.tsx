import { router, usePage } from '@inertiajs/react'
import { formatDistanceToNow } from 'date-fns'
import { Copy } from 'lucide-react'
import { useState } from 'react'
import { copyText } from '@/components/copyText'
import {
  AlertDialog,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { createApiToken, mcpSnippets, type ApiToken } from '@/lib/apiTokens'

// docs/mcp.md, where the README's repository keeps it.
const MCP_DOCS = 'https://github.com/solidtrace/solid_trace/blob/main/docs/mcp.md'

type ApiTokensProps = {
  api_tokens: ApiToken[]
  /** Where a client adds the MCP server, as the page's request reached Console. */
  mcp_url: string
}

/**
 * User settings' API tokens: make one by name and see its secret once, with
 * the snippets that add the MCP server to Claude Code or Cursor; list them;
 * revoke one.
 */
export function ApiTokens() {
  const { api_tokens: tokens, mcp_url: mcpUrl } = usePage<ApiTokensProps>().props
  const [name, setName] = useState('')
  const [making, setMaking] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [made, setMade] = useState<{ name: string; secret: string } | null>(null)
  const [revoking, setRevoking] = useState<ApiToken | null>(null)

  const make: React.FormEventHandler = async (e) => {
    e.preventDefault()
    setMaking(true)
    setError(null)
    try {
      const { apiToken, secret } = await createApiToken(name)
      setMade({ name: apiToken.name, secret })
      setName('')
      router.reload({ only: ['api_tokens'] })
    } catch (failure) {
      setError((failure as Error).message)
    } finally {
      setMaking(false)
    }
  }

  const revoke = (token: ApiToken) => {
    router.delete(`/api_tokens/${token.id}`, { preserveScroll: true, onFinish: () => setRevoking(null) })
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>API tokens</CardTitle>
        <CardDescription>
          An API token lets Claude Code, Cursor or another MCP client act as you through SolidTrace's MCP server, with your role in each Organization.{' '}
          <a href={MCP_DOCS} target="_blank" rel="noreferrer" className="underline underline-offset-2">How to connect, and what it can do</a>
        </CardDescription>
      </CardHeader>
      <CardContent className="space-y-6 pb-6">
        <form onSubmit={make} className="flex flex-wrap items-end gap-3">
          <div className="space-y-2">
            <Label htmlFor="api-token-name">Token name</Label>
            <Input
              id="api-token-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="Laptop, Claude Code"
              className="w-64"
            />
          </div>
          <Button type="submit" disabled={making || name.trim() === ''}>
            {making ? 'Making…' : 'Make token'}
          </Button>
          {error && <p role="alert" className="w-full text-sm text-destructive">{error}</p>}
        </form>

        {made && <MadeToken name={made.name} secret={made.secret} mcpUrl={mcpUrl} onDone={() => setMade(null)} />}

        {tokens.length === 0 ? (
          <p className="text-sm text-muted-foreground">No API tokens yet.</p>
        ) : (
          <Table aria-label="API tokens">
            <TableHeader>
              <TableRow>
                <TableHead>Name</TableHead>
                <TableHead>Created</TableHead>
                <TableHead>Last used</TableHead>
                <TableHead className="w-0" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {tokens.map(token => (
                <TableRow key={token.id}>
                  <TableCell>{token.name}</TableCell>
                  <TableCell className="text-muted-foreground">{ago(token.created_at)}</TableCell>
                  <TableCell className="text-muted-foreground">{token.last_used_at ? ago(token.last_used_at) : 'Never'}</TableCell>
                  <TableCell className="text-right">
                    <Button variant="outline" size="sm" onClick={() => setRevoking(token)} aria-label={`Revoke ${token.name}`}>
                      Revoke
                    </Button>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </CardContent>

      <AlertDialog open={revoking !== null} onOpenChange={(open) => { if (!open) setRevoking(null) }}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Revoke "{revoking?.name}"?</AlertDialogTitle>
            <AlertDialogDescription>
              Clients that use this token can no longer reach SolidTrace's MCP server. This can't be undone.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <Button variant="destructive" onClick={() => revoking && revoke(revoking)}>Revoke token</Button>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </Card>
  )
}

/** A token just made: its secret, shown this once, and the snippets that use it. */
function MadeToken({ name, secret, mcpUrl, onDone }: { name: string; secret: string; mcpUrl: string; onDone: () => void }) {
  const shown = mcpSnippets(mcpUrl, secret)
  return (
    <section aria-label="New API token" className="flex flex-col gap-3 border p-4 text-sm">
      <div className="flex flex-col gap-1">
        <h3 className="font-medium">"{name}" is made</h3>
        <p className="text-muted-foreground">Copy it now: SolidTrace keeps only its digest, so it won't be shown again.</p>
      </div>
      <div className="flex items-center gap-2">
        <code className="min-w-0 flex-1 overflow-x-auto border bg-muted/40 px-3 py-2 font-mono text-xs">{secret}</code>
        <Button variant="outline" size="sm" onClick={() => copyText(secret, 'API token')} aria-label="Copy the API token">
          <Copy className="h-3.5 w-3.5" /> Copy
        </Button>
      </div>

      <Tabs defaultValue={shown[0].id} className="gap-0">
        <TabsList variant="line">
          {shown.map(s => <TabsTrigger key={s.id} value={s.id}>{s.label}</TabsTrigger>)}
        </TabsList>
        {shown.map(s => (
          <TabsContent key={s.id} value={s.id} className="flex flex-col gap-2 border-t pt-3">
            <div className="flex items-center justify-between gap-2">
              <span className="text-xs text-muted-foreground">{s.note}</span>
              <Button variant="outline" size="sm" onClick={() => copyText(s.code, 'Snippet')} aria-label={`Copy the ${s.label} snippet`}>
                <Copy className="h-3.5 w-3.5" /> Copy
              </Button>
            </div>
            <pre className="overflow-x-auto border bg-muted/40 p-3 font-mono text-xs leading-5">{s.code}</pre>
          </TabsContent>
        ))}
      </Tabs>

      <div>
        <Button variant="outline" size="sm" onClick={onDone}>Done</Button>
      </div>
    </section>
  )
}

function ago(iso: string): string {
  return `${formatDistanceToNow(new Date(iso))} ago`
}
