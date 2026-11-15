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
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Textarea } from '@/components/ui/textarea'
import {
  createApiToken,
  DEFAULT_EXPIRY,
  dispatcherFields,
  EXPIRY_CHOICES,
  mcpSnippets,
  tokenFields,
  tokenLimitsText,
  type ApiToken,
  type DispatcherChoice,
  type NamedRecord,
  type OfferedScope,
  type TokenForm,
} from '@/lib/apiTokens'

// docs/mcp.md, where the README's repository keeps it.
const MCP_DOCS = 'https://github.com/solidtrace/solid_trace/blob/main/docs/mcp.md'

type ApiTokensProps = {
  api_tokens: ApiToken[]
  /** Where a client adds the MCP server, as the page's request reached Console. */
  mcp_url: string
  /** Why MCP clients can't sign in through the browser on this install; null when they can. */
  browser_sign_in_note: string | null
  /** The scopes you can put on your own token. */
  scopes: OfferedScope[]
  organizations: NamedRecord[]
  /** Each Organization's Projects, for the mcp:read Project limit. */
  projects: (NamedRecord & { organization_id: number })[]
  dispatcher: DispatcherChoice
}

const EMPTY_FORM: TokenForm = {
  name: '', scopes: [], organizationId: '', expiry: DEFAULT_EXPIRY, agentSessionIds: '', projectId: '',
}
// Select values for "no choice", as an item value can't be ''.
const EACH = 'each'
const NONE = 'none'

/**
 * User settings' API tokens: make one with a name, scopes, an Organization,
 * limits and an expiry, and see its secret once, with the snippets that add
 * the MCP server to Claude Code or Cursor; list them; revoke one.
 */
export function ApiTokens() {
  const {
    api_tokens: tokens, mcp_url: mcpUrl, browser_sign_in_note: browserSignInNote, scopes: offered, organizations, projects,
    dispatcher,
  } = usePage<ApiTokensProps>().props
  const [form, setForm] = useState<TokenForm>(EMPTY_FORM)
  const [making, setMaking] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [made, setMade] = useState<{ name: string; secret: string; dispatcher: boolean } | null>(null)
  const [revoking, setRevoking] = useState<ApiToken | null>(null)

  const set = (changes: Partial<TokenForm>) => setForm(current => ({ ...current, ...changes }))
  const organization = organizations.find(o => String(o.id) === form.organizationId)
  const hasSessionScope = form.scopes.some(scope => scope.startsWith('sessions:'))
  const takesProject = form.scopes.includes('mcp:read') && !form.scopes.includes('mcp')

  // A token with scopes needs one Organization: the first tick picks the first one.
  const tick = (scope: string, on: boolean) => {
    const scopes = on ? [...form.scopes, scope] : form.scopes.filter(s => s !== scope)
    const organizationId = on && form.organizationId === '' && organizations[0] ? String(organizations[0].id) : form.organizationId
    set({ scopes: offered.map(s => s.name).filter(name => scopes.includes(name)), organizationId })
  }

  const make: React.FormEventHandler = async (e) => {
    e.preventDefault()
    setMaking(true)
    setError(null)
    try {
      const { apiToken, secret } = await createApiToken(tokenFields(form))
      setMade({ name: apiToken.name, secret, dispatcher: false })
      setForm(EMPTY_FORM)
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
        {browserSignInNote && <p role="note" className="border p-3 text-sm text-muted-foreground">{browserSignInNote}</p>}

        <form onSubmit={make} className="flex flex-col gap-4">
          <div className="flex flex-wrap items-end gap-3">
            <div className="space-y-2">
              <Label htmlFor="api-token-name">Token name</Label>
              <Input
                id="api-token-name"
                value={form.name}
                onChange={(e) => set({ name: e.target.value })}
                placeholder="Laptop, Claude Code"
                className="w-64"
              />
            </div>
            <div className="space-y-2">
              <Label>Organization</Label>
              <Select
                value={form.organizationId || EACH}
                onValueChange={(value) => set({ organizationId: value === EACH || !value ? '' : String(value), projectId: '' })}
                items={{ [EACH]: 'Each of yours', ...Object.fromEntries(organizations.map(o => [String(o.id), o.name])) }}
              >
                <SelectTrigger className="w-48" aria-label="Organization"><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectItem value={EACH}>Each of yours</SelectItem>
                  {organizations.map(o => <SelectItem key={o.id} value={String(o.id)}>{o.name}</SelectItem>)}
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-2">
              <Label>Expiry</Label>
              <Select value={form.expiry} onValueChange={(value) => set({ expiry: String(value ?? DEFAULT_EXPIRY) })} items={EXPIRY_CHOICES}>
                <SelectTrigger className="w-36" aria-label="Expiry"><SelectValue /></SelectTrigger>
                <SelectContent>
                  {Object.entries(EXPIRY_CHOICES).map(([value, label]) => <SelectItem key={value} value={value}>{label}</SelectItem>)}
                </SelectContent>
              </Select>
            </div>
          </div>

          <fieldset className="flex flex-col gap-2">
            <legend className="mb-1 text-sm font-medium">Scopes</legend>
            <p className="text-xs text-muted-foreground">
              With no scope, the token acts as you over the MCP server. With scopes, it can do only what they give, in one Organization.
            </p>
            {offered.map(scope => (
              <div key={scope.name} className="flex items-start gap-2">
                <Checkbox
                  id={`scope-${scope.name}`}
                  checked={form.scopes.includes(scope.name)}
                  onCheckedChange={(on) => tick(scope.name, !!on)}
                />
                <Label htmlFor={`scope-${scope.name}`} className="text-sm font-normal leading-snug">
                  <span className="font-mono">{scope.name}</span>
                  <span className="text-muted-foreground"> — {scope.text.replace('%{organization}', organization?.name ?? 'the Organization')}</span>
                </Label>
              </div>
            ))}
          </fieldset>

          {hasSessionScope && (
            <div className="space-y-2">
              <Label htmlFor="api-token-agent-sessions">Agent session ids</Label>
              <Textarea
                id="api-token-agent-sessions"
                value={form.agentSessionIds}
                onChange={(e) => set({ agentSessionIds: e.target.value })}
                placeholder="One a line"
                className="max-w-md font-mono"
              />
              <p className="text-xs text-muted-foreground">
                sessions:read needs this list. sessions:write without a list uploads any Agent session.
              </p>
            </div>
          )}

          {takesProject && (
            <div className="space-y-2">
              <Label>Project limit for mcp:read</Label>
              <Select
                value={form.projectId || NONE}
                onValueChange={(value) => set({ projectId: value === NONE || !value ? '' : String(value) })}
                items={{ [NONE]: 'Every Project', ...Object.fromEntries(projects.map(p => [String(p.id), p.name])) }}
              >
                <SelectTrigger className="w-48" aria-label="Project limit"><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectItem value={NONE}>Every Project</SelectItem>
                  {projects.filter(p => String(p.organization_id) === form.organizationId)
                    .map(p => <SelectItem key={p.id} value={String(p.id)}>{p.name}</SelectItem>)}
                </SelectContent>
              </Select>
            </div>
          )}

          <div>
            <Button type="submit" disabled={making || form.name.trim() === ''}>
              {making ? 'Making…' : 'Make token'}
            </Button>
          </div>
          {error && <p role="alert" className="text-sm text-destructive">{error}</p>}
        </form>

        {dispatcher.organizations.length > 0 && (
          <DispatcherToken choice={dispatcher} onMade={(apiToken, secret) => setMade({ name: apiToken.name, secret, dispatcher: true })} />
        )}

        {made && (
          <MadeToken name={made.name} secret={made.secret} mcpUrl={mcpUrl} dispatcher={made.dispatcher} onDone={() => setMade(null)} />
        )}

        {tokens.length === 0 ? (
          <p className="text-sm text-muted-foreground">No API tokens yet.</p>
        ) : (
          <Table aria-label="API tokens">
            <TableHeader>
              <TableRow>
                <TableHead>Name</TableHead>
                <TableHead>Scopes</TableHead>
                <TableHead>Organization</TableHead>
                <TableHead>Limits</TableHead>
                <TableHead>Expires</TableHead>
                <TableHead>Created</TableHead>
                <TableHead>Last used</TableHead>
                <TableHead className="w-0" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {tokens.map(token => (
                <TableRow key={token.id}>
                  <TableCell>{token.name}</TableCell>
                  <TableCell className="font-mono text-xs">
                    {token.scopes.length > 0 ? token.scopes.join(' ') : <span className="font-sans text-muted-foreground">None (MCP as you)</span>}
                  </TableCell>
                  <TableCell>{token.organization?.name ?? <span className="text-muted-foreground">Each</span>}</TableCell>
                  <TableCell className="text-muted-foreground">{tokenLimitsText(token)}</TableCell>
                  <TableCell className="text-muted-foreground">{expiry(token.expires_at)}</TableCell>
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
              {revoking?.dispatcher
                ? "The Dispatcher and each of its child tokens can no longer reach SolidTrace, so the factory stops until you give it a new Dispatcher's token. This can't be undone."
                : "Clients that use this token can no longer reach SolidTrace. This can't be undone."}
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

/**
 * The Dispatcher's token, for an Admin: it has fixed scopes, acts as you in
 * one Organization, and makes the child tokens of each Run.
 */
function DispatcherToken({ choice, onMade }: { choice: DispatcherChoice; onMade: (apiToken: ApiToken, secret: string) => void }) {
  const [organizationId, setOrganizationId] = useState(String(choice.organizations[0].id))
  const [expiry, setExpiry] = useState(DEFAULT_EXPIRY)
  const [making, setMaking] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const organization = choice.organizations.find(o => String(o.id) === organizationId)

  const make: React.FormEventHandler = async (e) => {
    e.preventDefault()
    setMaking(true)
    setError(null)
    try {
      const { apiToken, secret } = await createApiToken(dispatcherFields(choice, organizationId, expiry))
      onMade(apiToken, secret)
      router.reload({ only: ['api_tokens'] })
    } catch (failure) {
      setError((failure as Error).message)
    } finally {
      setMaking(false)
    }
  }

  return (
    <form onSubmit={make} aria-label="The Dispatcher's token" className="flex flex-col gap-3 border p-4">
      <div className="flex flex-col gap-1">
        <h3 className="text-sm font-medium">The Dispatcher's token</h3>
        <p className="text-xs text-muted-foreground">
          For Admins. The Dispatcher acts as you in {organization?.name ?? 'the Organization'}, with only{' '}
          <span className="font-mono">{choice.scopes.join(' ')}</span>. For each Run it makes a child token with
          sessions:write and sessions:read for the Run's Agent sessions, or mcp:read for one Project, that expires no
          later than this token. If you leave the Organization, this token and its child tokens stop working.
        </p>
      </div>
      <div className="flex flex-wrap items-end gap-3">
        <div className="space-y-2">
          <Label>Organization</Label>
          <Select
            value={organizationId}
            onValueChange={(value) => { if (value) setOrganizationId(String(value)) }}
            items={Object.fromEntries(choice.organizations.map(o => [String(o.id), o.name]))}
          >
            <SelectTrigger className="w-48" aria-label="Organization of the Dispatcher's token"><SelectValue /></SelectTrigger>
            <SelectContent>
              {choice.organizations.map(o => <SelectItem key={o.id} value={String(o.id)}>{o.name}</SelectItem>)}
            </SelectContent>
          </Select>
        </div>
        <div className="space-y-2">
          <Label>Expiry</Label>
          <Select value={expiry} onValueChange={(value) => setExpiry(String(value ?? DEFAULT_EXPIRY))} items={EXPIRY_CHOICES}>
            <SelectTrigger className="w-36" aria-label="Expiry of the Dispatcher's token"><SelectValue /></SelectTrigger>
            <SelectContent>
              {Object.entries(EXPIRY_CHOICES).map(([value, label]) => <SelectItem key={value} value={value}>{label}</SelectItem>)}
            </SelectContent>
          </Select>
        </div>
        <Button type="submit" variant="outline" disabled={making}>
          {making ? 'Making…' : "Make the Dispatcher's token"}
        </Button>
      </div>
      {error && <p role="alert" className="text-sm text-destructive">{error}</p>}
    </form>
  )
}

/**
 * A token just made: its secret, shown this once, and the snippets that use
 * it; for the Dispatcher's token, where the Dispatcher reads it.
 */
function MadeToken({ name, secret, mcpUrl, dispatcher, onDone }: {
  name: string; secret: string; mcpUrl: string; dispatcher: boolean; onDone: () => void
}) {
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

      {dispatcher ? (
        <p className="text-muted-foreground">
          Give it to the Dispatcher as <span className="font-mono">DISPATCHER_SOLIDTRACE_TOKEN</span>, then restart the Dispatcher.
        </p>
      ) : (
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
      )}

      <div>
        <Button variant="outline" size="sm" onClick={onDone}>Done</Button>
      </div>
    </section>
  )
}

function ago(iso: string): string {
  return `${formatDistanceToNow(new Date(iso))} ago`
}

function expiry(iso: string | null): string {
  if (!iso) return 'No expiry'
  const at = new Date(iso)
  return at.getTime() > Date.now() ? `in ${formatDistanceToNow(at)}` : `Expired ${formatDistanceToNow(at)} ago`
}
