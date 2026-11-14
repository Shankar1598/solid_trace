import { useState } from 'react'
import { Link, router, usePage } from '@inertiajs/react'
import { formatDistanceToNow } from 'date-fns'
import { MonitorSmartphone } from 'lucide-react'
import {
  AlertDialog,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { shortUserAgent } from '@/lib/userAgent'
import type { SharedProps } from '@/types'

/**
 * One of the user's Signed-in clients: a live Grant that has had a token
 * (UserSettingsController#signed_in_client).
 */
export interface SignedInClientRow {
  id: number
  // name_checked: the name comes from a published description at host;
  // else the client gave it (DCR), and host is its redirect URIs' host.
  client: { name: string, name_checked: boolean, host: string }
  organization: { name: string, slug: string }
  approved_via: 'browser' | 'device'
  user_agent: string | null
  approved_at: string
  last_used_at: string | null
}

const date = (iso: string) => new Date(iso).toLocaleDateString(undefined, { dateStyle: 'medium' })

/**
 * Settings → User → Signed-in clients: each MCP client that the user
 * signed in through the browser or a device code, one row for each
 * sign-in, and Revoke, which asks first (SignedInClientsController).
 */
export function SignedInClientsCard({ clients }: { clients: SignedInClientRow[] }) {
  const { current_org: currentOrg } = usePage<SharedProps>().props
  const [revoking, setRevoking] = useState<SignedInClientRow | null>(null)

  const revoke = (client: SignedInClientRow) => {
    router.delete(`/signed_in_clients/${client.id}`, { preserveScroll: true, onFinish: () => setRevoking(null) })
  }

  return (
    <Card data-testid="signed-in-clients">
      <CardHeader>
        <CardTitle className="flex items-center gap-2"><MonitorSmartphone className="h-4 w-4" />Signed-in clients</CardTitle>
        <CardDescription>MCP clients that act as you, each in one Organization. Revoke one to sign it out at once.</CardDescription>
      </CardHeader>
      <CardContent>
        {clients.length === 0 ? (
          <p className="text-sm text-muted-foreground">
            Clients that you sign in through the browser show here. API tokens are on{' '}
            {currentOrg
              ? <Link href={`/${currentOrg.slug}/settings/api_tokens`} className="underline underline-offset-2">Settings → API tokens &amp; MCP</Link>
              : 'Settings → API tokens & MCP'}.
          </p>
        ) : (
          <ul className="divide-y" aria-label="Your Signed-in clients">
            {clients.map((client) => (
              <li key={client.id} className="flex flex-wrap items-center justify-between gap-2 py-3" aria-label={`${client.client.name} in ${client.organization.name}`}>
                <div className="min-w-0 space-y-0.5">
                  <div className="flex flex-wrap items-center gap-2 text-sm font-medium">
                    <span className="break-all">{client.client.name}</span>
                    {client.client.name_checked
                      ? <span className="font-mono text-xs font-normal text-muted-foreground">{client.client.host}</span>
                      : <Badge variant="warn">Not checked</Badge>}
                  </div>
                  <div className="text-xs text-muted-foreground">
                    {client.organization.name}
                    {' · '}{client.approved_via === 'device' ? 'Device code' : 'Browser'}
                    {client.user_agent && (
                      <>{' · '}<span title={client.user_agent}>{shortUserAgent(client.user_agent)}</span></>
                    )}
                    {!client.client.name_checked && client.client.host && (
                      <>{' · '}<span className="font-mono">{client.client.host}</span></>
                    )}
                  </div>
                  <div className="text-xs text-muted-foreground">
                    Approved {date(client.approved_at)}
                    {' · '}{client.last_used_at ? `Last used ${formatDistanceToNow(new Date(client.last_used_at))} ago` : 'Never used'}
                  </div>
                </div>
                <Button
                  type="button"
                  size="sm"
                  variant="outline"
                  onClick={() => setRevoking(client)}
                  aria-label={`Revoke ${client.client.name} in ${client.organization.name}`}
                >
                  Revoke
                </Button>
              </li>
            ))}
          </ul>
        )}
      </CardContent>

      <AlertDialog open={revoking !== null} onOpenChange={(open) => { if (!open) setRevoking(null) }}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Revoke "{revoking?.client.name}"?</AlertDialogTitle>
            <AlertDialogDescription>
              This client can no longer act as you in {revoking?.organization.name}. To use it again, sign it in again.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <Button variant="destructive" onClick={() => revoking && revoke(revoking)}>Revoke client</Button>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </Card>
  )
}
