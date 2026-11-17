import { router } from '@inertiajs/react'
import { useState } from 'react'
import AuthLayout from '@/components/layouts/AuthLayout'
import ApprovalChoices, { type ApprovalOrganization } from '@/components/oauth/ApprovalChoices'

type Props = {
  request: string
  client: { name: string; name_checked: boolean; name_note: string; redirect_host: string }
  loopback_warning: string | null
  organizations: ApprovalOrganization[]
  no_organization: string | null
  revoke_hint: string
}

// The approval page (Oauth::AuthorizationsController): an OAuth client asks
// to act as the user in one Organization. The form sends only the pending
// request's key, the Organization's id and the decision; Console sends the
// browser back to the client.
export default function Authorize({ request, client, loopback_warning, organizations, no_organization, revoke_hint }: Props) {
  const [processing, setProcessing] = useState(false)

  const decide = (decision: 'approve' | 'deny', organizationId: string) => {
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
          {/* A published name: the client owns the host that publishes it. A registered name is only its claim. */}
          <p className={client.name_checked ? 'text-muted-foreground' : 'text-amber-700 dark:text-amber-400'}>
            {client.name_note}
          </p>
          <p>
            After you decide, Console sends you to <span className="font-mono">{client.redirect_host}</span>.
          </p>
        </div>

        {loopback_warning && (
          <p role="alert" className="border border-amber-500/50 bg-amber-500/10 p-3">{loopback_warning}</p>
        )}

        <ApprovalChoices
          organizations={organizations}
          no_organization={no_organization}
          revoke_hint={revoke_hint}
          processing={processing}
          onDecide={decide}
        />
      </div>
    </AuthLayout>
  )
}
