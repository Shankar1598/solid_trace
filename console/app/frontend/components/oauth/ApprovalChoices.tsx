import { useState } from 'react'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'

// two_factor_note: the Organization has Require two-factor and the user has
// no Two-factor, so Approve sends them to set it up first.
export type ApprovalOrganization = {
  id: number
  name: string
  slug: string
  scopes: string[]
  two_factor_note: string | null
}

type Props = {
  organizations: ApprovalOrganization[]
  no_organization: string | null
  revoke_hint: string
  processing: boolean
  onDecide: (decision: 'approve' | 'deny', organizationId: string) => void
}

// What the approval page and the device page share: the Organization picker
// (the first is the last used one), each scope's text, the Require
// two-factor note, and Approve and Deny. A user in no Organization sees why,
// and only Deny.
export default function ApprovalChoices({ organizations, no_organization, revoke_hint, processing, onDecide }: Props) {
  const [organizationId, setOrganizationId] = useState(organizations[0]?.id.toString() ?? '')
  const organization = organizations.find((o) => o.id.toString() === organizationId)
  const items = organizations.map((o) => ({ value: o.id.toString(), label: o.name }))

  return (
    <>
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
          {organization?.two_factor_note && (
            <p role="note" className="border border-amber-500/50 bg-amber-500/10 p-3">{organization.two_factor_note}</p>
          )}
        </>
      )}

      {!no_organization && <p className="text-muted-foreground">{revoke_hint}</p>}

      <div className="flex gap-2">
        {!no_organization && (
          <Button type="button" disabled={processing} onClick={() => onDecide('approve', organizationId)}>Approve</Button>
        )}
        <Button type="button" variant="outline" disabled={processing} onClick={() => onDecide('deny', organizationId)}>
          Deny
        </Button>
      </div>
    </>
  )
}
