import { router } from '@inertiajs/react'
import { useState } from 'react'
import AuthLayout from '@/components/layouts/AuthLayout'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'

type Organization = { id: number; name: string; slug: string; scopes: string[] }

type Props = {
  request: string
  client: { name: string; name_note: string; redirect_host: string }
  loopback_warning: string | null
  organizations: Organization[]
  no_organization: string | null
  revoke_hint: string
}

// The approval page (Oauth::AuthorizationsController): an OAuth client asks
// to act as the user in one Organization. The form sends only the pending
// request's key, the Organization's id and the decision; Console sends the
// browser back to the client. The first Organization is the last used one.
// A user in no Organization sees why, and only Deny.
export default function Authorize({ request, client, loopback_warning, organizations, no_organization, revoke_hint }: Props) {
  const [organizationId, setOrganizationId] = useState(organizations[0]?.id.toString() ?? '')
  const [processing, setProcessing] = useState(false)
  const organization = organizations.find((o) => o.id.toString() === organizationId)
  const items = organizations.map((o) => ({ value: o.id.toString(), label: o.name }))

  const decide = (decision: 'approve' | 'deny') => {
    setProcessing(true)
    router.post('/oauth/authorize', { request, organization_id: organizationId, decision }, {
      onFinish: () => setProcessing(false),
    })
  }

  return (
    <AuthLayout title={`Sign in ${client.name}`} description="A program asks to use SolidTrace as you.">
      <div className="space-y-5 text-sm">
        <div className="space-y-1">
          <p className="text-base font-medium">{client.name}</p>
          <p className="text-muted-foreground">{client.name_note}</p>
          <p>
            After you decide, Console sends you to <span className="font-mono">{client.redirect_host}</span>.
          </p>
        </div>

        {loopback_warning && (
          <p role="alert" className="border border-amber-500/50 bg-amber-500/10 p-3">{loopback_warning}</p>
        )}

        {no_organization ? (
          <p role="note">{no_organization}</p>
        ) : (
          <>
            {organizations.length > 1 && (
              <div className="space-y-2">
                <Label htmlFor="organization">Organization</Label>
                <Select value={organizationId} onValueChange={(value) => setOrganizationId(value ?? '')} items={items}>
                  <SelectTrigger id="organization">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {items.map((item) => (
                      <SelectItem key={item.value} value={item.value}>{item.label}</SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            )}
            <ul className="list-disc space-y-1 pl-5">
              {organization?.scopes.map((text) => <li key={text}>{text}</li>)}
            </ul>
          </>
        )}

        {!no_organization && <p className="text-muted-foreground">{revoke_hint}</p>}

        <div className="flex gap-2">
          {!no_organization && (
            <Button type="button" disabled={processing} onClick={() => decide('approve')}>Approve</Button>
          )}
          <Button type="button" variant="outline" disabled={processing} onClick={() => decide('deny')}>Deny</Button>
        </div>
      </div>
    </AuthLayout>
  )
}
