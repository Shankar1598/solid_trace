import { router } from '@inertiajs/react'
import { useState } from 'react'
import AuthLayout from '@/components/layouts/AuthLayout'
import ApprovalChoices, { type ApprovalOrganization } from '@/components/oauth/ApprovalChoices'

type Props = {
  request: string
  client: { name: string; name_note: string }
  // When and from which IP address the program asked for the code.
  asked_at: string
  asked_from: string
  organizations: ApprovalOrganization[]
  no_organization: string | null
  revoke_hint: string
}

// The device page after a right code (Oauth::DevicesController): the same
// content as the approval page, and when and from where the program asked,
// so the user can tell their own program from another person's. The form
// sends only the pending request's key, the Organization's id and the
// decision; the program then gets its answer when it polls.
export default function DeviceApproval({ request, client, asked_at, asked_from, organizations, no_organization, revoke_hint }: Props) {
  const [processing, setProcessing] = useState(false)

  const decide = (decision: 'approve' | 'deny', organizationId: string) => {
    setProcessing(true)
    router.post('/device', { request, organization_id: organizationId, decision }, {
      onFinish: () => setProcessing(false),
    })
  }

  return (
    <AuthLayout title={`Sign in ${client.name}`} description="A program asks to use SolidTrace as you.">
      <div className="space-y-5 text-sm">
        <div className="space-y-1">
          <p className="text-base font-medium">{client.name}</p>
          <p className="text-muted-foreground">{client.name_note}</p>
          <p data-testid="asked">
            Asked for at <time dateTime={asked_at}>{new Date(asked_at).toLocaleString()}</time> from{' '}
            <span className="font-mono">{asked_from}</span>
          </p>
        </div>

        <p role="alert" className="border border-amber-500/50 bg-amber-500/10 p-3">
          Approve only if you started this program yourself, at this time and from this address.
        </p>

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
